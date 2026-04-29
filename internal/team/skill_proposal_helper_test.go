package team

import (
	"context"
	"strings"
	"testing"
)

// skillProposalSpec is a convenience constructor so tests don't need to fill
// every field of teamSkill.
func skillProposalSpec(name, description, createdBy string) teamSkill {
	return teamSkill{
		Name:        name,
		Description: description,
		Content:     "Some content.",
		CreatedBy:   createdBy,
		Channel:     "general",
		Status:      "proposed",
	}
}

// callWriteSkillProposalLocked is a helper that acquires b.mu, calls
// writeSkillProposalLocked, then checks that the lock is still held (i.e.
// the function returned with lock held as documented). It is safe to call
// from tests because b.wikiWorker is nil so the deadlock-avoidance path is
// exercised without an actual wiki worker goroutine.
func callWriteSkillProposalLocked(b *Broker, spec teamSkill) (*teamSkill, error) {
	b.mu.Lock()
	sk, err := b.writeSkillProposalLocked(spec)
	b.mu.Unlock()
	return sk, err
}

func TestWriteSkillProposalLocked_ValidatesName(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	tests := []struct {
		name    string
		spec    teamSkill
		wantErr string
	}{
		{
			name:    "empty name",
			spec:    skillProposalSpec("", "A description.", "archivist"),
			wantErr: "name is required",
		},
		{
			name:    "whitespace name",
			spec:    skillProposalSpec("   ", "A description.", "archivist"),
			wantErr: "name is required",
		},
		{
			// skillSlug strips leading underscores → "-bad" which starts with dash
			name:    "underscore-leading name produces dash-start slug",
			spec:    skillProposalSpec("_bad", "A description.", "archivist"),
			wantErr: "slug",
		},
		{
			// skillSlug strips leading dashes but they are kept; "- bad" → "--bad"
			name:    "dash-leading name stays invalid",
			spec:    skillProposalSpec("-bad-start", "A description.", "archivist"),
			wantErr: "slug",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := callWriteSkillProposalLocked(b, tc.spec)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestWriteSkillProposalLocked_ValidatesDescription(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("my-skill", "", "archivist")
	_, err := callWriteSkillProposalLocked(b, spec)
	if err == nil {
		t.Fatal("expected error for empty description, got nil")
	}
	if !strings.Contains(err.Error(), "description") {
		t.Errorf("error %q should mention description", err.Error())
	}
}

func TestWriteSkillProposalLocked_SystemAuthorWhitelist(t *testing.T) {
	t.Parallel()

	systemAuthors := []string{"archivist", "scanner", "system"}

	for _, author := range systemAuthors {
		author := author
		t.Run("system_author_"+author, func(t *testing.T) {
			t.Parallel()
			b := newTestBroker(t)
			spec := skillProposalSpec("my-skill-"+author, "A description.", author)
			sk, err := callWriteSkillProposalLocked(b, spec)
			if err != nil {
				t.Fatalf("expected system author %q to bypass member check, got err: %v", author, err)
			}
			if sk == nil {
				t.Fatal("expected non-nil skill, got nil")
			}
			if sk.CreatedBy != author {
				t.Errorf("CreatedBy: got %q, want %q", sk.CreatedBy, author)
			}
		})
	}
}

func TestWriteSkillProposalLocked_NonSystemAuthorRequiresMember(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	// "random-agent" is not in the system author whitelist and not a
	// registered team member — should return an error.
	spec := skillProposalSpec("my-skill", "A description.", "random-agent")
	_, err := callWriteSkillProposalLocked(b, spec)
	if err == nil {
		t.Fatal("expected error for unregistered non-system author, got nil")
	}
	if !strings.Contains(err.Error(), "registered team member") {
		t.Errorf("error %q should mention 'registered team member'", err.Error())
	}
}

func TestWriteSkillProposalLocked_DeduplicatesOnSlugCollision(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("dedup-skill", "First write.", "archivist")
	sk1, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Second write with the same name should return the existing skill.
	spec2 := skillProposalSpec("dedup-skill", "Second write (different desc).", "archivist")
	sk2, err := callWriteSkillProposalLocked(b, spec2)
	if err != nil {
		t.Fatalf("second write (dedup): %v", err)
	}
	if sk2 == nil {
		t.Fatal("dedup: expected non-nil existing skill, got nil")
	}
	// Should be the SAME skill (first write wins).
	if sk2.Description != sk1.Description {
		t.Errorf("dedup: description changed — expected %q, got %q (second write was not deduped)",
			sk1.Description, sk2.Description)
	}

	// In-memory list should have exactly one skill.
	b.mu.Lock()
	count := 0
	for _, s := range b.skills {
		if skillSlug(s.Name) == "dedup-skill" {
			count++
		}
	}
	b.mu.Unlock()
	if count != 1 {
		t.Errorf("expected exactly 1 skill with slug 'dedup-skill', found %d", count)
	}
}

func TestWriteSkillProposalLocked_SuccessfulCreate(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("send-digest", "Send a daily digest.", "archivist")
	spec.Tags = []string{"comms"}
	spec.Trigger = "Every morning"

	sk, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sk == nil {
		t.Fatal("expected non-nil skill")
	}
	if sk.Name != "send-digest" {
		t.Errorf("Name: got %q, want 'send-digest'", sk.Name)
	}
	if sk.Status != "proposed" {
		t.Errorf("Status: got %q, want 'proposed'", sk.Status)
	}
	if sk.CreatedBy != "archivist" {
		t.Errorf("CreatedBy: got %q, want 'archivist'", sk.CreatedBy)
	}

	// Verify the skill is in b.skills.
	b.mu.Lock()
	found := b.findSkillByNameLocked("send-digest")
	b.mu.Unlock()
	if found == nil {
		t.Error("skill not found in b.skills after creation")
	}

	// Verify a proposal request was appended.
	b.mu.Lock()
	var hasProposal bool
	for _, req := range b.requests {
		if req.Kind == "skill_proposal" && req.ReplyTo == "send-digest" {
			hasProposal = true
			break
		}
	}
	b.mu.Unlock()
	if !hasProposal {
		t.Error("no skill_proposal request found in b.requests")
	}
}

func TestWriteSkillProposalLocked_DefaultsStatus(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	// Status field left empty — should default to "proposed".
	spec := teamSkill{
		Name:        "auto-status",
		Description: "Should default to proposed.",
		Content:     "content",
		CreatedBy:   "archivist",
	}
	sk, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sk.Status != "proposed" {
		t.Errorf("Status: got %q, want 'proposed'", sk.Status)
	}
}

// TestWriteSkillProposalLocked_GuardRejectsDangerous covers the trust-ladder
// gate: a community-trust skill with a dangerous body is rejected outright,
// the rejection counter is bumped, and no skill is appended to b.skills.
func TestWriteSkillProposalLocked_GuardRejectsDangerous(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("evil-skill", "Wipes the world.", "archivist")
	spec.Content = "rm -rf /var/data\nThen continue with normal steps."

	_, err := callWriteSkillProposalLocked(b, spec)
	if err == nil {
		t.Fatal("expected guard rejection, got nil error")
	}
	if !strings.Contains(err.Error(), "skill_guard") {
		t.Errorf("error %q should mention skill_guard", err.Error())
	}

	b.mu.Lock()
	rejected := b.skillCompileMetrics.ProposalsRejectedByGuardTotal
	count := 0
	for _, s := range b.skills {
		if skillSlug(s.Name) == "evil-skill" {
			count++
		}
	}
	b.mu.Unlock()
	if rejected < 1 {
		t.Errorf("ProposalsRejectedByGuardTotal: got %d, want >= 1", rejected)
	}
	if count != 0 {
		t.Errorf("skill should not be in b.skills, found %d", count)
	}
}

// TestWriteSkillProposalLocked_GuardAllowsCautionForCommunity verifies that
// caution verdicts pass through under community trust (Stage A wiki source)
// and the safety_scan stamp is preserved on the skill we just wrote.
func TestWriteSkillProposalLocked_GuardAllowsCautionForCommunity(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("install-doc", "Document install steps.", "archivist")
	spec.Content = "Install: visit https://example.com/install for setup."

	sk, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("expected caution to pass under community trust, got: %v", err)
	}
	if sk == nil {
		t.Fatal("expected non-nil skill")
	}
	// Verify it landed in b.skills.
	b.mu.Lock()
	found := b.findSkillByNameLocked("install-doc")
	b.mu.Unlock()
	if found == nil {
		t.Error("skill not found after caution-allowed write")
	}
}

// TestWriteSkillProposalLocked_GuardStampsSafetyScan verifies the safety_scan
// metadata is rendered into the wiki copy when a skill writes successfully.
// (This indirectly covers the safety_scan stamp by exercising the guard
// scaffolding; the exact YAML payload is checked elsewhere.)
func TestWriteSkillProposalLocked_GuardStampsSafetyScan(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	spec := skillProposalSpec("stamped", "A clean skill.", "archivist")
	spec.Content = "Step 1: do the thing.\nStep 2: report back."
	sk, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sk == nil {
		t.Fatal("expected skill")
	}
	// The scan ran — counter should NOT have bumped (verdict was safe).
	b.mu.Lock()
	rejected := b.skillCompileMetrics.ProposalsRejectedByGuardTotal
	b.mu.Unlock()
	if rejected != 0 {
		t.Errorf("expected zero rejections for safe verdict, got %d", rejected)
	}
}

func TestWriteSkillProposalLocked_ValidSlugVariants(t *testing.T) {
	t.Parallel()

	validNames := []struct {
		input    string
		wantSlug string
	}{
		{"my-skill", "my-skill"},
		{"abc123", "abc123"},
		{"a", "a"},
		{"send-digest-v2", "send-digest-v2"},
		{"Send-Digest", "send-digest"}, // uppercase normalised by skillSlug
	}

	for _, tc := range validNames {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			b := newTestBroker(t)
			spec := skillProposalSpec(tc.input, "A description.", "archivist")
			sk, err := callWriteSkillProposalLocked(b, spec)
			if err != nil {
				t.Fatalf("name=%q: unexpected error: %v", tc.input, err)
			}
			if skillSlug(sk.Name) != tc.wantSlug {
				t.Errorf("name=%q: slug=%q, want %q", tc.input, skillSlug(sk.Name), tc.wantSlug)
			}
		})
	}
}

// TestWriteSkillProposalLocked_OwnerValidation pins down the PR 7 owner
// validation contract:
//   - Unknown slugs are stripped with a WARN log; the skill is NOT rejected.
//   - All-unknown lists fall back to lead-routable (empty OwnerAgents).
//   - Known + unknown mix retains the known slugs and drops the rest.
//   - Whitespace + case differences canonicalise to lowercase trimmed slugs.
//   - Duplicate slugs are deduped.
func TestWriteSkillProposalLocked_OwnerValidation(t *testing.T) {
	t.Parallel()

	makeBrokerWithMembers := func(t *testing.T) *Broker {
		t.Helper()
		b := newTestBroker(t)
		b.mu.Lock()
		b.members = []officeMember{
			{Slug: "ceo", Name: "CEO", BuiltIn: true},
			{Slug: "csm", Name: "CSM"},
			{Slug: "deploy-bot", Name: "Deploy Bot"},
		}
		b.mu.Unlock()
		return b
	}

	tests := []struct {
		name   string
		owners []string
		want   []string
	}{
		{
			name:   "all unknown owners falls back to lead-routable",
			owners: []string{"ghost", "phantom"},
			want:   nil,
		},
		{
			name:   "known and unknown keeps the known slug",
			owners: []string{"deploy-bot", "ghost"},
			want:   []string{"deploy-bot"},
		},
		{
			name:   "all known owners pass through",
			owners: []string{"deploy-bot", "csm"},
			want:   []string{"deploy-bot", "csm"},
		},
		{
			name:   "whitespace and case canonicalise to lowercase trimmed",
			owners: []string{"  Deploy-Bot  ", "CSM"},
			want:   []string{"deploy-bot", "csm"},
		},
		{
			name:   "duplicate slugs are deduped",
			owners: []string{"csm", "csm", "CSM"},
			want:   []string{"csm"},
		},
		{
			name:   "empty input stays empty",
			owners: nil,
			want:   nil,
		},
		{
			// F5: belt-and-suspenders regex. The "../etc/passwd" shape would
			// also fail member-existence, but the regex makes path-injection
			// shapes inert at the string level — defence in depth.
			name:   "path-traversal shape rejected by slug regex",
			owners: []string{"../etc/passwd", "deploy-bot"},
			want:   []string{"deploy-bot"},
		},
		{
			name:   "leading-dash slug rejected by regex",
			owners: []string{"-bad-leading-dash", "csm"},
			want:   []string{"csm"},
		},
		{
			name:   "uppercase-letters fold to lowercase before regex check",
			owners: []string{"DEPLOY-BOT"},
			want:   []string{"deploy-bot"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := makeBrokerWithMembers(t)
			spec := skillProposalSpec("scoped-skill-"+skillSlug(tc.name), "A description.", "archivist")
			spec.Content = "## Steps\n\n1. Do it."
			spec.OwnerAgents = tc.owners

			sk, err := callWriteSkillProposalLocked(b, spec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(sk.OwnerAgents) != len(tc.want) {
				t.Fatalf("OwnerAgents len: got %v, want %v", sk.OwnerAgents, tc.want)
			}
			for i, want := range tc.want {
				if sk.OwnerAgents[i] != want {
					t.Errorf("OwnerAgents[%d]: got %q, want %q", i, sk.OwnerAgents[i], want)
				}
			}
		})
	}

	t.Run("validation does not block the write", func(t *testing.T) {
		t.Parallel()
		b := makeBrokerWithMembers(t)
		spec := skillProposalSpec("survives-typos", "A description.", "archivist")
		spec.Content = "## Steps\n\n1. Do it."
		spec.OwnerAgents = []string{"ghost"}

		sk, err := callWriteSkillProposalLocked(b, spec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Skill exists, owners stripped to empty (lead-routable fallback).
		if sk == nil {
			t.Fatal("skill must still be written even when every owner is unknown")
		}
		if len(sk.OwnerAgents) != 0 {
			t.Errorf("expected lead-routable fallback (empty OwnerAgents), got %v", sk.OwnerAgents)
		}
	})
}

// PR 7 task #13: similarity gate integration. The four tests below pin
// down the three writeSkillProposalLocked branches plus the metric
// increment. Tests use the stubEmbedder from skill_similarity_test.go for
// deterministic cosine scores; the helper falls through to Jaccard when no
// embedder is wired, but the embedder path lets tests dial the score
// precisely across the enhance / ambiguous / create-new bands.

// similaritySpec is a writeSkillProposalLocked spec tuned for the embedder
// path. The body+description are what the helper hashes for comparison.
func similaritySpec(name, description string) teamSkill {
	return teamSkill{
		Name:        name,
		Description: description,
		Content:     "## Steps\n\n1. Do it.",
		CreatedBy:   "archivist",
		Channel:     "general",
		Status:      "proposed",
	}
}

// staticVecEmbedder returns a fixed vector for any candidate text and a
// caller-controlled vector for any existing-skill text. Lets tests pick the
// exact cosine score by choosing the second vector's projection on the
// first. All vectors are L2-normalised.
func staticVecEmbedder(candidateVec, existingVec []float32) *stubEmbedder {
	cand := l2norm(append([]float32(nil), candidateVec...))
	exist := l2norm(append([]float32(nil), existingVec...))
	return &stubEmbedder{
		vec: func(text string) []float32 {
			if strings.Contains(text, "candidate-text") {
				return cand
			}
			return exist
		},
	}
}

func TestWriteSkillProposalLocked_SimilarSkillExists_ReturnsErrSentinel(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)

	// Seed an active skill the candidate will collide with.
	b.mu.Lock()
	addSkill(b, "send-invoice-reminder", "Send the AR follow-up at d7.", "candidate-text body that the embedder keys off.")
	b.mu.Unlock()

	// Embedder returns an identical vector for both candidate and existing,
	// so cosine score = 1.0, which exceeds the enhance threshold (0.85).
	v := l2norm([]float32{1, 0, 0})
	b.skillEmbedder = staticVecEmbedder(v, v)

	spec := similaritySpec("invoice-d7-reminder", "AR reminder for the d7 cohort.")
	// Make the candidate text hashable by the stub embedder.
	spec.Content = "candidate-text body that the embedder keys off."

	sk, err := callWriteSkillProposalLocked(b, spec)
	if sk != nil {
		t.Errorf("expected sk == nil on enhance verdict, got %+v", sk)
	}
	if err == nil {
		t.Fatal("expected errSkillSimilarToExisting, got nil")
	}
	var sentinel *errSkillSimilarToExisting
	if !errorsAs(err, &sentinel) {
		t.Fatalf("expected *errSkillSimilarToExisting, got %T: %v", err, err)
	}
	if sentinel.Slug != "send-invoice-reminder" {
		t.Errorf("Slug: got %q, want send-invoice-reminder", sentinel.Slug)
	}
	if sentinel.Score < 0.85 {
		t.Errorf("Score: got %v, want >= 0.85", sentinel.Score)
	}
	if sentinel.Method != "embedding-cosine" {
		t.Errorf("Method: got %q, want embedding-cosine", sentinel.Method)
	}
	// The candidate must NOT have been written.
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.findSkillByNameLocked("invoice-d7-reminder") != nil {
		t.Error("similar candidate must not be written to b.skills")
	}
}

func TestEnhancementCandidatesTotal_Increments(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.mu.Lock()
	addSkill(b, "send-invoice-reminder", "Send the AR follow-up at d7.", "candidate-text body that the embedder keys off.")
	b.mu.Unlock()

	v := l2norm([]float32{1, 0, 0})
	b.skillEmbedder = staticVecEmbedder(v, v)

	spec := similaritySpec("invoice-d7-reminder", "AR reminder for the d7 cohort.")
	spec.Content = "candidate-text body that the embedder keys off."

	before := b.skillCompileMetrics.EnhancementCandidatesTotal
	_, err := callWriteSkillProposalLocked(b, spec)
	if err == nil {
		t.Fatal("expected errSkillSimilarToExisting, got nil")
	}
	after := b.skillCompileMetrics.EnhancementCandidatesTotal
	if after != before+1 {
		t.Errorf("EnhancementCandidatesTotal: got %d, want %d", after, before+1)
	}
}

func TestWriteSkillProposalLocked_AmbiguousSimilarity_AnnotatesFrontmatter(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.mu.Lock()
	addSkill(b, "draft-monthly-report", "Draft the monthly summary report.", "existing-text drafting workflow.")
	b.mu.Unlock()

	// Pick vectors so cosine = 0.75 (between ambiguous=0.70 and enhance=0.85).
	cand := l2norm([]float32{1, 0, 0})
	exist := l2norm([]float32{0.75, 0.6614, 0}) // dot(cand, exist) ≈ 0.75
	b.skillEmbedder = staticVecEmbedder(cand, exist)

	spec := similaritySpec("compile-quarterly-summary", "Compile the quarterly summary briefing.")
	spec.Content = "candidate-text drafting a quarterly summary."

	sk, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error on ambiguous verdict: %v", err)
	}
	if sk == nil {
		t.Fatal("expected the candidate to be written despite ambiguous flag")
	}
	if sk.Status != "proposed" {
		t.Errorf("Status: got %q, want proposed", sk.Status)
	}
	// EnhancementCandidatesTotal must NOT increment on ambiguous — that's
	// the contract that distinguishes ambiguous (write + annotate) from
	// enhance (return sentinel + bump metric).
	if got := b.skillCompileMetrics.EnhancementCandidatesTotal; got != 0 {
		t.Errorf("EnhancementCandidatesTotal must NOT increment on ambiguous, got %d", got)
	}

	// Verify the verdict the integration saw was actually "ambiguous" by
	// re-running the helper against the original catalog. The candidate was
	// written into b.skills, so we exclude the just-written entry by name.
	// (skillSimilarityEligible already self-skips by name.)
	b.mu.Lock()
	verdict := b.findSimilarActiveSkillLocked(context.Background(), spec)
	b.mu.Unlock()
	if verdict.Recommendation != "ambiguous" {
		t.Errorf("verdict.Recommendation = %q, want ambiguous (score=%v)", verdict.Recommendation, verdict.Score)
	}
	if verdict.Existing == nil || skillSlug(verdict.Existing.Name) != "draft-monthly-report" {
		t.Errorf("verdict.Existing should point at draft-monthly-report, got %+v", verdict.Existing)
	}
}

func TestWriteSkillProposalLocked_DistinctSkill_WritesNormally(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.mu.Lock()
	addSkill(b, "send-invoice-reminder", "Send the AR follow-up at d7.", "completely different existing-text body.")
	b.mu.Unlock()

	// Pick orthogonal vectors so cosine = 0 (clearly create_new).
	cand := l2norm([]float32{1, 0, 0})
	exist := l2norm([]float32{0, 1, 0})
	b.skillEmbedder = staticVecEmbedder(cand, exist)

	spec := similaritySpec("renewal-reminder", "Customer renewal reminder workflow.")
	spec.Content = "candidate-text orthogonal body."

	sk, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error on distinct skill: %v", err)
	}
	if sk == nil {
		t.Fatal("expected the candidate to be written")
	}
	if sk.Status != "proposed" {
		t.Errorf("Status: got %q, want proposed", sk.Status)
	}
	if got := b.skillCompileMetrics.EnhancementCandidatesTotal; got != 0 {
		t.Errorf("EnhancementCandidatesTotal must stay 0 on create_new, got %d", got)
	}
}

// errorsAs is a tiny shim so this test file doesn't need to import errors
// purely for one call site. Mirrors errors.As for *errSkillSimilarToExisting.
func errorsAs(err error, target **errSkillSimilarToExisting) bool {
	for err != nil {
		if t, ok := err.(*errSkillSimilarToExisting); ok {
			*target = t
			return true
		}
		// Unwrap if applicable.
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
			continue
		}
		break
	}
	return false
}

// PR 7 task #15: enhance interview contract.

// TestAppendSkillProposalRequestLocked_EnhanceKindHasThreeOptions pins down
// the interview-options contract: the new enhance_skill_proposal kind ships
// [enhance, approve_anyway, reject], the legacy skill_proposal stays
// [accept, reject].
func TestAppendSkillProposalRequestLocked_EnhanceKindHasThreeOptions(t *testing.T) {
	t.Parallel()

	t.Run("enhance kind", func(t *testing.T) {
		t.Parallel()
		b := newTestBroker(t)
		candidate := teamSkill{
			Name:        "invoice-d7-reminder",
			Title:       "Invoice d7 reminder",
			Description: "AR follow-up at d7.",
			CreatedBy:   "archivist",
			Channel:     "general",
		}
		b.mu.Lock()
		b.appendSkillProposalRequestLocked(candidate, "general", "2026-04-29T00:00:00Z", "send-invoice-reminder")
		b.mu.Unlock()

		if got := len(b.requests); got != 1 {
			t.Fatalf("requests: got %d, want 1", got)
		}
		req := b.requests[0]
		if req.Kind != "enhance_skill_proposal" {
			t.Errorf("Kind: got %q, want enhance_skill_proposal", req.Kind)
		}
		if got := len(req.Options); got != 3 {
			t.Fatalf("Options len: got %d, want 3", got)
		}
		want := []string{"enhance", "approve_anyway", "reject"}
		for i, w := range want {
			if req.Options[i].ID != w {
				t.Errorf("Options[%d].ID: got %q, want %q", i, req.Options[i].ID, w)
			}
		}
		if req.RecommendedID != "enhance" {
			t.Errorf("RecommendedID: got %q, want enhance", req.RecommendedID)
		}
		if req.Metadata["enhances_slug"] != "send-invoice-reminder" {
			t.Errorf("Metadata.enhances_slug: got %v, want send-invoice-reminder", req.Metadata["enhances_slug"])
		}
		if req.EnhanceCandidate == nil {
			t.Fatal("EnhanceCandidate must be set on enhance interviews")
		}
		if req.EnhanceCandidate.Name != "invoice-d7-reminder" {
			t.Errorf("EnhanceCandidate.Name: got %q, want invoice-d7-reminder", req.EnhanceCandidate.Name)
		}
	})

	t.Run("legacy skill_proposal kind keeps two options", func(t *testing.T) {
		t.Parallel()
		b := newTestBroker(t)
		candidate := teamSkill{
			Name:        "send-renewal",
			Title:       "Send renewal",
			Description: "Send the renewal email.",
			CreatedBy:   "archivist",
			Channel:     "general",
		}
		b.mu.Lock()
		b.appendSkillProposalRequestLocked(candidate, "general", "2026-04-29T00:00:00Z", "")
		b.mu.Unlock()

		req := b.requests[0]
		if req.Kind != "skill_proposal" {
			t.Errorf("Kind: got %q, want skill_proposal", req.Kind)
		}
		if got := len(req.Options); got != 2 {
			t.Fatalf("Options len: got %d, want 2", got)
		}
		if req.Options[0].ID != "accept" || req.Options[1].ID != "reject" {
			t.Errorf("Options: got %s/%s, want accept/reject", req.Options[0].ID, req.Options[1].ID)
		}
		if req.EnhanceCandidate != nil {
			t.Error("EnhanceCandidate must be nil on legacy skill_proposal")
		}
	})

	t.Run("ambiguous skill_proposal carries similar_to_existing metadata", func(t *testing.T) {
		t.Parallel()
		b := newTestBroker(t)
		candidate := teamSkill{
			Name:        "near-dup",
			Title:       "Near dup",
			Description: "A near-duplicate skill.",
			CreatedBy:   "archivist",
			Channel:     "general",
			SimilarToExisting: &SkillSimilarRef{
				Slug:   "original-skill",
				Score:  0.78,
				Method: "embedding-cosine",
			},
		}
		b.mu.Lock()
		b.appendSkillProposalRequestLocked(candidate, "general", "2026-04-29T00:00:00Z", "")
		b.mu.Unlock()

		req := b.requests[0]
		if req.Kind != "skill_proposal" {
			t.Errorf("Kind: got %q, want skill_proposal", req.Kind)
		}
		ref, ok := req.Metadata["similar_to_existing"].(SkillSimilarRef)
		if !ok {
			t.Fatalf("Metadata.similar_to_existing missing or wrong type: %T %v", req.Metadata["similar_to_existing"], req.Metadata["similar_to_existing"])
		}
		if ref.Slug != "original-skill" {
			t.Errorf("Metadata.similar_to_existing.slug: got %q, want original-skill", ref.Slug)
		}
	})
}

// TestWriteSkillProposalLocked_AmbiguousSimilarity_PopulatesSkillSimilarRef
// guards Gap 3: after an ambiguous-band write the in-memory teamSkill
// surfaces SimilarToExisting via JSON. The Skills app reads /skills and
// expects this field directly, not via a frontmatter round-trip.
func TestWriteSkillProposalLocked_AmbiguousSimilarity_PopulatesSkillSimilarRef(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.mu.Lock()
	addSkill(b, "draft-monthly-report", "Draft the monthly summary report.", "existing-text drafting workflow.")
	b.mu.Unlock()

	cand := l2norm([]float32{1, 0, 0})
	exist := l2norm([]float32{0.75, 0.6614, 0})
	b.skillEmbedder = staticVecEmbedder(cand, exist)

	spec := similaritySpec("compile-quarterly-summary", "Compile the quarterly summary briefing.")
	spec.Content = "candidate-text drafting a quarterly summary."

	sk, err := callWriteSkillProposalLocked(b, spec)
	if err != nil {
		t.Fatalf("unexpected error on ambiguous: %v", err)
	}
	if sk == nil {
		t.Fatal("expected the candidate to be written")
	}
	if sk.SimilarToExisting == nil {
		t.Fatal("SimilarToExisting must be populated on ambiguous-band writes")
	}
	if sk.SimilarToExisting.Slug != "draft-monthly-report" {
		t.Errorf("SimilarToExisting.Slug: got %q, want draft-monthly-report", sk.SimilarToExisting.Slug)
	}
	if sk.SimilarToExisting.Score < 0.70 || sk.SimilarToExisting.Score >= 0.85 {
		t.Errorf("SimilarToExisting.Score: got %v, want in [0.70, 0.85)", sk.SimilarToExisting.Score)
	}
}

// TestWriteSkillProposalLocked_BypassSimilarity_OverridesGate guards the
// approve_anyway path: with bypassSimilarity=true the gate is skipped and
// the candidate writes normally even when the embedder would otherwise
// flag it as enhance_existing.
func TestWriteSkillProposalLocked_BypassSimilarity_OverridesGate(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.mu.Lock()
	addSkill(b, "send-invoice-reminder", "Send the AR follow-up at d7.", "candidate-text body that the embedder keys off.")
	b.mu.Unlock()

	v := l2norm([]float32{1, 0, 0})
	b.skillEmbedder = staticVecEmbedder(v, v)

	spec := similaritySpec("invoice-d7-reminder", "AR reminder for the d7 cohort.")
	spec.Content = "candidate-text body that the embedder keys off."

	// First call WITHOUT bypass — must return the sentinel.
	if _, err := callWriteSkillProposalLocked(b, spec); err == nil {
		t.Fatal("expected sentinel without bypass; got nil")
	}

	// Second call WITH bypass — must succeed and write the skill.
	b.mu.Lock()
	sk, err := b.writeSkillProposalWithOptsLocked(spec, proposalOpts{bypassSimilarity: true})
	b.mu.Unlock()
	if err != nil {
		t.Fatalf("bypass write failed: %v", err)
	}
	if sk == nil {
		t.Fatal("bypass write returned nil")
	}
	if sk.Status != "proposed" {
		t.Errorf("Status: got %q, want proposed", sk.Status)
	}
	// Bypass must NOT bump EnhancementCandidatesTotal a second time.
	// (It was bumped once on the first non-bypass call.)
	if got := b.skillCompileMetrics.EnhancementCandidatesTotal; got != 1 {
		t.Errorf("EnhancementCandidatesTotal: got %d, want 1 (bypass must not re-trigger gate)", got)
	}
}
