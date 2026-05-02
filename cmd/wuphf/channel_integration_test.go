package main

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/team"
)

func TestSlugifyGroupTitleAddsTgPrefix(t *testing.T) {
	cases := map[string]string{
		"My Team":             "tg-my-team",
		" My!@#$Team Lounge ": "tg-my-team-lounge",
		"---":                 "tg-telegram",
		"":                    "tg-telegram",
		"NUMBERS 123":         "tg-numbers-123",
	}
	for input, want := range cases {
		if got := team.SlugifyTelegramTitle(input); got != want {
			t.Errorf("team.SlugifyTelegramTitle(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSlugifyOpenclawLabelFallsBackToSession(t *testing.T) {
	cases := map[string]string{
		"My Session":  "my-session",
		"--- ---":     "session",
		"":            "session",
		"NUMBERS 123": "numbers-123",
	}
	for input, want := range cases {
		if got := slugifyOpenclawLabel(input); got != want {
			t.Errorf("slugifyOpenclawLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestChannelIntegrationOptionsMatchesSpecs(t *testing.T) {
	got := channelIntegrationOptions()
	if len(got) != len(channelIntegrationSpecs) {
		t.Fatalf("expected %d options, got %d", len(channelIntegrationSpecs), len(got))
	}
	for i, opt := range got {
		spec := channelIntegrationSpecs[i]
		if opt.Label != spec.Label || opt.Value != spec.Value || opt.Description != spec.Description {
			t.Errorf("option %d mismatch: got %+v want %+v", i, opt, spec)
		}
	}
}

func TestFindChannelIntegrationKnownAndUnknown(t *testing.T) {
	if _, ok := findChannelIntegration("nope"); ok {
		t.Fatalf("expected miss for unknown integration")
	}
	if len(channelIntegrationSpecs) == 0 {
		t.Skip("no integration specs declared, skipping known-spec lookup")
	}
	known := channelIntegrationSpecs[0].Value
	spec, ok := findChannelIntegration(known)
	if !ok {
		t.Fatalf("expected hit for %q", known)
	}
	if spec.Value != known {
		t.Errorf("expected spec.Value=%q, got %q", known, spec.Value)
	}
}

func TestChannelIntegrationOptionsContainsKnownProvider(t *testing.T) {
	got := channelIntegrationOptions()
	var found bool
	for _, opt := range got {
		if strings.Contains(strings.ToLower(opt.Label), "google") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one Google integration option, got %v", got)
	}
}

func TestFindManifestTelegramChannelMatchesRemoteIDOnly(t *testing.T) {
	manifest := company.Manifest{Channels: []company.ChannelSpec{{
		Slug: "tg-old-title",
		Surface: &company.ChannelSurfaceSpec{
			Provider: "telegram",
			RemoteID: "100",
		},
	}}}

	got, err := findManifestTelegramChannel(manifest, "tg-new-title", "100")
	if err != nil {
		t.Fatalf("same remote id: unexpected error: %v", err)
	}
	if got != "tg-old-title" {
		t.Fatalf("same remote id: got %q, want existing slug", got)
	}

	manifest.Channels = append(manifest.Channels, company.ChannelSpec{
		Slug: "tg-new-title",
		Surface: &company.ChannelSurfaceSpec{
			Provider: "telegram",
			RemoteID: "200",
		},
	})
	if _, err := findManifestTelegramChannel(manifest, "tg-new-title", "300"); err == nil {
		t.Fatal("slug collision with different remote id: expected error")
	}
}

func TestFindLiveTelegramChannelMatchesRemoteIDOnly(t *testing.T) {
	channels := []telegramBrokerChannel{{
		Slug: "tg-old-title",
		Surface: &telegramBrokerSurface{
			Provider: "telegram",
			RemoteID: "100",
		},
	}}

	got, err := findLiveTelegramChannel(channels, "tg-new-title", "100")
	if err != nil {
		t.Fatalf("same remote id: unexpected error: %v", err)
	}
	if got != "tg-old-title" {
		t.Fatalf("same remote id: got %q, want existing slug", got)
	}

	if _, err := findLiveTelegramChannel([]telegramBrokerChannel{{
		Slug: "tg-new-title",
	}}, "tg-new-title", "100"); err == nil {
		t.Fatal("slug collision with non-telegram channel: expected error")
	}

	if _, err := findLiveTelegramChannel([]telegramBrokerChannel{{
		Slug: "tg-new-title",
		Surface: &telegramBrokerSurface{
			Provider: "telegram",
			RemoteID: "200",
		},
	}}, "tg-new-title", "100"); err == nil {
		t.Fatal("slug collision with different Telegram remote id: expected error")
	}
}
