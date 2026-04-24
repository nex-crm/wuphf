package migration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/gitexec"
	"github.com/nex-crm/wuphf/internal/team"
)

// wikiWorkerWriter adapts team.WikiWorker onto WikiWriter so the
// integration test can drive the real write path without the worker
// needing to know about migration.WikiWriter directly.
type wikiWorkerWriter struct{ w *team.WikiWorker }

func (ww wikiWorkerWriter) Enqueue(ctx context.Context, slug, path, content, mode, commitMsg string) (string, int, error) {
	return ww.w.Enqueue(ctx, slug, path, content, mode, commitMsg)
}
func (ww wikiWorkerWriter) Root() string { return ww.w.Repo().Root() }

// fixedAdapter is a deterministic adapter used by the integration test.
type fixedAdapter struct{ records []MigrationRecord }

func (f *fixedAdapter) Iter(ctx context.Context) (<-chan MigrationRecord, error) {
	ch := make(chan MigrationRecord, len(f.records))
	for _, r := range f.records {
		ch <- r
	}
	close(ch)
	return ch, nil
}

// inMemoryWriter captures Enqueue calls without touching git. Used by
// dry-run and plan-level tests that don't need the full wiki pipeline.
type inMemoryWriter struct {
	mu       sync.Mutex
	existing map[string][]byte
	root     string
	calls    []inMemoryCall
}

type inMemoryCall struct {
	Slug, Path, Content, Mode, Msg string
}

func newInMemoryWriter(t *testing.T) *inMemoryWriter {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "team"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return &inMemoryWriter{existing: map[string][]byte{}, root: root}
}

func (w *inMemoryWriter) Root() string { return w.root }

func (w *inMemoryWriter) Enqueue(ctx context.Context, slug, path, content, mode, msg string) (string, int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, inMemoryCall{slug, path, content, mode, msg})
	full := filepath.Join(w.root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return "", 0, err
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		return "", 0, err
	}
	return "abc1234", len(content), nil
}

func (w *inMemoryWriter) seedExisting(t *testing.T, relPath, content string) {
	t.Helper()
	full := filepath.Join(w.root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Nazz", "nazz"},
		{"Nazz Mohammad!", "nazz-mohammad"},
		{"  trim me  ", "trim-me"},
		{"---leading---", "leading"},
		{"emoji 👋 dropped", "emoji-dropped"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderArticleIncludesProvenanceHeader(t *testing.T) {
	rec := MigrationRecord{
		Kind:    KindPeople,
		Slug:    "nazz",
		Title:   "Nazz",
		Content: "Founder of WUPHF.",
		Source:  "nex",
	}
	now := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	got := renderArticle(rec, now)
	if !strings.Contains(got, "# Nazz") {
		t.Errorf("missing title heading: %q", got)
	}
	if !strings.Contains(got, "Imported from nex") {
		t.Errorf("missing provenance line: %q", got)
	}
}

func TestMigratorDryRunSkipsCommit(t *testing.T) {
	w := newInMemoryWriter(t)
	m := NewMigrator(w)
	m.Stderr = &bytes.Buffer{}
	m.now = func() time.Time { return time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC) }

	adapter := &fixedAdapter{records: []MigrationRecord{
		{Kind: KindPeople, Slug: "nazz", Title: "Nazz", Content: "founder", Source: "nex"},
	}}
	summary, err := m.Run(context.Background(), adapter, RunOptions{DryRun: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(w.calls) != 0 {
		t.Errorf("dry run issued %d commits, want 0", len(w.calls))
	}
	if summary.Written != 0 {
		t.Errorf("written=%d, want 0 on dry run", summary.Written)
	}
	if len(summary.Plans) != 1 {
		t.Fatalf("plans=%d, want 1", len(summary.Plans))
	}
	if summary.Plans[0].Path != "team/people/nazz.md" {
		t.Errorf("path=%q", summary.Plans[0].Path)
	}
	if summary.Plans[0].Action != "create" {
		t.Errorf("action=%q", summary.Plans[0].Action)
	}
}

func TestMigratorSkipsIdenticalContent(t *testing.T) {
	w := newInMemoryWriter(t)
	m := NewMigrator(w)
	m.Stderr = &bytes.Buffer{}
	fixedNow := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixedNow }

	rec := MigrationRecord{Kind: KindPeople, Slug: "nazz", Title: "Nazz", Content: "founder", Source: "nex"}
	// Pre-seed with the exact rendered output so the dedup check fires.
	w.seedExisting(t, "team/people/nazz.md", renderArticle(rec, fixedNow))

	summary, err := m.Run(context.Background(), &fixedAdapter{records: []MigrationRecord{rec}}, RunOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary.Skipped != 1 {
		t.Errorf("skipped=%d, want 1", summary.Skipped)
	}
	if summary.Written != 0 {
		t.Errorf("written=%d, want 0", summary.Written)
	}
	if len(w.calls) != 0 {
		t.Errorf("enqueued %d calls, want 0", len(w.calls))
	}
}

func TestMigratorRenamesOnCollision(t *testing.T) {
	w := newInMemoryWriter(t)
	m := NewMigrator(w)
	m.Stderr = &bytes.Buffer{}
	fixedNow := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixedNow }

	w.seedExisting(t, "team/people/nazz.md", "# Nazz\n\nSomething else entirely.\n")

	rec := MigrationRecord{Kind: KindPeople, Slug: "nazz", Title: "Nazz", Content: "founder", Source: "gbrain"}
	summary, err := m.Run(context.Background(), &fixedAdapter{records: []MigrationRecord{rec}}, RunOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary.Collisions != 1 {
		t.Errorf("collisions=%d, want 1", summary.Collisions)
	}
	if summary.Written != 1 {
		t.Errorf("written=%d, want 1", summary.Written)
	}
	if len(w.calls) != 1 {
		t.Fatalf("want 1 enqueue, got %d", len(w.calls))
	}
	got := w.calls[0].Path
	if !strings.HasPrefix(got, "team/people/nazz-from-gbrain-") || !strings.HasSuffix(got, ".md") {
		t.Errorf("collision path=%q, want suffixed rename", got)
	}
}

func TestMigratorRespectsLimit(t *testing.T) {
	w := newInMemoryWriter(t)
	m := NewMigrator(w)
	m.Stderr = &bytes.Buffer{}
	m.now = func() time.Time { return time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC) }

	records := []MigrationRecord{
		{Kind: KindPeople, Slug: "a", Title: "A", Content: "a", Source: "nex"},
		{Kind: KindPeople, Slug: "b", Title: "B", Content: "b", Source: "nex"},
		{Kind: KindPeople, Slug: "c", Title: "C", Content: "c", Source: "nex"},
	}
	summary, err := m.Run(context.Background(), &fixedAdapter{records: records}, RunOptions{Limit: 2})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary.Written != 2 {
		t.Errorf("written=%d, want 2", summary.Written)
	}
}

// TestMigratorIntegrationWithWikiWorker verifies the migration lands in
// the real wiki git repo with the `migrate` author identity.
func TestMigratorIntegrationWithWikiWorker(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH, skipping integration test")
	}
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := team.NewRepoAt(root, backup)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init wiki: %v", err)
	}
	worker := team.NewWikiWorker(repo, nil)
	worker.Start(ctx)
	defer worker.Stop()

	m := NewMigrator(wikiWorkerWriter{w: worker})
	m.Stderr = &bytes.Buffer{}
	m.now = func() time.Time { return time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC) }

	adapter := &fixedAdapter{records: []MigrationRecord{
		{Kind: KindPeople, Slug: "nazz", Title: "Nazz", Content: "Founder.", Source: "nex"},
		{Kind: KindCompanies, Slug: "hubspot", Title: "HubSpot", Content: "Prior life.", Source: "gbrain"},
	}}
	summary, err := m.Run(ctx, adapter, RunOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary.Written != 2 {
		t.Fatalf("written=%d, want 2", summary.Written)
	}

	// Wait for the async worker to flush both commits. worker.Enqueue
	// waits for the reply, so by the time Run returns the files should
	// be on disk — but give the OS a small buffer.
	for _, rel := range []string{"team/people/nazz.md", "team/companies/hubspot.md"} {
		full := filepath.Join(root, rel)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(full); err == nil {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if _, err := os.Stat(full); err != nil {
			t.Fatalf("expected article at %s: %v", full, err)
		}
	}

	// Confirm commit author == migrate. Clean the subprocess env so this
	// assertion is not hijacked by an inherited GIT_DIR (e.g. when the test
	// runs under a pre-push hook — git exports GIT_DIR pointing at the
	// outer repo and `git log` would return the outer repo's history
	// instead of this test's fixture.
	gitLog := exec.Command("git", "-C", root, "log", "--format=%an <%ae>")
	gitLog.Env = gitexec.CleanEnv()
	out, err := gitLog.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(string(out), "migrate <migrate@wuphf.local>") {
		t.Fatalf("expected migrate author in log, got:\n%s", string(out))
	}
}
