package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
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

// TestHubURL_GithubScheme_BranchOverride validates the branch override escape
// hatch for custom repos whose default branch is not main.
func TestHubURL_GithubScheme_BranchOverride(t *testing.T) {
	t.Parallel()
	got, err := skillpublish.HubURL("github:nex-crm/wuphf-skills@master", "review-pr")
	if err != nil {
		t.Fatalf("HubURL: %v", err)
	}
	want := "https://raw.githubusercontent.com/nex-crm/wuphf-skills/master/skills/review-pr/SKILL.md"
	if got != want {
		t.Fatalf("HubURL github scheme branch:\n got: %s\nwant: %s", got, want)
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
		{"github:nex-crm/wuphf-skills", "github-nex-crm-wuphf-skills"},
		{"github:nex-crm/wuphf-skills@master", "github-nex-crm-wuphf-skills-master"},
		{"  Lobehub  ", "lobehub"},
	}
	for _, tc := range cases {
		got := sanitizeHubLabel(tc.in)
		if got != tc.want {
			t.Fatalf("sanitizeHubLabel(%q): got %q want %q", tc.in, got, tc.want)
		}
	}
}

// TestBuildPublishPRBody_NoCWDLeak guards against the privacy regression:
// the PR body lands in a public hub repo, so it must not include the
// user's local working directory ("Workspace: /Users/<name>/...").
// Source provenance is captured by m.Source which is sanitised to
// [a-z0-9-].
func TestBuildPublishPRBody_NoCWDLeak(t *testing.T) {
	t.Parallel()
	manifest := skillpublish.Manifest{
		Name:        "deploy-frontend",
		Description: "Ship the frontend.",
		Version:     "1.0.0",
		License:     "MIT",
		Source:      "wuphf-team-deploy-frontend",
		PublishedAt: "2026-04-28T00:00:00Z",
	}
	body := buildPublishPRBody(manifest, "")
	if strings.Contains(body, "Workspace:") {
		t.Errorf("PR body must not contain Workspace field (privacy: leaks cwd to public hub repo); got:\n%s", body)
	}
	cwd, _ := os.Getwd()
	if cwd != "" && strings.Contains(body, cwd) {
		t.Errorf("PR body contains the test runner's cwd %q; got:\n%s", cwd, body)
	}
}

// TestPostBrokerSkill_RoundTrip pins the broker round-trip the install
// path uses end-to-end. Without this, a regression like the previous
// `action: "propose"` 403 would not be caught until manual smoke. We
// stand up an httptest.Server that mimics the broker's POST /skills
// endpoint, point WUPHF_BROKER_BASE_URL at it, and assert the request
// shape the install path emits.
func TestPostBrokerSkill_RoundTrip(t *testing.T) {
	var captured struct {
		method      string
		contentType string
		auth        string
		payload     map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/skills" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		captured.method = r.Method
		captured.contentType = r.Header.Get("Content-Type")
		captured.auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured.payload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"skill-x"}`))
	}))
	defer srv.Close()

	t.Setenv("WUPHF_BROKER_BASE_URL", srv.URL)
	t.Setenv("WUPHF_BROKER_TOKEN", "test-token")

	payload := map[string]any{
		"action":      "create",
		"name":        "deploy-frontend",
		"title":       "deploy-frontend",
		"description": "Ship it.",
		"content":     "## Steps\n",
		"created_by":  "hub:anthropics",
		"channel":     "skills",
	}
	if err := postBrokerSkill(context.Background(), payload); err != nil {
		t.Fatalf("postBrokerSkill: %v", err)
	}

	if captured.method != http.MethodPost {
		t.Errorf("method: got %q want POST", captured.method)
	}
	if captured.contentType != "application/json" {
		t.Errorf("content-type: got %q", captured.contentType)
	}
	if captured.auth != "Bearer test-token" {
		t.Errorf("auth: got %q want Bearer test-token", captured.auth)
	}
	if got := captured.payload["action"]; got != "create" {
		t.Errorf("action: got %v want create (NOT propose — install IS the human approval; see PR review)", got)
	}
	if got := captured.payload["name"]; got != "deploy-frontend" {
		t.Errorf("name: got %v", got)
	}
}

// TestFetchURL_RejectsRedirectOffGitHub pins the redirect-host guard:
// a malicious `github:` hub could 302 the raw fetch to an
// attacker-controlled host, and the post-install BuildManifest +
// broker POST would then accept content from anywhere. fetchURL must
// refuse any redirect that lands off raw.githubusercontent.com.
func TestFetchURL_RejectsRedirectOffGitHub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example.com/SKILL.md", http.StatusFound)
	}))
	defer srv.Close()

	_, err := fetchURL(context.Background(), srv.URL+"/skills/x/SKILL.md")
	if err == nil {
		t.Fatal("expected error rejecting redirect to evil.example.com, got nil")
	}
	if !strings.Contains(err.Error(), "refusing redirect") {
		t.Errorf("expected 'refusing redirect' in error, got %q", err.Error())
	}
}

// TestFetchURL_RejectsOversizedPayload ensures the size guard refuses, rather
// than truncates, oversized skill payloads.
func TestFetchURL_RejectsOversizedPayload(t *testing.T) {
	tooLarge := strings.Repeat("x", 4*1024*1024+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, tooLarge)
	}))
	defer srv.Close()

	_, err := fetchURL(context.Background(), srv.URL+"/skills/x/SKILL.md")
	if err == nil {
		t.Fatal("expected oversized payload error, got nil")
	}
	if !strings.Contains(err.Error(), "response body exceeds") {
		t.Errorf("expected size error, got %q", err.Error())
	}
}

// TestPostBrokerSkill_4xxSurfacesBody pins the error-shape: a 4xx from
// the broker should bubble up with the response body so the user can see
// the broker's actual rejection reason (e.g. "name already exists" or
// the propose-path 403 we used to hit).
func TestPostBrokerSkill_4xxSurfacesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`skill with this name already exists`))
	}))
	defer srv.Close()

	t.Setenv("WUPHF_BROKER_BASE_URL", srv.URL)
	t.Setenv("WUPHF_BROKER_TOKEN", "")

	err := postBrokerSkill(context.Background(), map[string]any{
		"action": "create", "name": "x", "content": "y", "created_by": "z",
	})
	if err == nil {
		t.Fatal("expected error on 409, got nil")
	}
	if !strings.Contains(err.Error(), "skill with this name already exists") {
		t.Errorf("expected broker body in error, got %q", err.Error())
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

func TestReorderFlagsFirst_BoolFlagDoesNotConsumePositional(t *testing.T) {
	t.Parallel()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Bool("dry-run", false, "")
	fs.String("to", "", "")

	got := reorderFlagsFirst(fs, []string{"--dry-run", "deploy-frontend", "--to", "anthropics"})
	want := []string{"--dry-run", "--to", "anthropics", "deploy-frontend"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("reorderFlagsFirst:\n got: %#v\nwant: %#v", got, want)
	}
}
