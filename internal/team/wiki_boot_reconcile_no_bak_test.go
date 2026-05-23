package team

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestBackupMirrorSkipsEmptyWiki verifies that BackupMirror is a no-op on a
// freshly initialised wiki where only the .gitkeep scaffolding exists. The
// observable effect is no ~/.wuphf/wiki.bak/ directory next to ~/.wuphf/wiki/
// until at least one real article lands. Regression for #981.
func TestBackupMirrorSkipsEmptyWiki(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Init seeded team/{people,companies,…}/.gitkeep — no real articles yet.
	if err := repo.BackupMirror(ctx); err != nil {
		t.Fatalf("backup mirror: %v", err)
	}
	if _, err := os.Stat(repo.BackupRoot()); !os.IsNotExist(err) {
		t.Fatalf("expected no wiki.bak/ on empty wiki, stat err = %v", err)
	}
}

// TestBackupMirrorRunsAfterFirstArticle verifies the guard is one-shot:
// once a real article exists the backup proceeds normally.
func TestBackupMirrorRunsAfterFirstArticle(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "ceo", "team/people/nazz.md", "# Nazz\n", "create", "seed"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := repo.BackupMirror(ctx); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.BackupRoot(), "team/people/nazz.md")); err != nil {
		t.Fatalf("expected mirrored article after first write: %v", err)
	}
}

// TestTeamSubtreeHasArticleIgnoresDotFiles confirms that .gitkeep and other
// dot-prefixed scaffolding do not count as articles. The semantics matter:
// the boot guard keys on real content, not git-layout placeholders.
func TestTeamSubtreeHasArticleIgnoresDotFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "team")
	if err := os.MkdirAll(filepath.Join(dir, "people"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "people", ".gitkeep"), nil, 0o600); err != nil {
		t.Fatalf("write keep: %v", err)
	}
	has, err := teamSubtreeHasArticle(dir)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if has {
		t.Fatal("expected .gitkeep not to count as an article")
	}

	// Now add a real .md article — has should flip true.
	if err := os.WriteFile(filepath.Join(dir, "people", "nazz.md"), []byte("# N\n"), 0o600); err != nil {
		t.Fatalf("write md: %v", err)
	}
	has, err = teamSubtreeHasArticle(dir)
	if err != nil {
		t.Fatalf("walk 2: %v", err)
	}
	if !has {
		t.Fatal("expected .md article to count")
	}
}

// TestTeamSubtreeHasArticleAcceptsUppercaseExtension regression-guards the
// CodeRabbit fix on PR #987: validateArticlePath accepts .MD/.Md, so the
// scanner must too — otherwise BackupMirror would skip the snapshot on a
// wiki that has perfectly valid articles. Closes #987 follow-up.
func TestTeamSubtreeHasArticleAcceptsUppercaseExtension(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "team")
	if err := os.MkdirAll(filepath.Join(dir, "people"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"NAZZ.MD", "alex.Md"} {
		if err := os.WriteFile(filepath.Join(dir, "people", name), []byte("# N\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		has, err := teamSubtreeHasArticle(dir)
		if err != nil {
			t.Fatalf("walk after %s: %v", name, err)
		}
		if !has {
			t.Fatalf("expected %s to count as a markdown article", name)
		}
		// Clean up between iterations so the next file is the only signal.
		if err := os.Remove(filepath.Join(dir, "people", name)); err != nil {
			t.Fatalf("cleanup %s: %v", name, err)
		}
	}
}

// TestTeamSubtreeHasArticleMissingDir treats a never-created team/ as
// "empty" rather than an error — fresh boot with the worker still
// initialising should not surface a walk failure.
func TestTeamSubtreeHasArticleMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	has, err := teamSubtreeHasArticle(dir)
	if err != nil {
		t.Fatalf("missing dir should be silent, got err: %v", err)
	}
	if has {
		t.Fatal("expected missing dir to count as empty")
	}
}
