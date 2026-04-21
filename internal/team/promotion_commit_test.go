package team

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newPromotionRepo wires up a fresh git repo with a seeded notebook entry
// for promotion tests.
func newPromotionRepo(t *testing.T) *Repo {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	return repo
}

func seedNotebookEntry(t *testing.T, repo *Repo, slug, filename, content string) string {
	t.Helper()
	rel := filepath.ToSlash(filepath.Join("agents", slug, "notebook", filename))
	if _, _, err := repo.CommitNotebook(context.Background(), slug, rel, content, "create", "seed notebook entry"); err != nil {
		t.Fatalf("seed notebook: %v", err)
	}
	return rel
}

func TestApplyPromotion_HappyPath(t *testing.T) {
	repo := newPromotionRepo(t)
	src := seedNotebookEntry(t, repo, "pm", "2026-04-20-retro.md",
		"# Retro\n\nWe launched cleanly.\n",
	)
	p := &Promotion{
		ID:         "rvw-1",
		SourceSlug: "pm",
		SourcePath: src,
		TargetPath: "team/playbooks/q2-launch.md",
		Rationale:  "Canonical launch playbook",
	}
	sha, err := repo.ApplyPromotion(context.Background(), p, "ceo")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if sha == "" {
		t.Fatal("expected non-empty commit SHA")
	}

	// Target wiki article exists with body (minus any frontmatter).
	targetBytes, err := os.ReadFile(filepath.Join(repo.Root(), p.TargetPath))
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !strings.Contains(string(targetBytes), "We launched cleanly") {
		t.Fatalf("target missing body: %q", string(targetBytes))
	}
	if strings.HasPrefix(string(targetBytes), "---") {
		t.Fatal("target should not inherit frontmatter from source")
	}

	// Source notebook retained with frontmatter.
	sourceBytes, err := os.ReadFile(filepath.Join(repo.Root(), p.SourcePath))
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	body := string(sourceBytes)
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("source missing frontmatter: %q", body)
	}
	if !strings.Contains(body, "promoted_to: team/playbooks/q2-launch.md") {
		t.Fatalf("source missing promoted_to: %q", body)
	}
	if !strings.Contains(body, "promoted_by: ceo") {
		t.Fatalf("source missing promoted_by: %q", body)
	}
	// After the second commit the SHA should be the real one, not the placeholder.
	if strings.Contains(body, "promoted_commit_sha: 0000000000") {
		t.Fatalf("source still has placeholder SHA: %q", body)
	}
	if !strings.Contains(body, "promoted_commit_sha: "+sha) {
		t.Fatalf("source missing real SHA %q: %q", sha, body)
	}
	// Author of the primary commit is the approver slug.
	out, err := repo.runGitUnlocked("log", "-1", "--pretty=%an", "--", p.TargetPath)
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if strings.TrimSpace(out) != "ceo" {
		t.Fatalf("author=%s, want ceo", strings.TrimSpace(out))
	}
}

func TestApplyPromotion_TargetExistsReturnsError(t *testing.T) {
	repo := newPromotionRepo(t)
	src := seedNotebookEntry(t, repo, "pm", "x.md", "# X\n\nbody\n")
	// Seed the target path already.
	existing := "team/playbooks/q2-launch.md"
	if _, _, err := repo.Commit(context.Background(), "ceo", existing, "# Existing\n\nbody\n", "create", "seed target"); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	p := &Promotion{
		ID: "rvw-1", SourceSlug: "pm", SourcePath: src, TargetPath: existing,
	}
	_, err := repo.ApplyPromotion(context.Background(), p, "ceo")
	if !errors.Is(err, ErrPromotionTargetExists) {
		t.Fatalf("expected ErrPromotionTargetExists, got %v", err)
	}
}

func TestApplyPromotion_IndexIncludesTarget(t *testing.T) {
	repo := newPromotionRepo(t)
	src := seedNotebookEntry(t, repo, "pm", "x.md", "# X\n\nbody\n")
	p := &Promotion{
		ID: "rvw-1", SourceSlug: "pm", SourcePath: src, TargetPath: "team/playbooks/x.md", Rationale: "r",
	}
	if _, err := repo.ApplyPromotion(context.Background(), p, "ceo"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	idx, err := os.ReadFile(filepath.Join(repo.Root(), "index", "all.md"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if !strings.Contains(string(idx), "team/playbooks/x.md") {
		t.Fatalf("index missing target: %q", string(idx))
	}
}

func TestUpsertPromotionFrontmatter_FreshBlock(t *testing.T) {
	body := "# Retro\n\nbody\n"
	out := upsertPromotionFrontmatter(body, frontmatterFields{
		PromotedTo: "team/x.md", PromotedAt: "2026-04-20T00:00:00Z", PromotedBy: "ceo", PromotedSHA: "abc",
	})
	if !strings.HasPrefix(out, "---\npromoted_to: team/x.md\n") {
		t.Fatalf("no frontmatter prefix: %q", out)
	}
	if !strings.Contains(out, "# Retro") {
		t.Fatalf("body dropped: %q", out)
	}
}

func TestUpsertPromotionFrontmatter_UpdatesExistingKeys(t *testing.T) {
	body := "---\ntitle: Retro\npromoted_to: old/path.md\n---\n\n# Retro\n\nbody\n"
	out := upsertPromotionFrontmatter(body, frontmatterFields{
		PromotedTo: "team/new.md", PromotedAt: "2026-04-20T00:00:00Z", PromotedBy: "ceo", PromotedSHA: "abc",
	})
	if !strings.Contains(out, "promoted_to: team/new.md") {
		t.Fatalf("expected updated promoted_to: %q", out)
	}
	if strings.Contains(out, "promoted_to: old/path.md") {
		t.Fatalf("old promoted_to still present: %q", out)
	}
	// Pre-existing frontmatter key untouched.
	if !strings.Contains(out, "title: Retro") {
		t.Fatalf("title dropped: %q", out)
	}
}

func TestStripFrontmatter(t *testing.T) {
	cases := map[string]string{
		"# hi\n":                                 "# hi\n",
		"---\nfoo: 1\n---\n# body\n":             "# body\n",
		"---\nfoo: 1\n---\n\n# body\n":           "# body\n",
		"no frontmatter here": "no frontmatter here",
	}
	for in, want := range cases {
		if got := stripFrontmatter(in); got != want {
			t.Errorf("stripFrontmatter(%q) = %q, want %q", in, got, want)
		}
	}
}

// runGitUnlocked is a test helper to run git commands outside the normal
// write path. Uses the standard runGitLocked but wrapped in the mutex.
func (r *Repo) runGitUnlocked(args ...string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runGitLocked(context.Background(), "system", args...)
}
