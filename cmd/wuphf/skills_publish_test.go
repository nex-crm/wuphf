package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/skillpublish"
	"github.com/nex-crm/wuphf/internal/team"
)

// sampleSkillMarkdown produces a minimal but valid SKILL.md for tests. Mirrors
// the canonical Anthropic format so RenderSkillMarkdown / ParseSkillMarkdown
// both round-trip cleanly.
func sampleSkillMarkdown(t *testing.T, name, desc string) []byte {
	t.Helper()
	fm := team.SkillFrontmatter{
		Name:        name,
		Description: desc,
		Version:     "1.0.0",
		License:     "MIT",
	}
	out, err := team.RenderSkillMarkdown(fm, "## Steps\n\n1. Read context\n2. Act\n")
	if err != nil {
		t.Fatalf("RenderSkillMarkdown: %v", err)
	}
	return out
}

// TestBuildManifest validates that a parsed SKILL.md produces a manifest with
// every load-bearing field preserved. This is the primary unit test for the
// publish path.
func TestBuildManifest(t *testing.T) {
	t.Parallel()
	md := sampleSkillMarkdown(t, "deploy-frontend", "Ship the frontend.")
	fm, body, err := team.ParseSkillMarkdown(md)
	if err != nil {
		t.Fatalf("ParseSkillMarkdown: %v", err)
	}
	manifest, err := skillpublish.BuildManifest(skillpublishFrontmatterFromTeam(fm), body, "wuphf-pr4-publish", time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	if manifest.Name != "deploy-frontend" {
		t.Fatalf("name: got %q want deploy-frontend", manifest.Name)
	}
	if manifest.Description != "Ship the frontend." {
		t.Fatalf("description: got %q", manifest.Description)
	}
	if manifest.Version != "1.0.0" {
		t.Fatalf("version: got %q", manifest.Version)
	}
	if manifest.License != "MIT" {
		t.Fatalf("license: got %q", manifest.License)
	}
	if !strings.Contains(manifest.Body, "## Steps") {
		t.Fatalf("body should contain step block; got %q", manifest.Body)
	}
	if manifest.Source != "wuphf-wuphf-pr4-publish-deploy-frontend" {
		t.Fatalf("source: got %q", manifest.Source)
	}
	if manifest.PublishedAt != "2026-04-28T00:00:00Z" {
		t.Fatalf("published_at: got %q", manifest.PublishedAt)
	}
}

// TestHubURL_Anthropics validates the canonical anthropics hub URL.
func TestHubURL_Anthropics(t *testing.T) {
	t.Parallel()
	got, err := skillpublish.HubURL("anthropics", "deploy-frontend")
	if err != nil {
		t.Fatalf("HubURL: %v", err)
	}
	want := "https://raw.githubusercontent.com/anthropics/skills/main/skills/deploy-frontend/SKILL.md"
	if got != want {
		t.Fatalf("HubURL anthropics:\n got: %s\nwant: %s", got, want)
	}
}

// TestHubURL_GithubScheme validates the github:owner/repo escape hatch.
func TestHubURL_GithubScheme(t *testing.T) {
	t.Parallel()
	got, err := skillpublish.HubURL("github:nex-crm/wuphf-skills", "review-pr")
	if err != nil {
		t.Fatalf("HubURL: %v", err)
	}
	want := "https://raw.githubusercontent.com/nex-crm/wuphf-skills/main/skills/review-pr/SKILL.md"
	if got != want {
		t.Fatalf("HubURL github scheme:\n got: %s\nwant: %s", got, want)
	}
}

// TestHubURL_Unknown ensures we surface a clear error for unknown hubs.
func TestHubURL_Unknown(t *testing.T) {
	t.Parallel()
	if _, err := skillpublish.HubURL("nonexistent-hub", "deploy-frontend"); err == nil {
		t.Fatalf("expected error for unknown hub")
	}
}

// TestResolveSkillPath_LiteralPath confirms a literal markdown path is
// accepted as-is when it points at an existing file.
func TestResolveSkillPath_LiteralPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "deploy-frontend.md")
	if err := os.WriteFile(path, sampleSkillMarkdown(t, "deploy-frontend", "ship it"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := resolveSkillPath(path)
	if err != nil {
		t.Fatalf("resolveSkillPath: %v", err)
	}
	want, _ := filepath.Abs(path)
	if got != want {
		t.Fatalf("resolveSkillPath: got %s want %s", got, want)
	}
}

// TestResolveSkillPath_Slug walks the wiki resolution path: passing a bare
// slug must surface the expected `<wikiRoot>/team/skills/<slug>.md` layout.
// Uses t.Setenv so cannot run in parallel with TestResolveSkillPath_Missing.
func TestResolveSkillPath_Slug(t *testing.T) {
	wikiHome := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", wikiHome)
	skillsDir := filepath.Join(wikiHome, ".wuphf", "wiki", "team", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillFile := filepath.Join(skillsDir, "daily-digest.md")
	if err := os.WriteFile(skillFile, sampleSkillMarkdown(t, "daily-digest", "do it daily"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := resolveSkillPath("daily-digest")
	if err != nil {
		t.Fatalf("resolveSkillPath: %v", err)
	}
	if got != skillFile {
		t.Fatalf("resolveSkillPath: got %s want %s", got, skillFile)
	}
}

// TestResolveSkillPath_Missing surfaces a clear error when neither slug nor
// path resolves. Uses t.Setenv so cannot run in parallel.
func TestResolveSkillPath_Missing(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	if _, err := resolveSkillPath("does-not-exist"); err == nil {
		t.Fatalf("expected error for missing slug")
	}
	if _, err := resolveSkillPath("/tmp/wuphf-test-nope.md"); err == nil {
		t.Fatalf("expected error for missing path")
	}
	if _, err := resolveSkillPath("  "); err == nil {
		t.Fatalf("expected error for blank input")
	}
}

// TestSanitizeHubLabel produces a stable broker-side identifier per source.
func TestSanitizeHubLabel(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"anthropics", "anthropics"},
		{"claude-marketplace", "claude-marketplace"},
		{"github:nex-crm/wuphf-skills", "github-nex-crm-wuphf-skills"},
		{"  Lobehub  ", "lobehub"},
	}
	for _, tc := range cases {
		got := sanitizeHubLabel(tc.in)
		if got != tc.want {
			t.Fatalf("sanitizeHubLabel(%q): got %q want %q", tc.in, got, tc.want)
		}
	}
}

// TestBuildPublishPRBody surfaces the manifest details + caller-provided
// extra prose in a stable order.
func TestBuildPublishPRBody(t *testing.T) {
	t.Parallel()
	manifest := skillpublish.Manifest{
		Name:        "deploy-frontend",
		Description: "Ship the frontend.",
		Version:     "1.2.3",
		License:     "MIT",
		Source:      "wuphf-team-deploy-frontend",
		PublishedAt: "2026-04-28T00:00:00Z",
	}
	body := buildPublishPRBody(manifest, "Cheers from team WUPHF.")
	checks := []string{
		"Ship the frontend.",
		"`deploy-frontend` (v1.2.3, MIT)",
		"`wuphf-team-deploy-frontend`",
		"2026-04-28T00:00:00Z",
		"Cheers from team WUPHF.",
	}
	for _, c := range checks {
		if !strings.Contains(body, c) {
			t.Fatalf("publish PR body missing %q\nbody: %s", c, body)
		}
	}
}
