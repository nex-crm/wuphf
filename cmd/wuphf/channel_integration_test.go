package main

import (
	"strings"
	"testing"
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
		if got := slugifyGroupTitle(input); got != want {
			t.Errorf("slugifyGroupTitle(%q) = %q, want %q", input, got, want)
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
