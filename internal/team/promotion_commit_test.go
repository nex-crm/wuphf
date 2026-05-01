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

func TestApplyPromotion_PreservesSkillFrontmatterOnPlaybookPath(t *testing.T) {
	repo := newPromotionRepo(t)
	source := "---\n" +
		"name: customer-refund\n" +
		"description: Issue a refund for a customer order.\n" +
		"version: 1.0.0\n" +
		"# trailing comment kept verbatim\n" +
		"---\n" +
		"# Customer Refund\n\nSteps go here.\n"
	src := seedNotebookEntry(t, repo, "pm", "customer-refund.md", source)

	p := &Promotion{
		ID:         "rvw-skill-1",
		SourceSlug: "pm",
		SourcePath: src,
		TargetPath: "team/playbooks/customer-refund.md",
		Rationale:  "Promote skill",
	}
	if _, err := repo.ApplyPromotion(context.Background(), p, "ceo"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(repo.Root(), p.TargetPath))
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	body := string(got)
	if !strings.HasPrefix(body, "---\nname: customer-refund\n") {
		t.Fatalf("target missing skill frontmatter prefix: %q", body)
	}
	if !strings.Contains(body, "description: Issue a refund for a customer order.") {
		t.Fatalf("target missing description: %q", body)
	}
	// Comments and unknown keys MUST survive (we do not parse-and-
	// reserialise).
	if !strings.Contains(body, "# trailing comment kept verbatim") {
		t.Fatalf("target lost YAML comment: %q", body)
	}
	if !strings.Contains(body, "# Customer Refund") {
		t.Fatalf("target dropped markdown body: %q", body)
	}
}

func TestApplyPromotion_PreservesSkillFrontmatterOnSkillsPath(t *testing.T) {
	repo := newPromotionRepo(t)
	source := "---\n" +
		"name: incident-response\n" +
		"description: Triage and resolve a production incident.\n" +
		"---\n" +
		"# Incident\n\nbody\n"
	src := seedNotebookEntry(t, repo, "ops", "incident.md", source)

	p := &Promotion{
		ID:         "rvw-skill-2",
		SourceSlug: "ops",
		SourcePath: src,
		TargetPath: "team/skills/incident-response.md",
		Rationale:  "Promote skill",
	}
	if _, err := repo.ApplyPromotion(context.Background(), p, "ceo"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(repo.Root(), p.TargetPath))
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !strings.HasPrefix(string(got), "---\nname: incident-response\n") {
		t.Fatalf("target missing skill frontmatter on team/skills/ path: %q", string(got))
	}
}

func TestApplyPromotion_StripsBackLinkOnlyFrontmatterOnPlaybookPath(t *testing.T) {
	// Source carries promoted_* keys (a re-promotion scenario where the
	// notebook entry was previously promoted somewhere else) but no
	// `name`/`description` keys. The target should have the back-link
	// frontmatter stripped — preserving it would self-reference the
	// previous target.
	repo := newPromotionRepo(t)
	source := "---\n" +
		"promoted_to: team/playbooks/old-target.md\n" +
		"promoted_at: 2026-04-01T00:00:00Z\n" +
		"promoted_by: ceo\n" +
		"promoted_commit_sha: abc1234\n" +
		"---\n" +
		"# Retro\n\nbody\n"
	src := seedNotebookEntry(t, repo, "pm", "retro.md", source)

	p := &Promotion{
		ID: "rvw-strip-1", SourceSlug: "pm", SourcePath: src,
		TargetPath: "team/playbooks/new-target.md", Rationale: "r",
	}
	if _, err := repo.ApplyPromotion(context.Background(), p, "ceo"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(repo.Root(), p.TargetPath))
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if strings.HasPrefix(string(got), "---") {
		t.Fatalf("target should NOT inherit back-link-only frontmatter: %q", string(got))
	}
	if strings.Contains(string(got), "promoted_to") {
		t.Fatalf("target leaked back-link key: %q", string(got))
	}
}

func TestApplyPromotion_StripsFrontmatterOnNonSkillPath(t *testing.T) {
	// Even with skill-shaped name/description keys, the rule does NOT apply
	// to non-skill paths (e.g. team/people/, team/decisions/). We keep the
	// pre-existing strip behaviour so those wiki sections continue to look
	// the way they did.
	repo := newPromotionRepo(t)
	source := "---\n" +
		"name: jane-doe\n" +
		"description: Engineer.\n" +
		"---\n" +
		"# Jane Doe\n\nbody\n"
	src := seedNotebookEntry(t, repo, "pm", "jane.md", source)

	p := &Promotion{
		ID: "rvw-people-1", SourceSlug: "pm", SourcePath: src,
		TargetPath: "team/people/jane-doe.md", Rationale: "r",
	}
	if _, err := repo.ApplyPromotion(context.Background(), p, "ceo"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(repo.Root(), p.TargetPath))
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if strings.HasPrefix(string(got), "---") {
		t.Fatalf("non-skill target should not preserve frontmatter: %q", string(got))
	}
}

func TestStripPromotionFrontmatterForTarget(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		targetPath string
		want       string
	}{
		{
			name:       "no frontmatter passes through",
			body:       "# hi\n",
			targetPath: "team/playbooks/x.md",
			want:       "# hi\n",
		},
		{
			name:       "skill frontmatter preserved on team/playbooks/",
			body:       "---\nname: x\ndescription: y\n---\n# body\n",
			targetPath: "team/playbooks/x.md",
			want:       "---\nname: x\ndescription: y\n---\n# body\n",
		},
		{
			name:       "skill frontmatter preserved on team/skills/",
			body:       "---\nname: x\ndescription: y\n---\n# body\n",
			targetPath: "team/skills/x.md",
			want:       "---\nname: x\ndescription: y\n---\n# body\n",
		},
		{
			name:       "skill frontmatter stripped on team/people/",
			body:       "---\nname: x\ndescription: y\n---\n# body\n",
			targetPath: "team/people/x.md",
			want:       "# body\n",
		},
		{
			name:       "back-link only frontmatter stripped on team/playbooks/",
			body:       "---\npromoted_to: team/playbooks/old.md\npromoted_by: ceo\n---\n# body\n",
			targetPath: "team/playbooks/new.md",
			want:       "# body\n",
		},
		{
			name:       "name without description stripped on team/playbooks/",
			body:       "---\nname: x\n---\n# body\n",
			targetPath: "team/playbooks/x.md",
			want:       "# body\n",
		},
		{
			name:       "indented name does not satisfy preservation gate",
			body:       "---\nmetadata:\n  name: nested\n  description: nested\n---\n# body\n",
			targetPath: "team/playbooks/x.md",
			want:       "# body\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripPromotionFrontmatterForTarget(tc.body, tc.targetPath)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStripPromotionFrontmatterForTarget_FiltersPromotedKeys(t *testing.T) {
	body := "---\n" +
		"name: send-digest\n" +
		"description: Send the daily digest.\n" +
		"promoted_to: team/playbooks/old.md\n" +
		"promoted_at: 2026-01-01T00:00:00Z\n" +
		"promoted_by: builder\n" +
		"promoted_commit_sha: abc123\n" +
		"---\n" +
		"# body\n"

	got := stripPromotionFrontmatterForTarget(body, "team/skills/send-digest.md")

	mustContain := []string{
		"name: send-digest",
		"description: Send the daily digest.",
		"# body",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}

	mustNotContain := []string{
		"promoted_to:",
		"promoted_at:",
		"promoted_by:",
		"promoted_commit_sha:",
	}
	for _, banned := range mustNotContain {
		if strings.Contains(got, banned) {
			t.Errorf("output should not contain %q\nfull output:\n%s", banned, got)
		}
	}

	// Frontmatter delimiters preserved.
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("output should start with frontmatter delimiter, got:\n%s", got)
	}
	if !strings.Contains(got, "\n---\n") {
		t.Errorf("output should contain closing frontmatter delimiter, got:\n%s", got)
	}
}

func TestStripPromotionKeys_HandlesIndentedAndCommentVariants(t *testing.T) {
	yamlBlock := "name: x\n" +
		"description: y\n" +
		"  promoted_to: leading-space\n" +
		"\tpromoted_at: leading-tab\n" +
		"promoted_by: builder\n" +
		"promoted_commit_sha: abc\n" +
		"keep_me: still-here\n"

	got := stripPromotionKeys(yamlBlock)

	if strings.Contains(got, "promoted_to:") {
		t.Errorf("indented promoted_to should be stripped, got:\n%s", got)
	}
	if strings.Contains(got, "promoted_at:") {
		t.Errorf("tab-indented promoted_at should be stripped, got:\n%s", got)
	}
	if strings.Contains(got, "promoted_by:") || strings.Contains(got, "promoted_commit_sha:") {
		t.Errorf("promoted_by/commit_sha should be stripped, got:\n%s", got)
	}
	if !strings.Contains(got, "name: x") || !strings.Contains(got, "description: y") || !strings.Contains(got, "keep_me: still-here") {
		t.Errorf("non-promotion keys should be preserved, got:\n%s", got)
	}
}

func TestStripFrontmatter(t *testing.T) {
	cases := map[string]string{
		"# hi\n":                       "# hi\n",
		"---\nfoo: 1\n---\n# body\n":   "# body\n",
		"---\nfoo: 1\n---\n\n# body\n": "# body\n",
		"no frontmatter here":          "no frontmatter here",
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
