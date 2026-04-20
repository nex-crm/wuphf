package team

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	return NewRepoAt(root, backup)
}

func TestRepoInitIsIdempotent(t *testing.T) {
	// Arrange
	repo := newTestRepo(t)
	ctx := context.Background()

	// Act
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("second init: %v", err)
	}

	// Assert
	if _, err := os.Stat(filepath.Join(repo.Root(), ".git")); err != nil {
		t.Fatalf("expected .git to exist: %v", err)
	}
	for _, sub := range []string{"team/people", "team/companies", "team/projects", "team/playbooks", "team/decisions", "team/inbox/raw", "index"} {
		if _, err := os.Stat(filepath.Join(repo.Root(), sub)); err != nil {
			t.Fatalf("expected %s to exist: %v", sub, err)
		}
	}
}

func TestRepoInitDetectsMissingGit(t *testing.T) {
	// Arrange — blank out PATH so git cannot be found.
	t.Setenv("PATH", "")
	repo := newTestRepo(t)

	// Act
	err := repo.Init(context.Background())

	// Assert
	if !errors.Is(err, ErrGitUnavailable) {
		t.Fatalf("expected ErrGitUnavailable, got %v", err)
	}
}

func TestRepoInitPreservesOrphanDir(t *testing.T) {
	// Arrange — wiki exists with content but no .git
	repo := newTestRepo(t)
	if err := os.MkdirAll(repo.Root(), 0o700); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	orphanFile := filepath.Join(repo.Root(), "marker.txt")
	if err := os.WriteFile(orphanFile, []byte("orphan"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	// Act
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Assert — orphan moved aside, new wiki has .git
	if _, err := os.Stat(filepath.Join(repo.Root(), ".git")); err != nil {
		t.Fatalf("expected .git to exist: %v", err)
	}
	parent := filepath.Dir(repo.Root())
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	var foundOrphan bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "wiki.orphan-") {
			foundOrphan = true
		}
	}
	if !foundOrphan {
		t.Fatalf("expected orphan dir, got %+v", entries)
	}
}

func TestRepoCommitRecordsSlugIdentity(t *testing.T) {
	// Arrange
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Act
	sha, bytesWritten, err := repo.Commit(ctx, "ceo", "team/people/nazz.md", "# Nazz\n\nFounder.\n", "create", "add nazz brief")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Assert
	if sha == "" {
		t.Fatal("expected non-empty sha")
	}
	if bytesWritten == 0 {
		t.Fatal("expected bytes written")
	}
	refs, err := repo.Log(ctx, "team/people/nazz.md")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("expected at least one log entry")
	}
	if refs[0].Author != "ceo" {
		t.Fatalf("expected author ceo, got %q", refs[0].Author)
	}
	if !strings.Contains(refs[0].Message, "add nazz brief") {
		t.Fatalf("expected message to contain commit msg, got %q", refs[0].Message)
	}
}

func TestRepoCommitRejectsPathTraversal(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	cases := []string{
		"../etc/passwd",
		"/etc/passwd",
		"team/../../escape.md",
		"notteam/x.md",
		"team/foo.txt",
	}
	for _, p := range cases {
		if _, _, err := repo.Commit(ctx, "ceo", p, "x", "create", "bad"); err == nil {
			t.Fatalf("expected error for %q", p)
		}
	}
}

func TestRepoCommitRejectsEmptyContent(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "ceo", "team/people/x.md", "  \n", "create", "m"); err == nil {
		t.Fatal("expected empty content to be rejected")
	}
}

func TestRepoCommitAppendSection(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "ceo", "team/playbooks/x.md", "# X\n\nfirst", "create", "start"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "eng", "team/playbooks/x.md", "## Step 2\n\nmore", "append_section", "extend"); err != nil {
		t.Fatalf("append: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(repo.Root(), "team/playbooks/x.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "first") || !strings.Contains(string(body), "Step 2") {
		t.Fatalf("expected both sections in %q", string(body))
	}
}

func TestRepoFsckCleanAfterInit(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := repo.Fsck(ctx); err != nil {
		t.Fatalf("fsck should pass, got %v", err)
	}
}

func TestRepoFsckDetectsCorruption(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Corrupt the repo by removing .git
	if err := os.RemoveAll(filepath.Join(repo.Root(), ".git")); err != nil {
		t.Fatalf("rm .git: %v", err)
	}
	err := repo.Fsck(ctx)
	if !errors.Is(err, ErrRepoCorrupt) {
		t.Fatalf("expected ErrRepoCorrupt, got %v", err)
	}
}

func TestRepoIndexRegenProducesValidMarkdown(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "ceo", "team/people/nazz.md", "# Nazz\n\nFounder.\n", "create", "add"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "eng", "team/projects/wiki.md", "# LLM wiki\n", "create", "add"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := repo.IndexRegen(ctx); err != nil {
		t.Fatalf("regen: %v", err)
	}
	body, err := os.ReadFile(repo.IndexAllPath())
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "# Team wiki index") {
		t.Fatalf("expected heading in index, got %q", text)
	}
	if !strings.Contains(text, "Nazz") || !strings.Contains(text, "LLM wiki") {
		t.Fatalf("expected both article titles, got %q", text)
	}
}

func TestRepoBackupMirror(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "ceo", "team/people/nazz.md", "# Nazz\n", "create", "m"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := repo.BackupMirror(ctx); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.BackupRoot(), "team/people/nazz.md")); err != nil {
		t.Fatalf("expected mirrored article: %v", err)
	}
}

func TestRepoRestoreFromBackup(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "ceo", "team/people/nazz.md", "# Nazz\n", "create", "m"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := repo.BackupMirror(ctx); err != nil {
		t.Fatalf("backup: %v", err)
	}
	// Nuke the live repo
	if err := os.RemoveAll(filepath.Join(repo.Root(), ".git")); err != nil {
		t.Fatalf("rm .git: %v", err)
	}
	if err := repo.RestoreFromBackup(ctx); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := repo.Fsck(ctx); err != nil {
		t.Fatalf("fsck after restore: %v", err)
	}
}

func TestRepoRestoreFromBackupMissing(t *testing.T) {
	repo := newTestRepo(t)
	err := repo.RestoreFromBackup(context.Background())
	if !errors.Is(err, ErrBackupMissing) {
		t.Fatalf("expected ErrBackupMissing, got %v", err)
	}
}

func TestRepoRecoverDirtyTree(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Write a file without committing — simulates a crashed write
	path := filepath.Join(repo.Root(), "team/people/crashed.md")
	if err := os.WriteFile(path, []byte("# Crashed\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := repo.RecoverDirtyTree(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	refs, err := repo.Log(ctx, "team/people/crashed.md")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("expected recovery commit in log")
	}
	if refs[0].Author != "wuphf-recovery" {
		t.Fatalf("expected wuphf-recovery author, got %q", refs[0].Author)
	}
}

func TestRepoRecoverDirtyTreeCleanIsNoop(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Nothing dirty — should succeed and not create a new commit
	if err := repo.RecoverDirtyTree(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
}

func TestRepoCommitBootstrapAttributesToBootstrapAuthor(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Simulate MaterializeWiki: drop two skeleton files under team/ without
	// going through Commit(). CommitBootstrap should pick them up and
	// attribute them to wuphf-bootstrap (NOT wuphf-recovery, NOT system).
	if err := os.MkdirAll(filepath.Join(repo.Root(), "team", "playbooks"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skeletons := map[string]string{
		"team/playbooks/renewal.md":        "# Renewal\n",
		"team/decisions/wiki-as-default.md": "# Decision\n",
	}
	for rel, body := range skeletons {
		full := filepath.Join(repo.Root(), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	sha, err := repo.CommitBootstrap(ctx, "wuphf: materialize test blueprint")
	if err != nil {
		t.Fatalf("CommitBootstrap: %v", err)
	}
	if sha == "" {
		t.Fatal("expected non-empty sha")
	}

	// The most recent commit for each skeleton must be authored by wuphf-bootstrap.
	for rel := range skeletons {
		refs, err := repo.Log(ctx, rel)
		if err != nil {
			t.Fatalf("log %s: %v", rel, err)
		}
		if len(refs) == 0 {
			t.Fatalf("%s: expected a commit in log", rel)
		}
		if refs[0].Author != "wuphf-bootstrap" {
			t.Fatalf("%s: expected wuphf-bootstrap author, got %q", rel, refs[0].Author)
		}
		if !strings.Contains(refs[0].Message, "materialize test blueprint") {
			t.Fatalf("%s: expected commit message to carry the caller's msg, got %q", rel, refs[0].Message)
		}
	}
}

func TestRepoCommitBootstrapIsNoopOnCleanTree(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Fresh init → tree is clean → no bootstrap commit should be created.
	sha, err := repo.CommitBootstrap(ctx, "should not commit")
	if err != nil {
		t.Fatalf("CommitBootstrap clean: %v", err)
	}
	if sha != "" {
		t.Fatalf("expected empty sha on clean tree, got %q", sha)
	}
}

func TestRepoAuditLogCoversAllAuthors(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	// One skeleton → bootstrap commit.
	full := filepath.Join(repo.Root(), "team", "playbooks", "renewal.md")
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte("# Renewal\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := repo.CommitBootstrap(ctx, "materialize"); err != nil {
		t.Fatalf("CommitBootstrap: %v", err)
	}
	// Two agent commits.
	if _, _, err := repo.Commit(ctx, "operator", "team/people/sarah.md", "# Sarah\n", "create", "sarah"); err != nil {
		t.Fatalf("Commit sarah: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "planner", "team/playbooks/renewal.md", "# Renewal v2\n", "replace", "update renewal"); err != nil {
		t.Fatalf("Commit renewal: %v", err)
	}

	entries, err := repo.AuditLog(ctx, time.Time{}, 0)
	if err != nil {
		t.Fatalf("AuditLog: %v", err)
	}
	// Expect at least 4 commits: init + bootstrap + sarah + renewal.
	if len(entries) < 4 {
		t.Fatalf("expected >=4 entries, got %d: %+v", len(entries), entries)
	}
	authors := map[string]bool{}
	for _, e := range entries {
		authors[e.Author] = true
	}
	for _, want := range []string{"system", "wuphf-bootstrap", "operator", "planner"} {
		if !authors[want] {
			t.Errorf("expected author %q in audit log, got authors=%v", want, authors)
		}
	}
	// Newest-first ordering — first entry should be the most recent commit.
	if entries[0].Author != "planner" {
		t.Errorf("expected newest-first ordering with planner at top, got %q", entries[0].Author)
	}
	// Paths populated for at least the content commits.
	for _, e := range entries {
		if e.Author == "planner" && len(e.Paths) == 0 {
			t.Errorf("expected paths to populate for planner commit, got empty")
		}
	}
}

func TestRepoAuditLogSinceFilter(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "operator", "team/people/a.md", "# A\n", "create", "a"); err != nil {
		t.Fatalf("Commit a: %v", err)
	}
	// since=far-future: should return nothing
	future := time.Now().Add(24 * time.Hour)
	entries, err := repo.AuditLog(ctx, future, 0)
	if err != nil {
		t.Fatalf("AuditLog: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries with far-future since, got %d", len(entries))
	}
}

func TestRepoAuditLogLimitCaps(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	for i := 0; i < 5; i++ {
		path := "team/people/" + string(rune('a'+i)) + ".md"
		if _, _, err := repo.Commit(ctx, "operator", path, "# X\n", "create", "p"); err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}
	entries, err := repo.AuditLog(ctx, time.Time{}, 2)
	if err != nil {
		t.Fatalf("AuditLog: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries with limit=2, got %d", len(entries))
	}
}
