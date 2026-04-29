package team

import (
	"context"
	"strings"
	"testing"
)

// stubEmbedder produces deterministic, L2-normalised vectors from a
// caller-supplied scoring function. Returning vectors directly (instead of
// hashing the input text) lets tests dial the cosine score precisely
// across the enhance / ambiguous / create-new bands without depending on
// the behaviour of any particular tokenizer or hash function.
type stubEmbedder struct {
	vec  func(text string) []float32
	fail bool
}

func (s *stubEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if s.fail {
		return nil, errStubEmbedder
	}
	return s.vec(text), nil
}

var errStubEmbedder = stubEmbedderErr("stub-embedder failed")

type stubEmbedderErr string

func (e stubEmbedderErr) Error() string { return string(e) }

// l2norm returns v / ||v||. Inputs to cosineSimilarity are expected to be
// normalised, so test vectors are normalised here.
func l2norm(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	scale := float32(1.0 / float64Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * scale
	}
	return out
}

func float64Sqrt(x float64) float64 {
	// Newton-Raphson is overkill; standard library would do, but pulling
	// math here for one call would muddy imports for one test helper.
	z := x
	for i := 0; i < 24; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// addSkill appends an active skill with the given fields to b.skills.
// Tests use this directly rather than going through writeSkillProposalLocked
// so the similarity gate is exercised against a known catalog without
// running the full proposal pipeline.
func addSkill(b *Broker, name, description, content string) {
	b.skills = append(b.skills, teamSkill{
		Name:        name,
		Title:       name,
		Description: description,
		Content:     content,
		Status:      "active",
	})
}

func TestFindSimilarActive_NoSkills(t *testing.T) {
	b := newTestBroker(t)
	spec := teamSkill{
		Name:        "deploy-canary",
		Description: "Promote a canary build to production",
		Content:     "Run kubectl apply, wait for readiness, then ramp traffic.",
	}

	v := b.findSimilarActiveSkillLocked(context.Background(), spec)

	if v.Recommendation != "create_new" {
		t.Fatalf("recommendation = %q, want create_new", v.Recommendation)
	}
	if v.Existing != nil {
		t.Fatalf("Existing = %+v, want nil", v.Existing)
	}
	if v.Method != "jaccard-tokens" {
		t.Fatalf("method = %q, want jaccard-tokens", v.Method)
	}
}

func TestFindSimilarActive_ExactDuplicate(t *testing.T) {
	b := newTestBroker(t)
	addSkill(b,
		"send-invoice-reminder",
		"Send a reminder email to a customer with an open invoice",
		"Look up the invoice on Stripe and email the contact at day 7.",
	)

	spec := teamSkill{
		Name:        "send-invoice-reminder-v2", // different name so skip-self doesn't fire
		Description: "Send a reminder email to a customer with an open invoice",
		Content:     "Look up the invoice on Stripe and email the contact at day 7.",
	}

	v := b.findSimilarActiveSkillLocked(context.Background(), spec)

	// Score is high but not 1.0 because the names differ ("v2" suffix);
	// the description and body are identical, so Jaccard over the combined
	// fields lands well into the enhance band.
	if v.Score < 0.85 {
		t.Fatalf("score = %.3f, want >= 0.85 for exact-content duplicate", v.Score)
	}
	if v.Recommendation != "enhance_existing" {
		t.Fatalf("recommendation = %q, want enhance_existing", v.Recommendation)
	}
	if v.Existing == nil || v.Existing.Name != "send-invoice-reminder" {
		t.Fatalf("Existing = %+v, want pointer to send-invoice-reminder", v.Existing)
	}
}

func TestFindSimilarActive_NearDuplicateDifferentName(t *testing.T) {
	b := newTestBroker(t)
	addSkill(b,
		"send-invoice-reminder",
		"Send an invoice reminder email at day seven after issue",
		"Pull customer Stripe invoice, format reminder email, send via Postmark.",
	)
	addSkill(b,
		"deploy-canary",
		"Promote a canary build to production",
		"kubectl apply, wait for readiness probe, ramp traffic gradually.",
	)

	spec := teamSkill{
		Name:        "invoice-d7-reminder",
		Description: "Email a reminder for an invoice that is seven days overdue",
		Content:     "Lookup customer invoice on Stripe and send reminder email via Postmark template.",
	}

	v := b.findSimilarActiveSkillLocked(context.Background(), spec)

	if v.Recommendation != "enhance_existing" {
		t.Fatalf("recommendation = %q (score %.3f), want enhance_existing", v.Recommendation, v.Score)
	}
	if v.Existing == nil || v.Existing.Name != "send-invoice-reminder" {
		t.Fatalf("Existing = %+v, want pointer to send-invoice-reminder", v.Existing)
	}
}

func TestFindSimilarActive_DistinctSkills(t *testing.T) {
	b := newTestBroker(t)
	addSkill(b,
		"deploy-canary",
		"Promote a canary build to production",
		"kubectl apply, wait for readiness probe, ramp traffic gradually.",
	)
	addSkill(b,
		"renewal-reminder",
		"Email customers about an upcoming subscription renewal",
		"Query Stripe subscriptions, filter to renewals due in 30 days, send notice.",
	)

	spec := teamSkill{
		Name:        "rotate-secrets",
		Description: "Rotate Vault secrets for a service",
		Content:     "Authenticate with Vault, request a new lease, restart pods to pick up the new credential.",
	}

	v := b.findSimilarActiveSkillLocked(context.Background(), spec)

	if v.Recommendation != "create_new" {
		t.Fatalf("recommendation = %q (score %.3f), want create_new", v.Recommendation, v.Score)
	}
	if v.Existing != nil {
		t.Fatalf("Existing = %+v, want nil for distinct skills", v.Existing)
	}
}

func TestFindSimilarActive_AmbiguousRange(t *testing.T) {
	// Drives the embedding path with a stub that returns hand-crafted
	// vectors. The candidate is constructed to land at cosine ~0.78 against
	// the only catalog entry — squarely inside [0.70, 0.85).
	b := newTestBroker(t)
	addSkill(b,
		"draft-monthly-report",
		"Draft the monthly customer success report",
		"Pull churn, NPS, and expansion numbers; assemble into the standard template.",
	)

	const knownSkillText = "draft-monthly-report"
	const candidateText = "compile-quarterly-summary"

	b.skillEmbedder = &stubEmbedder{vec: func(text string) []float32 {
		// Two test vectors whose dot product is ~0.78 after L2 norm.
		// The candidate is matched by text prefix so the same call from
		// the gate consistently returns the same vector.
		switch {
		case strings.Contains(text, knownSkillText):
			return l2norm([]float32{1.0, 0.0, 0.0})
		case strings.Contains(text, candidateText):
			return l2norm([]float32{0.78, 0.6258, 0.0})
		}
		return l2norm([]float32{0, 1, 0})
	}}

	spec := teamSkill{
		Name:        candidateText,
		Description: "Assemble the quarterly customer summary",
		Content:     "Pull KPIs across all customers and compile a quarterly summary report.",
	}

	v := b.findSimilarActiveSkillLocked(context.Background(), spec)

	if v.Method != "embedding-cosine" {
		t.Fatalf("method = %q, want embedding-cosine", v.Method)
	}
	if v.Score < 0.70 || v.Score >= 0.85 {
		t.Fatalf("score = %.3f, want in [0.70, 0.85)", v.Score)
	}
	if v.Recommendation != "ambiguous" {
		t.Fatalf("recommendation = %q, want ambiguous", v.Recommendation)
	}
	if v.Existing == nil || v.Existing.Name != "draft-monthly-report" {
		t.Fatalf("Existing = %+v, want pointer to draft-monthly-report", v.Existing)
	}
}

func TestFindSimilarActive_SkipsSelf(t *testing.T) {
	b := newTestBroker(t)
	addSkill(b,
		"send-invoice-reminder",
		"Send a reminder email to a customer with an open invoice",
		"Look up the invoice on Stripe and email the contact at day 7.",
	)

	// In-place update: same name, same body. Without skip-self this would
	// return enhance_existing pointing at itself.
	spec := teamSkill{
		Name:        "send-invoice-reminder",
		Description: "Send a reminder email to a customer with an open invoice",
		Content:     "Look up the invoice on Stripe and email the contact at day 7.",
	}

	v := b.findSimilarActiveSkillLocked(context.Background(), spec)

	if v.Existing != nil {
		t.Fatalf("Existing = %+v, want nil (self-match must be skipped)", v.Existing)
	}
	if v.Recommendation != "create_new" {
		t.Fatalf("recommendation = %q, want create_new (catalog has no other skills to match)", v.Recommendation)
	}
}

func TestFindSimilarActive_FallbackToJaccard_NoEmbeddingProvider(t *testing.T) {
	b := newTestBroker(t)
	if b.skillEmbedder != nil {
		t.Fatalf("expected nil skillEmbedder by default, got %T", b.skillEmbedder)
	}
	addSkill(b,
		"send-invoice-reminder",
		"Send an invoice reminder email at day seven after issue",
		"Pull customer Stripe invoice, format reminder email, send via Postmark.",
	)

	spec := teamSkill{
		Name:        "invoice-d7-reminder",
		Description: "Email a reminder for an invoice that is seven days overdue",
		Content:     "Lookup customer invoice on Stripe and send reminder email via Postmark template.",
	}

	v := b.findSimilarActiveSkillLocked(context.Background(), spec)

	if v.Method != "jaccard-tokens" {
		t.Fatalf("method = %q, want jaccard-tokens (no embedder configured)", v.Method)
	}
	if v.Recommendation != "enhance_existing" {
		t.Fatalf("recommendation = %q (score %.3f), want enhance_existing", v.Recommendation, v.Score)
	}
}

func TestFindSimilarActive_ActiveOnly(t *testing.T) {
	b := newTestBroker(t)
	// Identical-content skill but flagged proposed/disabled/archived must
	// be excluded from the comparison set.
	for _, status := range []string{"proposed", "disabled", "archived", ""} {
		b.skills = append(b.skills, teamSkill{
			Name:        "send-invoice-reminder-" + status + "-x",
			Description: "Send a reminder email to a customer with an open invoice",
			Content:     "Look up the invoice on Stripe and email the contact at day 7.",
			Status:      status,
		})
	}

	spec := teamSkill{
		Name:        "send-invoice-reminder",
		Description: "Send a reminder email to a customer with an open invoice",
		Content:     "Look up the invoice on Stripe and email the contact at day 7.",
	}

	v := b.findSimilarActiveSkillLocked(context.Background(), spec)

	if v.Existing != nil {
		t.Fatalf("Existing = %+v, want nil (no active skills should be eligible)", v.Existing)
	}
	if v.Recommendation != "create_new" {
		t.Fatalf("recommendation = %q, want create_new (only inactive skills exist)", v.Recommendation)
	}
}

func TestFindSimilarActive_EmbedderFailureFallsBackToJaccard(t *testing.T) {
	// A flaky embedder must never block the proposal pipeline. The gate
	// degrades to Jaccard for the whole call rather than scoring against
	// a partial catalog.
	b := newTestBroker(t)
	b.skillEmbedder = &stubEmbedder{fail: true}
	addSkill(b,
		"send-invoice-reminder",
		"Send an invoice reminder email at day seven after issue",
		"Pull customer Stripe invoice, format reminder email, send via Postmark.",
	)

	spec := teamSkill{
		Name:        "invoice-d7-reminder",
		Description: "Email a reminder for an invoice that is seven days overdue",
		Content:     "Lookup customer invoice on Stripe and send reminder email via Postmark template.",
	}

	v := b.findSimilarActiveSkillLocked(context.Background(), spec)

	if v.Method != "jaccard-tokens" {
		t.Fatalf("method = %q, want jaccard-tokens fallback when embedder errors", v.Method)
	}
	if v.Recommendation != "enhance_existing" {
		t.Fatalf("recommendation = %q (score %.3f), want enhance_existing", v.Recommendation, v.Score)
	}
}

func TestFindSimilarActive_EmbeddingCacheReusesAcrossCalls(t *testing.T) {
	// Two compile passes against the same catalog should embed each
	// existing skill exactly once. Verifies the per-(slug, sha) cache
	// short-circuits the second call.
	b := newTestBroker(t)
	addSkill(b,
		"send-invoice-reminder",
		"Send an invoice reminder email",
		"body body body",
	)
	addSkill(b,
		"deploy-canary",
		"Promote a canary build",
		"kubectl apply",
	)

	calls := map[string]int{}
	b.skillEmbedder = &stubEmbedder{vec: func(text string) []float32 {
		calls[text]++
		switch {
		case strings.Contains(text, "invoice"):
			return l2norm([]float32{1, 0, 0})
		case strings.Contains(text, "canary") || strings.Contains(text, "kubectl"):
			return l2norm([]float32{0, 1, 0})
		}
		return l2norm([]float32{0, 0, 1})
	}}

	spec1 := teamSkill{
		Name:        "first-candidate",
		Description: "x",
		Content:     "y",
	}
	_ = b.findSimilarActiveSkillLocked(context.Background(), spec1)
	beforeSecond := len(calls)
	_ = b.findSimilarActiveSkillLocked(context.Background(), spec1)
	afterSecond := len(calls)

	if afterSecond != beforeSecond {
		t.Fatalf("expected no new embedder calls on second pass; before=%d after=%d", beforeSecond, afterSecond)
	}
}
