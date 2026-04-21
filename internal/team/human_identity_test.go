package team

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeriveSlug(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"sarah.chen@acme.com", "sarah-chen"},
		{"Sarah.Chen@Acme.com", "sarah-chen"},
		{"founder+work@example.io", "founder-work"},
		{"a_b.c@x.y", "a-b-c"},
		{"CAPS@example.com", "caps"},
		{"---weird.local---@ex.com", "weird-local"},
	}
	for _, c := range cases {
		got := deriveSlug(c.in)
		if got != c.want {
			t.Errorf("deriveSlug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRegistryPersistAndList — writing an identity twice is idempotent
// (no duplicate entry), and List surfaces whatever has been persisted.
func TestRegistryPersistAndList(t *testing.T) {
	dir := t.TempDir()
	reg := NewHumanIdentityRegistryAt(dir)

	id1, err := reg.Observe("Sarah Chen", "sarah.chen@acme.com")
	if err != nil {
		t.Fatalf("observe 1: %v", err)
	}
	if id1.Slug != "sarah-chen" {
		t.Errorf("want slug sarah-chen, got %q", id1.Slug)
	}

	// Duplicate Observe must not create a new file.
	_, err = reg.Observe("Sarah Chen", "sarah.chen@acme.com")
	if err != nil {
		t.Fatalf("observe duplicate: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("want 1 on-disk entry, got %d: %v", len(entries), entries)
	}

	// A different email grows the registry.
	if _, err := reg.Observe("Dwight Schrute", "dwight@dunder.com"); err != nil {
		t.Fatalf("observe 2: %v", err)
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("want 2 on-disk entries, got %d", len(entries))
	}

	found := reg.List()
	if len(found) != 2 {
		t.Fatalf("want 2 listed, got %d: %+v", len(found), found)
	}

	// Lookup by slug returns the cached entry.
	if id, ok := reg.Lookup("sarah-chen"); !ok || id.Email != "sarah.chen@acme.com" {
		t.Errorf("lookup sarah-chen: got %+v ok=%v", id, ok)
	}
	if _, ok := reg.Lookup("missing"); ok {
		t.Error("lookup missing should return false")
	}
}

// TestRegistryFallbackWhenGitConfigMissing — when git config is unset,
// Local() returns FallbackHumanIdentity and does NOT persist it (so a
// later probe with real config can still land).
func TestRegistryFallbackWhenGitConfigMissing(t *testing.T) {
	dir := t.TempDir()
	reg := NewHumanIdentityRegistryAt(dir)
	// Force the probe to fail by pointing git at empty config files.
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	id := reg.Local()
	if id.Slug != HumanAuthor {
		t.Errorf("want fallback slug %q, got %q", HumanAuthor, id.Slug)
	}
	if id.Email != FallbackHumanIdentity.Email {
		t.Errorf("want fallback email, got %q", id.Email)
	}
	// Fallback identity must NOT write to disk — otherwise a later
	// config-based probe would be masked by the cached fallback.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Errorf("fallback identity should not persist, found %s", e.Name())
		}
	}
}

// TestCommitHumanUsesRegistryIdentity — a write with a real identity
// lands on the commit's author metadata.
func TestCommitHumanUsesRegistryIdentity(t *testing.T) {
	worker, repo, _, teardown := newStartedWorker(t)
	defer teardown()

	id, err := buildIdentity("Sarah Chen", "sarah.chen@acme.com")
	if err != nil {
		t.Fatalf("build identity: %v", err)
	}

	sha, _, err := worker.EnqueueHumanAs(
		context.Background(), id,
		"team/people/sarah.md",
		"# Sarah\n\nFounding PM.\n",
		"human: add sarah",
		"",
	)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if sha == "" {
		t.Fatal("expected sha")
	}

	out, err := repo.runGitLocked(context.Background(), "system",
		"log", "-n", "1", "--format=%an\x1f%ae", "--", "team/people/sarah.md")
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(out), "\x1f")
	if len(parts) != 2 {
		t.Fatalf("unexpected git log output: %q", out)
	}
	if parts[0] != "Sarah Chen" {
		t.Errorf("want name 'Sarah Chen', got %q", parts[0])
	}
	if parts[1] != "sarah.chen@acme.com" {
		t.Errorf("want email sarah.chen@acme.com, got %q", parts[1])
	}
}

// TestHandleHumansReturnsRegistry — the /humans endpoint exposes the
// registered identities the UI uses for byline lookups.
func TestHandleHumansReturnsRegistry(t *testing.T) {
	dir := t.TempDir()
	reg := NewHumanIdentityRegistryAt(dir)
	if _, err := reg.Observe("Sarah Chen", "sarah.chen@acme.com"); err != nil {
		t.Fatalf("observe: %v", err)
	}
	setHumanIdentityRegistry(reg)
	// Restore so other tests don't leak this fake registry.
	t.Cleanup(func() { setHumanIdentityRegistry(NewHumanIdentityRegistry()) })

	b := &Broker{}
	req := httptest.NewRequest(http.MethodGet, "/humans", nil)
	rec := httptest.NewRecorder()
	b.handleHumans(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Humans []humanIdentityResponse `json:"humans"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found bool
	for _, h := range got.Humans {
		if h.Slug == "sarah-chen" && h.Name == "Sarah Chen" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected sarah-chen in /humans, got %+v", got.Humans)
	}
}

// TestHandleWikiWriteHumanReturnsAuthorSlug — the write handler surfaces
// the slug that actually authored the commit so the UI can update the
// byline without a second fetch.
func TestHandleWikiWriteHumanReturnsAuthorSlug(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()

	dir := t.TempDir()
	reg := NewHumanIdentityRegistryAt(dir)
	if _, err := reg.Observe("Sarah Chen", "sarah.chen@acme.com"); err != nil {
		t.Fatalf("observe: %v", err)
	}
	// Seed the local probe result so the handler picks sarah-chen instead
	// of probing the developer's real git config.
	reg.mu.Lock()
	id, _ := buildIdentity("Sarah Chen", "sarah.chen@acme.com")
	reg.localCache = &id
	reg.mu.Unlock()
	setHumanIdentityRegistry(reg)
	t.Cleanup(func() { setHumanIdentityRegistry(NewHumanIdentityRegistry()) })

	b := brokerForTest(t, worker)

	body, _ := json.Marshal(map[string]any{
		"path":           "team/people/wiki-author.md",
		"content":        "# Wiki Author\n\nFirst edit.\n",
		"commit_message": "human: bootstrap",
	})
	req := httptest.NewRequest(http.MethodPost, "/wiki/write-human", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	b.handleWikiWriteHuman(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["author_slug"] != "sarah-chen" {
		t.Errorf("want author_slug=sarah-chen, got %v", got["author_slug"])
	}
}

// TestIdentityFilenameDeterministic — the same email always hashes to the
// same filename, case-insensitively. Prevents duplicate on-disk entries
// for the same human who types their email with different casing.
func TestIdentityFilenameDeterministic(t *testing.T) {
	a := identityFilename("sarah@acme.com")
	b := identityFilename("SARAH@acme.com")
	if a != b {
		t.Errorf("filenames diverge for casing: %s vs %s", a, b)
	}
	if !strings.HasSuffix(a, ".json") {
		t.Errorf("expected .json suffix, got %s", a)
	}
	// Sanity: different emails hash to different names.
	if identityFilename("sarah@acme.com") == identityFilename("dwight@dunder.com") {
		t.Error("different emails produced same filename")
	}
}

// Unused-import guard.
var _ = filepath.Separator
