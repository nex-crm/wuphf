package workflow

import (
	"testing"
)

func TestSaveAndLoadVersion_HappyPath(t *testing.T) {
	setupTestDir(t)

	spec := WorkflowSpec{
		ID:    "email-triage",
		Title: "Email Triage v1",
		Steps: []StepSpec{
			{ID: "step1", Type: StepSelect},
		},
	}

	// Save first version.
	v, err := SaveVersion("email-triage", spec)
	if err != nil {
		t.Fatalf("SaveVersion: %v", err)
	}
	if v != 1 {
		t.Errorf("expected version 1, got %d", v)
	}

	// Save second version with updated title.
	spec.Title = "Email Triage v2"
	v2, err := SaveVersion("email-triage", spec)
	if err != nil {
		t.Fatalf("SaveVersion v2: %v", err)
	}
	if v2 != 2 {
		t.Errorf("expected version 2, got %d", v2)
	}

	// Load latest (version 0 = active).
	loaded, loadedV, err := LoadVersion("email-triage", 0)
	if err != nil {
		t.Fatalf("LoadVersion(0): %v", err)
	}
	if loadedV != 2 {
		t.Errorf("expected loaded version 2, got %d", loadedV)
	}
	if loaded.Title != "Email Triage v2" {
		t.Errorf("expected v2 title, got %q", loaded.Title)
	}

	// Load specific version.
	loaded1, loadedV1, err := LoadVersion("email-triage", 1)
	if err != nil {
		t.Fatalf("LoadVersion(1): %v", err)
	}
	if loadedV1 != 1 {
		t.Errorf("expected loaded version 1, got %d", loadedV1)
	}
	if loaded1.Title != "Email Triage v1" {
		t.Errorf("expected v1 title, got %q", loaded1.Title)
	}
}

func TestLoadVersion_NoVersions(t *testing.T) {
	setupTestDir(t)

	_, _, err := LoadVersion("nonexistent", 0)
	if err == nil {
		t.Fatal("expected error for loading nonexistent workflow version")
	}
}

func TestLoadVersion_OutOfRange(t *testing.T) {
	setupTestDir(t)

	spec := WorkflowSpec{
		ID:    "test-wf",
		Title: "Test",
		Steps: []StepSpec{{ID: "s1", Type: StepConfirm}},
	}

	if _, err := SaveVersion("test-wf", spec); err != nil {
		t.Fatalf("SaveVersion: %v", err)
	}

	// Version 2 doesn't exist yet.
	_, _, err := LoadVersion("test-wf", 2)
	if err == nil {
		t.Fatal("expected error for version out of range")
	}

	// Negative version.
	_, _, err = LoadVersion("test-wf", -1)
	if err == nil {
		t.Fatal("expected error for negative version")
	}
}

func TestListVersions_HappyPath(t *testing.T) {
	setupTestDir(t)

	spec := WorkflowSpec{
		ID:    "deploy-check",
		Title: "Deploy Check",
		Steps: []StepSpec{{ID: "s1", Type: StepRun, Agent: "checker"}},
	}

	for i := 0; i < 3; i++ {
		if _, err := SaveVersion("deploy-check", spec); err != nil {
			t.Fatalf("SaveVersion iteration %d: %v", i, err)
		}
	}

	manifest, err := ListVersions("deploy-check")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected manifest, got nil")
	}
	if manifest.VersionCount != 3 {
		t.Errorf("expected 3 versions, got %d", manifest.VersionCount)
	}
	if manifest.ActiveVersion != 3 {
		t.Errorf("expected active version 3, got %d", manifest.ActiveVersion)
	}
	if manifest.Key != "deploy-check" {
		t.Errorf("expected key deploy-check, got %q", manifest.Key)
	}
	if manifest.CreatedAt == "" {
		t.Error("expected CreatedAt to be set")
	}
	if manifest.UpdatedAt == "" {
		t.Error("expected UpdatedAt to be set")
	}
}

func TestListVersions_NoVersions(t *testing.T) {
	setupTestDir(t)

	manifest, err := ListVersions("never-saved")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if manifest != nil {
		t.Errorf("expected nil manifest for unsaved workflow, got %+v", manifest)
	}
}

func TestSaveVersion_PreservesCreatedAt(t *testing.T) {
	setupTestDir(t)

	spec := WorkflowSpec{
		ID:    "wf",
		Title: "WF",
		Steps: []StepSpec{{ID: "s1", Type: StepConfirm}},
	}

	SaveVersion("wf", spec)
	m1, _ := ListVersions("wf")
	createdAt := m1.CreatedAt

	SaveVersion("wf", spec)
	m2, _ := ListVersions("wf")

	if m2.CreatedAt != createdAt {
		t.Errorf("CreatedAt changed: %q -> %q", createdAt, m2.CreatedAt)
	}
	if m2.UpdatedAt == "" {
		t.Error("expected UpdatedAt to be set on second save")
	}
}
