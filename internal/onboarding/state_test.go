package onboarding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempHome redirects os.UserHomeDir (via $HOME) to a temp dir for
// the duration of f, keeping test state isolated from the real ~/.wuphf.
func withTempHome(t *testing.T, f func(home string)) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	f(dir)
}

func TestLoadFreshInstallReturnsNotOnboarded(t *testing.T) {
	withTempHome(t, func(_ string) {
		s, err := Load()
		if err != nil {
			t.Fatalf("Load: unexpected error: %v", err)
		}
		if s.Onboarded() {
			t.Fatal("fresh install should not be onboarded")
		}
		if s.Version != currentStateVersion {
			t.Fatalf("Version: got %d, want %d", s.Version, currentStateVersion)
		}
		if len(s.Checklist) == 0 {
			t.Fatal("fresh install should have a default checklist")
		}
	})
}

func TestLoadDefaultChecklistItems(t *testing.T) {
	withTempHome(t, func(_ string) {
		s, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		defaults := DefaultChecklist()
		if len(s.Checklist) != len(defaults) {
			t.Fatalf("checklist length: got %d, want %d", len(s.Checklist), len(defaults))
		}
		for i, item := range s.Checklist {
			if item.ID != defaults[i].ID {
				t.Errorf("checklist[%d].ID: got %q, want %q", i, item.ID, defaults[i].ID)
			}
			if item.Done {
				t.Errorf("checklist[%d] should not be done on fresh install", i)
			}
		}
	})
}

func TestLoadExistingFileReturnsCorrectData(t *testing.T) {
	withTempHome(t, func(home string) {
		dir := filepath.Join(home, ".wuphf")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		raw := `{
			"completed_at": "2026-04-14T10:23:00Z",
			"version": 1,
			"company_name": "Dunder Mifflin",
			"completed_steps": ["welcome", "setup"],
			"checklist_dismissed": false,
			"checklist": [
				{"id": "pick_team", "done": true},
				{"id": "second_key", "done": false},
				{"id": "github_repo", "done": false},
				{"id": "github_star", "done": false},
				{"id": "discord", "done": false}
			]
		}`
		if err := os.WriteFile(filepath.Join(dir, "onboarded.json"), []byte(raw), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		s, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !s.Onboarded() {
			t.Fatal("expected Onboarded()==true")
		}
		if s.CompanyName != "Dunder Mifflin" {
			t.Errorf("CompanyName: got %q, want %q", s.CompanyName, "Dunder Mifflin")
		}
		if len(s.CompletedSteps) != 2 {
			t.Errorf("CompletedSteps length: got %d, want 2", len(s.CompletedSteps))
		}
		if !s.Checklist[0].Done {
			t.Error("checklist[0] (pick_team) should be done")
		}
	})
}

func TestSaveIsAtomic(t *testing.T) {
	withTempHome(t, func(home string) {
		s := &State{
			Version:     currentStateVersion,
			CompanyName: "Initech",
			Checklist:   DefaultChecklist(),
		}
		if err := Save(s); err != nil {
			t.Fatalf("Save: %v", err)
		}
		path := StatePath()
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file should exist at %s after Save: %v", path, err)
		}
		// Ensure no temp file leaked.
		entries, _ := os.ReadDir(filepath.Dir(path))
		for _, e := range entries {
			if e.Name() != "onboarded.json" {
				t.Errorf("unexpected file in .wuphf dir after Save: %s", e.Name())
			}
		}
	})
}

func TestSaveRoundtrip(t *testing.T) {
	withTempHome(t, func(_ string) {
		original := &State{
			CompletedAt:        "2026-04-14T10:23:00Z",
			Version:            currentStateVersion,
			CompanyName:        "Paper Company",
			CompletedSteps:     []string{"welcome", "setup"},
			ChecklistDismissed: false,
			Checklist:          DefaultChecklist(),
		}
		if err := Save(original); err != nil {
			t.Fatalf("Save: %v", err)
		}
		loaded, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if loaded.CompanyName != original.CompanyName {
			t.Errorf("CompanyName: got %q, want %q", loaded.CompanyName, original.CompanyName)
		}
		if loaded.CompletedAt != original.CompletedAt {
			t.Errorf("CompletedAt: got %q, want %q", loaded.CompletedAt, original.CompletedAt)
		}
	})
}

func TestSaveProgressMergesCorrectly(t *testing.T) {
	withTempHome(t, func(_ string) {
		// First save progress for welcome step.
		welcomeAnswers := map[string]interface{}{
			"company_name": "Initech",
			"description":  "We do TPS reports",
		}
		if err := SaveProgress("welcome", welcomeAnswers); err != nil {
			t.Fatalf("SaveProgress welcome: %v", err)
		}

		// Then save progress for setup step.
		setupAnswers := map[string]interface{}{
			"anthropic_key": "sk-ant-test",
		}
		if err := SaveProgress("setup", setupAnswers); err != nil {
			t.Fatalf("SaveProgress setup: %v", err)
		}

		s, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if s.Partial == nil {
			t.Fatal("expected Partial to be non-nil")
		}
		if s.Partial.Step != "setup" {
			t.Errorf("Partial.Step: got %q, want %q", s.Partial.Step, "setup")
		}
		// Both steps' answers should be present.
		if _, ok := s.Partial.Answers["welcome"]; !ok {
			t.Error("expected welcome answers to be persisted")
		}
		if _, ok := s.Partial.Answers["setup"]; !ok {
			t.Error("expected setup answers to be persisted")
		}
		if s.Partial.Answers["welcome"]["company_name"] != "Initech" {
			t.Errorf("welcome company_name: got %v", s.Partial.Answers["welcome"]["company_name"])
		}
	})
}

func TestVersionBumpReturnsNotOnboarded(t *testing.T) {
	withTempHome(t, func(home string) {
		// Write a file that looks complete but with an old schema version.
		dir := filepath.Join(home, ".wuphf")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		stale := map[string]interface{}{
			"completed_at": time.Now().UTC().Format(time.RFC3339),
			"version":      0, // old version
			"company_name": "Old Corp",
		}
		data, _ := json.Marshal(stale)
		if err := os.WriteFile(filepath.Join(dir, "onboarded.json"), data, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		s, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if s.Onboarded() {
			t.Fatal("stale version should return onboarded=false")
		}
		// Version should be upgraded to current.
		if s.Version != currentStateVersion {
			t.Errorf("Version: got %d, want %d", s.Version, currentStateVersion)
		}
	})
}

func TestMarkChecklistItem(t *testing.T) {
	withTempHome(t, func(_ string) {
		// Start fresh.
		if err := Save(&State{Version: currentStateVersion, Checklist: DefaultChecklist()}); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if err := MarkChecklistItem("pick_team", true); err != nil {
			t.Fatalf("MarkChecklistItem: %v", err)
		}
		s, _ := Load()
		found := false
		for _, item := range s.Checklist {
			if item.ID == "pick_team" {
				found = true
				if !item.Done {
					t.Error("pick_team should be done")
				}
			}
		}
		if !found {
			t.Error("pick_team not found in checklist")
		}
	})
}

func TestDismissChecklist(t *testing.T) {
	withTempHome(t, func(_ string) {
		if err := Save(&State{Version: currentStateVersion, Checklist: DefaultChecklist()}); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if err := DismissChecklist(); err != nil {
			t.Fatalf("DismissChecklist: %v", err)
		}
		s, _ := Load()
		if !s.ChecklistDismissed {
			t.Error("ChecklistDismissed should be true")
		}
	})
}
