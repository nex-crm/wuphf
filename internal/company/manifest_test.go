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
}
