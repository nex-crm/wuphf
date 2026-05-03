package skillpublish

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildManifest_PreservesFrontmatter(t *testing.T) {
	t.Parallel()
	fm := FrontmatterLike{
		Name:        "deploy-frontend",
		Description: "Ship the web build with cache warmup.",
		Version:     "2.4.0",
		License:     "Apache-2.0",
	}
	body := "## Steps\n\n1. bun run build\n2. push to fly\n"
	at := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)

	got, err := BuildManifest(fm, body, "wuphf", at)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != fm.Name {
		t.Fatalf("name: got %q want %q", got.Name, fm.Name)
	}
	if got.Description != fm.Description {
		t.Fatalf("description: got %q want %q", got.Description, fm.Description)
	}
	if got.Version != "2.4.0" {
		t.Fatalf("version: got %q want %q", got.Version, "2.4.0")
	}
	if got.License != "Apache-2.0" {
		t.Fatalf("license: got %q want %q", got.License, "Apache-2.0")
	}
	if got.Body != strings.TrimSpace(body) {
		t.Fatalf("body: got %q want %q", got.Body, strings.TrimSpace(body))
	}
	if got.Source != "wuphf-wuphf-deploy-frontend" {
		t.Fatalf("source: got %q want %q", got.Source, "wuphf-wuphf-deploy-frontend")
	}
	if got.PublishedAt != "2026-04-28T12:00:00Z" {
		t.Fatalf("published_at: got %q", got.PublishedAt)
	}
}

func TestBuildManifest_DefaultsLicenseAndVersion(t *testing.T) {
	t.Parallel()
	fm := FrontmatterLike{Name: "x", Description: "y"}
	got, err := BuildManifest(fm, "body", "", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.License != "MIT" {
		t.Fatalf("license default: got %q want MIT", got.License)
	}
	if got.Version != "1.0.0" {
		t.Fatalf("version default: got %q want 1.0.0", got.Version)
	}
	if got.Source != "wuphf-x" {
		t.Fatalf("source without repo: got %q want wuphf-x", got.Source)
	}
	if got.PublishedAt == "" {
		t.Fatalf("published_at should be auto-stamped when zero time supplied")
	}
}

func TestBuildManifest_RequiresNameAndDescription(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		fm   FrontmatterLike
	}{
		{"missing name", FrontmatterLike{Description: "desc"}},
		{"missing description", FrontmatterLike{Name: "x"}},
		{"whitespace name", FrontmatterLike{Name: "   ", Description: "desc"}},
		{"whitespace description", FrontmatterLike{Name: "x", Description: "   "}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := BuildManifest(tc.fm, "body", "wuphf", time.Now()); err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

// TestBuildManifest_RejectsPathTraversalName pins the name validator
// added to defend against path-traversal attacks. Name flows into
// HubFilePath -> filepath.Join, so a "../../etc/x"-shaped name would
// write outside the hub clone directory. The validator must reject
// anything that doesn't match ^[a-z0-9][a-z0-9-]{0,63}$.
func TestBuildManifest_RejectsPathTraversalName(t *testing.T) {
	t.Parallel()
	cases := []string{
		"../../etc/passwd",
		"web/../research",
		"web research",           // space
		"web.research",           // dot
		"WEB-RESEARCH",           // uppercase
		"-leading-dash",          // leading dash
		strings.Repeat("a", 100), // exceeds 64-char length cap
		"name/with/slash",
		"",
	}
	for _, name := range cases {
		t.Run("reject_"+name, func(t *testing.T) {
			fm := FrontmatterLike{Name: name, Description: "x"}
			_, err := BuildManifest(fm, "body", "repo", time.Time{})
			if err == nil {
				t.Fatalf("expected error for name %q, got nil", name)
			}
		})
	}
	// And the canonical valid shape must still pass.
	fm := FrontmatterLike{Name: "web-research", Description: "Search the web."}
	if _, err := BuildManifest(fm, "body", "repo", time.Time{}); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
}

func TestBuildManifest_RoundTrip(t *testing.T) {
	t.Parallel()
	fm := FrontmatterLike{
		Name:        "web-research",
		Description: "Search the web",
		Version:     "1.2.3",
		License:     "MIT",
	}
	at := time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC)
	original, err := BuildManifest(fm, "do things", "myrepo", at)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var roundTripped Manifest
	if err := json.Unmarshal(raw, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if roundTripped != original {
		t.Fatalf("round trip mismatch:\noriginal: %+v\nround   : %+v", original, roundTripped)
	}
}

func TestHubURL_Anthropics(t *testing.T) {
	t.Parallel()
	got, err := HubURL("anthropics", "deploy-frontend")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://raw.githubusercontent.com/anthropics/skills/main/skills/deploy-frontend/SKILL.md"
	if got != want {
		t.Fatalf("HubURL anthropics:\n got: %s\nwant: %s", got, want)
	}
}

func TestHubURL_LobeHub(t *testing.T) {
	t.Parallel()
	got, err := HubURL("lobehub", "agent-foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://raw.githubusercontent.com/lobehub/lobe-chat-agents/main/agents/agent-foo.md"
	if got != want {
		t.Fatalf("HubURL lobehub:\n got: %s\nwant: %s", got, want)
	}
}

func TestHubURL_GithubScheme(t *testing.T) {
	t.Parallel()
	got, err := HubURL("github:nex-crm/wuphf-skills", "review-pr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://raw.githubusercontent.com/nex-crm/wuphf-skills/main/skills/review-pr/SKILL.md"
	if got != want {
		t.Fatalf("HubURL github scheme:\n got: %s\nwant: %s", got, want)
	}
}

func TestHubURL_Unknown(t *testing.T) {
	t.Parallel()
	if _, err := HubURL("bogushub", "x"); err == nil {
		t.Fatalf("expected error for unknown hub")
	}
}

func TestHubURL_GithubScheme_Malformed(t *testing.T) {
	t.Parallel()
	cases := []string{
		"github:",
		"github:owner",
		"github:owner/",
		"github:/repo",
	}
	for _, hub := range cases {
		if _, err := HubURL(hub, "name"); err == nil {
			t.Fatalf("expected error for malformed hub %q", hub)
		}
	}
}

func TestHubURL_RequiresName(t *testing.T) {
	t.Parallel()
	if _, err := HubURL("anthropics", "  "); err == nil {
		t.Fatalf("expected error for blank name")
	}
}

func TestHubFilePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		hub  string
		skl  string
		want string
	}{
		{"anthropics", "anthropics", "x", "skills/x/SKILL.md"},
		{"lobehub", "lobehub", "x", "agents/x.md"},
		{"github scheme", "github:foo/bar", "x", "skills/x/SKILL.md"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := HubFilePath(tc.hub, tc.skl)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("HubFilePath: got %s want %s", got, tc.want)
			}
		})
	}
}

func TestHubFilePath_Unknown(t *testing.T) {
	t.Parallel()
	if _, err := HubFilePath("nope", "x"); err == nil {
		t.Fatalf("expected error for unknown hub")
	}
}

func TestHubRepo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		hub  string
		want string
	}{
		{"anthropics", "anthropics/skills"},
		{"lobehub", "lobehub/lobe-chat-agents"},
		{"github:foo/bar", "foo/bar"},
	}
	for _, tc := range cases {
		got, err := HubRepo(tc.hub)
		if err != nil {
			t.Fatalf("HubRepo(%q): unexpected error: %v", tc.hub, err)
		}
		if got != tc.want {
			t.Fatalf("HubRepo(%q): got %s want %s", tc.hub, got, tc.want)
		}
	}
}

func TestHubRepo_Unknown(t *testing.T) {
	t.Parallel()
	if _, err := HubRepo("xyz"); err == nil {
		t.Fatalf("expected error for unknown hub")
	}
}

func TestHubBaseBranch(t *testing.T) {
	t.Parallel()
	if got := HubBaseBranch("anthropics"); got != "main" {
		t.Fatalf("HubBaseBranch: got %s want main", got)
	}
}

func TestPublishBranchName(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	got := PublishBranchName("Deploy Frontend", at)
	want := "wuphf-publish-deploy-frontend-1777377600"
	if got != want {
		t.Fatalf("PublishBranchName: got %s want %s", got, want)
	}
}

func TestIsGitHubScheme(t *testing.T) {
	t.Parallel()
	if !IsGitHubScheme("github:foo/bar") {
		t.Fatalf("expected github:foo/bar to be a github scheme")
	}
	if IsGitHubScheme("anthropics") {
		t.Fatalf("anthropics should not be a github scheme")
	}
}

func TestSanitizeSourceSegment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"Deploy Frontend", "deploy-frontend"},
		{"  Trim  ", "trim"},
		{"a/b/c", "a-b-c"},
		{"weird!!!chars", "weirdchars"},
		{"under_score", "under-score"},
		{"---", ""},
	}
	for _, tc := range cases {
		got := sanitizeSourceSegment(tc.in)
		if got != tc.want {
			t.Fatalf("sanitizeSourceSegment(%q): got %q want %q", tc.in, got, tc.want)
		}
	}
}
