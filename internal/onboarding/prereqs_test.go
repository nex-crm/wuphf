package onboarding

import (
	"testing"
)

func TestCheckOneGitFound(t *testing.T) {
	r := CheckOne("git")
	if !r.Found {
		t.Fatal("expected git to be found on PATH in CI/dev environment")
	}
	if r.Name != "git" {
		t.Errorf("Name: got %q, want %q", r.Name, "git")
	}
	if r.InstallURL == "" {
		t.Error("InstallURL should be non-empty for git")
	}
	if r.Version == "" {
		t.Error("Version should be non-empty when git is installed")
	}
}

func TestCheckOneNonexistentBinary(t *testing.T) {
	r := CheckOne("nonexistent-binary-xyz-wuphf")
	if r.Found {
		t.Fatal("expected Found=false for nonexistent binary")
	}
	if r.Version != "" {
		t.Errorf("Version should be empty when binary is not found, got %q", r.Version)
	}
	// InstallURL is empty for unknown binaries (not in prereqSpecs).
	if r.Name != "nonexistent-binary-xyz-wuphf" {
		t.Errorf("Name: got %q, want %q", r.Name, "nonexistent-binary-xyz-wuphf")
	}
}

func TestCheckAllReturnsThreeItems(t *testing.T) {
	results := CheckAll()
	if len(results) != 3 {
		t.Fatalf("CheckAll: got %d results, want 3", len(results))
	}
	names := []string{"node", "git", "claude"}
	for i, r := range results {
		if r.Name != names[i] {
			t.Errorf("CheckAll[%d].Name: got %q, want %q", i, r.Name, names[i])
		}
	}
}

func TestCheckAllRequiredFlags(t *testing.T) {
	results := CheckAll()
	for _, r := range results {
		if !r.Required {
			t.Errorf("%s: expected Required=true", r.Name)
		}
	}
}

func TestCheckAllInstallURLs(t *testing.T) {
	wantURLs := map[string]string{
		"node":   "https://nodejs.org",
		"git":    "https://git-scm.com",
		"claude": "https://claude.ai/code",
	}
	for _, r := range CheckAll() {
		want, ok := wantURLs[r.Name]
		if !ok {
			continue
		}
		if r.InstallURL != want {
			t.Errorf("%s: InstallURL: got %q, want %q", r.Name, r.InstallURL, want)
		}
	}
}

func TestCheckOneResultFields(t *testing.T) {
	// node may or may not be installed; just verify field consistency.
	r := CheckOne("node")
	if r.Name != "node" {
		t.Errorf("Name: got %q, want %q", r.Name, "node")
	}
	if r.Required != prereqSpecs["node"].required {
		t.Errorf("Required: got %v, want %v", r.Required, prereqSpecs["node"].required)
	}
	if r.Found && r.Version == "" {
		t.Error("if Found is true, Version should not be empty")
	}
	if !r.Found && r.Version != "" {
		t.Error("if Found is false, Version should be empty")
	}
}
