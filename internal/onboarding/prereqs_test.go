package onboarding

import (
	"os"
	"path/filepath"
	"strings"
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

func TestCheckAllReturnsSevenItems(t *testing.T) {
	results := CheckAll()
	if len(results) != 7 {
		t.Fatalf("CheckAll: got %d results, want 7", len(results))
	}
	names := []string{"node", "git", "claude", "codex", "opencode", "cursor", "windsurf"}
	for i, r := range results {
		if r.Name != names[i] {
			t.Errorf("CheckAll[%d].Name: got %q, want %q", i, r.Name, names[i])
		}
	}
}

func TestCheckAllRequiredFlags(t *testing.T) {
	// node and git are required (infrastructure).
	// claude, codex, opencode, cursor, windsurf are optional — the user picks runtime CLIs.
	wantRequired := map[string]bool{
		"node":     true,
		"git":      true,
		"claude":   false,
		"codex":    false,
		"opencode": false,
		"cursor":   false,
		"windsurf": false,
	}
	for _, r := range CheckAll() {
		want, ok := wantRequired[r.Name]
		if !ok {
			continue
		}
		if r.Required != want {
			t.Errorf("%s: Required: got %v, want %v", r.Name, r.Required, want)
		}
	}
}

func TestCheckAllInstallURLs(t *testing.T) {
	wantURLs := map[string]string{
		"node":     "https://nodejs.org",
		"git":      "https://git-scm.com",
		"claude":   "https://claude.ai/code",
		"codex":    "https://github.com/openai/codex",
		"opencode": "https://opencode.ai",
		"cursor":   "https://cursor.com/",
		"windsurf": "https://codeium.com/windsurf",
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

func TestCheckOneFindsOpencodeInCommonUserBinWhenPATHIsMinimal(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, ".opencode", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir opencode bin: %v", err)
	}
	opencodePath := filepath.Join(binDir, "opencode")
	if err := os.WriteFile(opencodePath, []byte("#!/bin/sh\nprintf 'opencode 9.9.9\\n'\n"), 0o755); err != nil {
		t.Fatalf("write fake opencode: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "minimal-path"))

	r := CheckOne("opencode")
	if !r.Found {
		t.Fatal("expected fallback opencode to be found")
	}
	if r.Version != "opencode 9.9.9" {
		t.Fatalf("Version: got %q, want %q", r.Version, "opencode 9.9.9")
	}
	if got := os.Getenv("PATH"); !strings.Contains(got, binDir) {
		t.Fatalf("expected PATH to include discovered opencode dir, got %q", got)
	}
}
