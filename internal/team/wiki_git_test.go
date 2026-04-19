package team

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
