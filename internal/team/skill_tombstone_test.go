package team

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// callLoadSkillTombstoneLocked wraps the method with lock acquire/release.
func callLoadSkillTombstoneLocked(b *Broker) ([]SkillTombstoneEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.loadSkillTombstoneLocked()
}

// writeTombstoneFile writes a tombstoneFile directly to disk under root for
// test setup without going through the queue.
func writeTombstoneFile(t *testing.T, root string, entries []SkillTombstoneEntry) {
	t.Helper()
	dir := filepath.Join(root, "team", "skills")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	tf := tombstoneFile{Rejected: entries}
	raw, err := yaml.Marshal(tf)
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}
	path := filepath.Join(dir, ".rejected.md")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write tombstone: %v", err)
	}
}

func TestLoadSkillTombstoneLocked_MissingFile_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	// No wiki worker — loadSkillTombstoneLocked should return nil, nil.
	entries, err := callLoadSkillTombstoneLocked(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(entries))
	}
}

func TestLoadSkillTombstoneLocked_WithWikiWorker_MissingFile_ReturnsEmpty(t *testing.T) {
	// Cannot use t.Parallel() — test calls t.Setenv.
	wikiRoot := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", wikiRoot)

	repo := NewRepoAt(wikiRoot, wikiRoot+".bak")
	if err := repo.Init(t.Context()); err != nil {
		t.Skipf("git not available: %v", err)
	}

	worker := NewWikiWorker(repo, noopPublisher{})
	worker.Start(t.Context())
	t.Cleanup(worker.Stop)

	b := newTestBroker(t)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	entries, err := callLoadSkillTombstoneLocked(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty on missing file, got %d entries", len(entries))
	}
}

func TestLoadSkillTombstoneLocked_ReadsExistingFile(t *testing.T) {
	// Cannot use t.Parallel() — test calls t.Setenv.
	wikiRoot := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", wikiRoot)

	repo := NewRepoAt(wikiRoot, wikiRoot+".bak")
	if err := repo.Init(t.Context()); err != nil {
		t.Skipf("git not available: %v", err)
	}

	want := []SkillTombstoneEntry{
		{Slug: "bad-skill", RejectedAt: "2026-04-28T10:00:00Z", Reason: "dangerous"},
		{Slug: "another-bad", SourceArticle: "team/wiki/process.md", RejectedAt: "2026-04-28T11:00:00Z"},
	}
	writeTombstoneFile(t, wikiRoot, want)

	worker := NewWikiWorker(repo, noopPublisher{})
	worker.Start(t.Context())
	t.Cleanup(worker.Stop)

	b := newTestBroker(t)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	got, err := callLoadSkillTombstoneLocked(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("entry count: got %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Slug != w.Slug {
			t.Errorf("entry[%d].Slug: got %q, want %q", i, got[i].Slug, w.Slug)
		}
		if got[i].Reason != w.Reason {
			t.Errorf("entry[%d].Reason: got %q, want %q", i, got[i].Reason, w.Reason)
		}
		if got[i].SourceArticle != w.SourceArticle {
			t.Errorf("entry[%d].SourceArticle: got %q, want %q", i, got[i].SourceArticle, w.SourceArticle)
		}
	}
}

func TestLoadSkillTombstoneLocked_MalformedFile_ReturnsEmpty(t *testing.T) {
	// Cannot use t.Parallel() — test calls t.Setenv.
	wikiRoot := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", wikiRoot)

	repo := NewRepoAt(wikiRoot, wikiRoot+".bak")
	if err := repo.Init(t.Context()); err != nil {
		t.Skipf("git not available: %v", err)
	}

	// Write garbage YAML.
	dir := filepath.Join(wikiRoot, "team", "skills")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".rejected.md"),
		[]byte("{ this is: [ invalid\n: yaml }"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	worker := NewWikiWorker(repo, noopPublisher{})
	worker.Start(t.Context())
	t.Cleanup(worker.Stop)

	b := newTestBroker(t)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	// Should tolerate malformed YAML and return empty without error.
	got, err := callLoadSkillTombstoneLocked(b)
	if err != nil {
		t.Fatalf("expected nil error on malformed YAML, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty on malformed YAML, got %d entries", len(got))
	}
}

func TestSkillTombstoneEntry_YAMLRoundTrip(t *testing.T) {
	t.Parallel()

	original := SkillTombstoneEntry{
		Slug:          "risky-skill",
		SourceArticle: "team/wiki/risky.md",
		RejectedAt:    "2026-04-28T12:34:56Z",
		Reason:        "dangerous: eval pattern detected",
	}

	tf := tombstoneFile{Rejected: []SkillTombstoneEntry{original}}
	raw, err := yaml.Marshal(tf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded tombstoneFile
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Rejected) != 1 {
		t.Fatalf("entry count: got %d, want 1", len(decoded.Rejected))
	}
	got := decoded.Rejected[0]
	if got.Slug != original.Slug {
		t.Errorf("Slug: got %q, want %q", got.Slug, original.Slug)
	}
	if got.SourceArticle != original.SourceArticle {
		t.Errorf("SourceArticle: got %q, want %q", got.SourceArticle, original.SourceArticle)
	}
	if got.RejectedAt != original.RejectedAt {
		t.Errorf("RejectedAt: got %q, want %q", got.RejectedAt, original.RejectedAt)
	}
	if got.Reason != original.Reason {
		t.Errorf("Reason: got %q, want %q", got.Reason, original.Reason)
	}
}
