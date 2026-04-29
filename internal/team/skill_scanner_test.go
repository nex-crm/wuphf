package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHasSkillBodyShape locks in the body-shape gate that protects the
// explicit-frontmatter fast path from FAST_PATH_TRAP articles (D9).
// Articles with valid Anthropic frontmatter still need a recognisable
// skill body shape — section header + list/numbered steps — before the
// scanner promotes them without LLM judgment.
func TestHasSkillBodyShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "steps + numbered list",
			body: "## Steps\n\n1. Do thing.\n2. Do other thing.",
			want: true,
		},
		{
			name: "how to + bullets",
			body: "## How to\n\n- Step one.\n- Step two.",
			want: true,
		},
		{
			name: "procedure + numbered",
			body: "## Procedure\n\n1. First.\n2. Second.",
			want: true,
		},
		{
			name: "runbook + numbered",
			body: "## Runbook\n\n1. Run.\n2. Verify.",
			want: true,
		},
		{
			name: "header but no list (bio prose)",
			body: "## Steps\n\nSome prose without numbered or bulleted items.",
			want: false,
		},
		{
			name: "list but no skill header (random notes)",
			body: "## Notes\n\n1. We talked about Q3.\n2. We talked about Q4.",
			want: false,
		},
		{
			name: "FAST_PATH_TRAP bio (D9)",
			body: "Jane joined the team in 2026 after a decade at Stripe.\nFavourite project: shipping Stripe's first multi-currency rail.",
			want: false,
		},
		{
			name: "FAST_PATH_TRAP decision log (D9)",
			body: "**Context.** Internal-only consumers.\n**Decision.** Ship REST.\n**Consequences.** Slower client iteration.",
			want: false,
		},
		{
			name: "marketing copy with Steps header but no list (D9)",
			body: "## Steps to better collaboration\n\nWUPHF helps your team move faster. Modern teams deserve modern tools.",
			want: false,
		},
		{
			name: "case-insensitive header match",
			body: "## STEPS\n\n1. Yell.",
			want: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasSkillBodyShape(tc.body); got != tc.want {
				t.Errorf("hasSkillBodyShape(%q): got %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestSpecToTeamSkill_OwnerAgentsRoundTrip locks in the per-agent skill scoping
// data field — owner_agents must travel from frontmatter through specToTeamSkill
// onto the teamSkill struct, mirroring the source_articles plumbing (D2).
func TestSpecToTeamSkill_OwnerAgentsRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fm   SkillFrontmatter
		want []string
	}{
		{
			name: "owner agents from frontmatter",
			fm: SkillFrontmatter{
				Name:        "deploy-frontend",
				Description: "Ship a hotfix release.",
				Metadata: SkillMetadata{
					Wuphf: SkillWuphfMeta{
						OwnerAgents: []string{"deploy-bot", "ceo"},
					},
				},
			},
			want: []string{"deploy-bot", "ceo"},
		},
		{
			name: "empty owner agents = lead-routable",
			fm: SkillFrontmatter{
				Name:        "weekly-retro",
				Description: "Run the weekly retro.",
			},
			want: nil,
		},
		{
			name: "single owner",
			fm: SkillFrontmatter{
				Name:        "csm-followup",
				Description: "Follow up with a customer.",
				Metadata: SkillMetadata{
					Wuphf: SkillWuphfMeta{
						OwnerAgents: []string{"csm"},
					},
				},
			},
			want: []string{"csm"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			spec := specToTeamSkill(tc.fm, "## Steps\n\n1. Do it.", "team/playbooks/x.md")

			if len(spec.OwnerAgents) != len(tc.want) {
				t.Fatalf("OwnerAgents len: got %d (%v), want %d (%v)", len(spec.OwnerAgents), spec.OwnerAgents, len(tc.want), tc.want)
			}
			for i, want := range tc.want {
				if spec.OwnerAgents[i] != want {
					t.Errorf("OwnerAgents[%d]: got %q, want %q", i, spec.OwnerAgents[i], want)
				}
			}

			// Mutating the spec slice must not bleed back into the source
			// frontmatter — defensive copy is required so downstream rewrites
			// can not accidentally edit the parsed frontmatter in place.
			if len(spec.OwnerAgents) > 0 && len(tc.fm.Metadata.Wuphf.OwnerAgents) > 0 {
				spec.OwnerAgents[0] = "MUTATED"
				if tc.fm.Metadata.Wuphf.OwnerAgents[0] == "MUTATED" {
					t.Error("specToTeamSkill must defensively copy OwnerAgents")
				}
			}
		})
	}
}

// TestInferOwnerAgentsFromPath covers the Stage A path-based default rule:
// per-agent notebook entries auto-scope to that agent; team/playbooks and
// team/customers stay lead-routable; explicit frontmatter always wins. The
// lead-routable case must return nil (not []string{}) so YAML omitempty
// drops the key on render.
func TestInferOwnerAgentsFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		relPath           string
		frontmatterOwners []string
		want              []string
	}{
		{
			name:    "agent notebook entry seeds owner from slug",
			relPath: "team/agents/csm/notebook/2026-04-22-followup.md",
			want:    []string{"csm"},
		},
		{
			name:    "deeply nested notebook entry still infers from slug",
			relPath: "team/agents/deploy-bot/notebook/deep/sub/incident.md",
			want:    []string{"deploy-bot"},
		},
		{
			name:    "playbook is lead-routable (nil)",
			relPath: "team/playbooks/retro.md",
			want:    nil,
		},
		{
			name:    "customer doc is lead-routable (nil)",
			relPath: "team/customers/acme/onboarding.md",
			want:    nil,
		},
		{
			name:    "agent profile (non-notebook) is lead-routable",
			relPath: "team/agents/csm/profile.md",
			want:    nil,
		},
		{
			name:    "mixed-case slug is normalised to lowercase",
			relPath: "team/agents/CSM/notebook/note.md",
			want:    []string{"csm"},
		},
		{
			name:              "frontmatter owners win over path inference",
			relPath:           "team/agents/csm/notebook/note.md",
			frontmatterOwners: []string{"deploy-bot", "ceo"},
			want:              []string{"deploy-bot", "ceo"},
		},
		{
			name:              "frontmatter owners win on a playbook path too",
			relPath:           "team/playbooks/retro.md",
			frontmatterOwners: []string{"eng"},
			want:              []string{"eng"},
		},
		{
			name:    "empty path is lead-routable",
			relPath: "",
			want:    nil,
		},
		{
			name:    "agents root directory entry is lead-routable",
			relPath: "team/agents/index.md",
			want:    nil,
		},
		{
			name:    "agents slug-only directory entry is lead-routable",
			relPath: "team/agents/csm",
			want:    nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := inferOwnerAgentsFromPath(tc.relPath, tc.frontmatterOwners)

			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %v (%T), want nil — lead-routable case must return nil so YAML omitempty drops the key", got, got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len: got %d (%v), want %d (%v)", len(got), got, len(tc.want), tc.want)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("[%d]: got %q, want %q", i, got[i], want)
				}
			}

			// Defensive copy: callers may sort/mutate the result without
			// touching the parsed frontmatter slice they passed in.
			if len(tc.frontmatterOwners) > 0 && len(got) > 0 {
				got[0] = "MUTATED"
				if tc.frontmatterOwners[0] == "MUTATED" {
					t.Error("inferOwnerAgentsFromPath must defensively copy frontmatterOwners")
				}
			}
		})
	}
}

// stubScannerLLM is a minimal llmProvider for the scanner tests that
// returns a single is_skill=true classification with the supplied
// frontmatter + body, regardless of the article path or content. Subsequent
// calls return is_skill=false so a multi-article walk doesn't fan-out.
type stubScannerLLM struct {
	fm     SkillFrontmatter
	body   string
	called int
}

func (p *stubScannerLLM) AskIsSkill(_ context.Context, _, _ string) (bool, SkillFrontmatter, string, error) {
	p.called++
	if p.called > 1 {
		return false, SkillFrontmatter{}, "", nil
	}
	return true, p.fm, p.body, nil
}

// newSkillScannerHarness mirrors newNotebookScannerHarness but for the
// Stage A scanner: brings up a temp wiki repo + worker so SkillScanner.Scan
// can resolve its on-disk root via b.WikiWorker.
func newSkillScannerHarness(t *testing.T) (*Broker, string, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	b := newTestBroker(t)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	return b, root, func() {
		cancel()
		worker.Stop()
	}
}

// writePlaybookEntry plops a markdown article at
// <root>/team/playbooks/<name>.md. team/playbooks/ is walked by the scanner
// (only team/playbooks/.compiled/ is excluded), so the file becomes a
// candidate article.
func writePlaybookEntry(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "team", "playbooks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestSkillScanner_SimilarityDivertsToEnhance proves the Stage A scanner
// catches errSkillSimilarToExisting from writeSkillProposalLocked, emits
// an enhance_skill_proposal interview, increments
// ScanResult.EnhancementCandidates, and does NOT write the candidate to
// b.skills. End-to-end via Scan() with a real wiki + stub LLM provider.
func TestSkillScanner_SimilarityDivertsToEnhance(t *testing.T) {
	b, root, teardown := newSkillScannerHarness(t)
	defer teardown()

	// Seed an active skill with text the candidate will collide with under
	// Jaccard tokens (no embedder set → falls through to jaccard-tokens at
	// threshold 0.35). Using identical body text guarantees a near-1.0 score.
	const collidingBody = "candidate text deploy frontend hotfix release pipeline smoke test toggle flipping"
	b.mu.Lock()
	addSkill(b, "deploy-frontend",
		"Ship a hotfix release to the frontend.",
		collidingBody)
	b.mu.Unlock()

	// Write a wiki article whose path is walked by the scanner.
	writePlaybookEntry(t, root, "ship-hotfix",
		"# Ship a hotfix\n\nShip the frontend hotfix release.\n")

	// Stub LLM: classify the article as is_skill with a slug DIFFERENT from
	// the seeded skill (otherwise pre-write dedup short-circuits before the
	// similarity gate). Body identical to the seeded skill so Jaccard hits
	// the enhance threshold.
	prov := &stubScannerLLM{
		fm: SkillFrontmatter{
			Name:        "ship-hotfix-release",
			Description: "Ship the hotfix release pipeline.",
		},
		body: collidingBody,
	}
	scanner := NewSkillScanner(b, prov, 10)

	res, err := scanner.Scan(context.Background(), "", false, "test")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.EnhancementCandidates != 1 {
		t.Fatalf("EnhancementCandidates: got %d, want 1 (errors: %+v, proposed: %d)",
			res.EnhancementCandidates, res.Errors, res.Proposed)
	}
	if res.Proposed != 0 {
		t.Errorf("Proposed: got %d, want 0 (sentinel must not count as a proposal)", res.Proposed)
	}
	if len(res.Errors) != 0 {
		t.Errorf("Errors: got %+v, want empty (similarity divert is not an error)", res.Errors)
	}

	// Candidate must NOT have been written.
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.findSkillByNameLocked("ship-hotfix-release") != nil {
		t.Error("similar candidate must not be written to b.skills on enhance verdict")
	}

	// An enhance_skill_proposal interview must have been appended pointing
	// at the existing skill's slug.
	if len(b.requests) == 0 {
		t.Fatal("expected at least one interview request after enhance divert")
	}
	last := b.requests[len(b.requests)-1]
	if last.Kind != "enhance_skill_proposal" {
		t.Errorf("interview kind: got %q, want enhance_skill_proposal", last.Kind)
	}
	if last.ReplyTo != "ship-hotfix-release" {
		t.Errorf("interview ReplyTo: got %q, want ship-hotfix-release", last.ReplyTo)
	}
	if last.Channel == "" {
		t.Error("interview channel must default to general, got empty")
	}
	if !strings.Contains(last.Title, "deploy-frontend") {
		t.Errorf("interview title should reference the existing slug, got %q", last.Title)
	}
}

// TestSkillScanner_EmitEnhancementInterviewFromSentinel is a focused unit
// test for the helper that the catch path delegates to — proves the
// channel + now defaults match writeSkillProposalLocked, the
// appendSkillProposalRequestLocked enhance branch is reached, and the
// resulting interview points at the existing skill's slug.
func TestSkillScanner_EmitEnhancementInterviewFromSentinel(t *testing.T) {
	b := newTestBroker(t)
	scanner := NewSkillScanner(b, &stubScannerLLM{}, 1)

	spec := teamSkill{
		Name:        "ship-hotfix-release",
		Title:       "Ship Hotfix Release",
		Description: "Ship the hotfix release pipeline.",
		Content:     "## Steps\n1. Tag.\n2. Push.",
		CreatedBy:   "archivist",
		// Channel intentionally left blank to assert the "general" default.
	}
	simErr := &errSkillSimilarToExisting{
		Slug:        "deploy-frontend",
		Title:       "Deploy Frontend",
		Description: "Ship a hotfix release to the frontend.",
		Score:       0.92,
		Method:      "jaccard-tokens",
	}

	requestsBefore := len(b.requests)
	scanner.emitEnhancementInterviewFromSentinel(spec, simErr)

	b.mu.Lock()
	defer b.mu.Unlock()

	if got, want := len(b.requests), requestsBefore+1; got != want {
		t.Fatalf("requests len: got %d, want %d", got, want)
	}
	last := b.requests[len(b.requests)-1]
	if last.Kind != "enhance_skill_proposal" {
		t.Errorf("Kind: got %q, want enhance_skill_proposal", last.Kind)
	}
	if last.ReplyTo != "ship-hotfix-release" {
		t.Errorf("ReplyTo: got %q, want ship-hotfix-release", last.ReplyTo)
	}
	if last.Channel != "general" {
		t.Errorf("Channel default: got %q, want general", last.Channel)
	}
	if !strings.Contains(last.Title, "deploy-frontend") {
		t.Errorf("Title should reference the existing slug, got %q", last.Title)
	}
	if !strings.Contains(last.Question, "deploy-frontend") {
		t.Errorf("Question should reference the existing slug, got %q", last.Question)
	}

	// nil-sentinel guard: helper must be a safe no-op so the catch site
	// doesn't have to second-guess errors.As's contract.
	requestsBefore = len(b.requests)
	scanner.emitEnhancementInterviewFromSentinel(spec, nil)
	if got := len(b.requests); got != requestsBefore {
		t.Errorf("nil sentinel must not append: requests went from %d to %d", requestsBefore, got)
	}
}
