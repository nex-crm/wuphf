package company

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifestFallsBackToDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.Name == "" || len(manifest.Members) == 0 || len(manifest.Channels) == 0 {
		t.Fatalf("expected default manifest, got %+v", manifest)
	}
}

func TestSaveAndLoadManifestRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "company.json")
	t.Setenv("WUPHF_COMPANY_FILE", path)

	manifest := Manifest{
		Name: "Test Office",
		Lead: "ceo",
		Members: []MemberSpec{
			{Slug: "ceo", Name: "CEO", Role: "CEO", System: true},
			{Slug: "ops", Name: "Ops", Role: "Operations"},
		},
		Channels: []ChannelSpec{
			{Slug: "general", Name: "general", Members: []string{"ceo", "ops"}},
			{Slug: "deals", Name: "deals", Members: []string{"ceo", "ops"}},
		},
	}
	if err := SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected manifest file: %v", err)
	}

	loaded, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.Name != "Test Office" {
		t.Fatalf("unexpected manifest name: %q", loaded.Name)
	}
	if len(loaded.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(loaded.Channels))
	}
	for _, ch := range loaded.Channels {
		if ch.Description == "" {
			t.Fatalf("expected channel description for %s", ch.Slug)
		}
		if !containsSlug(ch.Members, "ceo") {
			t.Fatalf("expected CEO to be present in channel %s", ch.Slug)
		}
	}
}

func TestManifestSurfaceSpecRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "company.json")
	t.Setenv("WUPHF_COMPANY_FILE", path)

	manifest := Manifest{
		Name: "Surface Test",
		Lead: "ceo",
		Members: []MemberSpec{
			{Slug: "ceo", Name: "CEO", Role: "CEO", System: true},
		},
		Channels: []ChannelSpec{
			{
				Slug:    "general",
				Name:    "general",
				Members: []string{"ceo"},
			},
			{
				Slug:    "tg-ops",
				Name:    "Telegram Ops",
				Members: []string{"ceo"},
				Surface: &ChannelSurfaceSpec{
					Provider:    "telegram",
					RemoteID:    "-100123",
					RemoteTitle: "Ops Group",
					Mode:        "supergroup",
					BotTokenEnv: "OPS_BOT_TOKEN",
				},
			},
		},
	}
	if err := SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	var tgChannel *ChannelSpec
	for i, ch := range loaded.Channels {
		if ch.Slug == "tg-ops" {
			tgChannel = &loaded.Channels[i]
			break
		}
	}
	if tgChannel == nil {
		t.Fatal("expected tg-ops channel after reload")
	}
	if tgChannel.Surface == nil {
		t.Fatal("expected surface spec to persist")
	}
	if tgChannel.Surface.Provider != "telegram" {
		t.Fatalf("expected provider=telegram, got %q", tgChannel.Surface.Provider)
	}
	if tgChannel.Surface.RemoteID != "-100123" {
		t.Fatalf("expected remote_id=-100123, got %q", tgChannel.Surface.RemoteID)
	}
	if tgChannel.Surface.RemoteTitle != "Ops Group" {
		t.Fatalf("expected remote_title, got %q", tgChannel.Surface.RemoteTitle)
	}
	if tgChannel.Surface.Mode != "supergroup" {
		t.Fatalf("expected mode=supergroup, got %q", tgChannel.Surface.Mode)
	}
	if tgChannel.Surface.BotTokenEnv != "OPS_BOT_TOKEN" {
		t.Fatalf("expected bot_token_env=OPS_BOT_TOKEN, got %q", tgChannel.Surface.BotTokenEnv)
	}

	// Channel without surface should have nil
	for _, ch := range loaded.Channels {
		if ch.Slug == "general" && ch.Surface != nil {
			t.Fatal("general channel should not have a surface")
		}
	}
}

func TestDefaultManifestHasNoSurface(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manifest := DefaultManifest()
	for _, ch := range manifest.Channels {
		if ch.Surface != nil {
			t.Fatalf("default channel %s should not have a surface", ch.Slug)
		}
	}
}
