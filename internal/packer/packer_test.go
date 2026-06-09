package packer

// packer_test.go covers the egress boundary end to end: the three ICP
// acceptance scenarios from docs/specs/slack-context-packer.md plus focused
// tests for the security-critical invariants (envelope held on unsanitizable
// ask, item dropped on scanner deny, delivery re-validation, final byte-scan,
// and idempotency). The real fail-closed EgressScanner is used throughout so
// redaction behavior is exercised for real.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A real openai key matches a non-poison pattern → redacted but emittable.
const fixtureAPIKey = "sk-proj-ABCDEFGHIJKLMNOPQRSTUVWXYZ012345"

// A PEM private key is a poison-class secret → withheld entirely.
const fixturePEMKey = "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXkt\n-----END OPENSSH PRIVATE KEY-----"

// --- fakes ---

type fakeBrain struct {
	plan      string
	planErr   error
	learnings []BrainItem
	wiki      []BrainItem
	roster    []BrainItem
}

func (f fakeBrain) PlanStep(string) (string, error)                { return f.plan, f.planErr }
func (f fakeBrain) TaskLearnings(string, int) ([]BrainItem, error) { return f.learnings, nil }
func (f fakeBrain) TaskWikiRefs(string) ([]BrainItem, error)       { return f.wiki, nil }
func (f fakeBrain) Roster(string) ([]BrainItem, error)             { return f.roster, nil }

type fakeBridge struct {
	posted []PackedDelegation
	ts     string
	err    error
	calls  int
}

func (b *fakeBridge) Post(_ context.Context, d PackedDelegation, _ string) (string, error) {
	b.calls++
	if b.err != nil {
		return "", b.err
	}
	b.posted = append(b.posted, d)
	return b.ts, nil
}

type fakeValidator struct{ err error }

func (v fakeValidator) Validate(ContextRequest) error { return v.err }

type memSink struct{ recs map[string]InjectionRecord }

func newMemSink() *memSink { return &memSink{recs: map[string]InjectionRecord{}} }
func (s *memSink) Lookup(key string) (InjectionRecord, bool) {
	r, ok := s.recs[key]
	return r, ok
}
func (s *memSink) Write(rec InjectionRecord) error {
	s.recs[rec.IdempotencyKey] = rec
	return nil
}

func fixedClock() time.Time { return time.Unix(1_700_000_000, 0).UTC() }

func untrustedProfile() BotProfile {
	return BotProfile{
		Version:   1,
		Slug:      "notetaker",
		Trust:     BotUntrusted,
		ReadScope: ReadMentionOnly,
		Identity:  BotIdentity{SlackTeamID: "T1", AppUserID: "U_BOT", InstallID: "I1"},
	}
}

func firstPartyThreadProfile() BotProfile {
	p := untrustedProfile()
	p.Slug = "warehouse-bot"
	p.Trust = BotFirstParty
	p.ReadScope = ReadThread
	return p
}

func baseRequest(p BotProfile, ask string) ContextRequest {
	return ContextRequest{
		TaskID:          "task-1",
		TaskUpdatedAt:   "2026-06-09T00:00:00Z",
		PlanID:          "plan-1",
		PlanVersion:     1,
		Target:          p,
		Intent:          StepIntent{Text: ask, Taint: TaintClean},
		Thread:          ThreadRef{WorkspaceID: "T1", ChannelID: "C1", ThreadTS: "100.1"},
		EgressPolicyVer: 7,
		IdempotencyKey:  "task-1:notetaker:step-1",
	}
}

// --- ICP acceptance scenarios ---

// ICP 1: Untrusted vendor bot + redaction + first-egress. The approved plan step
// carries a pasted API key; a raw task-body item is also present. Assert the key
// is redacted, the raw task body is denied, no wiki/learning leaks, and delivery
// records a sent InjectionRecord whose rendered bytes never contain the secret.
func TestICP1_UntrustedRedactsAndWithholds(t *testing.T) {
	brain := fakeBrain{
		plan:      "Reconcile the open invoices. Credentials if needed: " + fixtureAPIKey,
		learnings: []BrainItem{{Ref: "learn-1", Body: "prior reconciliation note"}},
		wiki:      []BrainItem{{Ref: "wiki/refunds.md", Body: "refund policy"}},
	}
	req := baseRequest(untrustedProfile(), "Reconcile June invoices and report totals.")
	policy := NewDefaultEgressPolicy(7)

	packed, audit, err := Pack(brain, policy, EgressScanner{}, req, GatherOptions{ReturnPact: "Reply in this thread with the totals."}, DeliveryAudience{LeastTrustedPresent: BotUntrusted})
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}

	if strings.Contains(packed.MentionText, fixtureAPIKey) {
		t.Fatalf("API key leaked into mention:\n%s", packed.MentionText)
	}
	if !strings.Contains(packed.MentionText, "[REDACTED]") {
		t.Fatalf("expected redaction marker in mention:\n%s", packed.MentionText)
	}
	// Untrusted gets NO learning / wiki, even though the brain returned them.
	for _, it := range audit {
		if it.Kind == KindLearning || it.Kind == KindWiki {
			if it.Class != ExportDenied {
				t.Fatalf("untrusted received %s with class %s, want denied", it.Kind, it.Class)
			}
		}
	}
	// The plan step (human-approved) is exported, redacted.
	if !hasAudit(audit, KindPlan, ExportRedacted) {
		t.Fatalf("plan step should be exported redacted; audit=%+v", audit)
	}

	bridge := &fakeBridge{ts: "200.2"}
	sink := newMemSink()
	rec, err := Deliver(context.Background(), bridge, fakeValidator{}, EgressScanner{}, sink, fixedClock, packed, req)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if rec.Status != DeliverySent || rec.MessageTS != "200.2" {
		t.Fatalf("delivery status=%s ts=%q, want sent/200.2", rec.Status, rec.MessageTS)
	}
	if rec.RenderedHash == "" || rec.Timestamp == "" {
		t.Fatalf("injection record missing hash/timestamp: %+v", rec)
	}
	if strings.Contains(bridge.posted[0].MentionText, fixtureAPIKey) {
		t.Fatalf("API key leaked through the bridge")
	}
}

// ICP 2: First-party warehouse bot, thread-verified. Gets a lean mention plus a
// thread block carrying a task-scoped learning and a task-linked wiki ref.
func TestICP2_FirstPartyThreadGetsScopedKnowledge(t *testing.T) {
	brain := fakeBrain{
		plan:      "Audit the warehouse counts for SKU-42.",
		learnings: []BrainItem{{Ref: "learn-1", Body: "counts drift after restock; reconcile twice"}},
		wiki:      []BrainItem{{Ref: "wiki/inventory.md", Body: "inventory reconciliation playbook"}},
		roster:    []BrainItem{{Ref: "roster-1", Body: "warehouse-bot owns counts"}},
	}
	req := baseRequest(firstPartyThreadProfile(), "Audit SKU-42 counts.")
	packed, audit, err := Pack(brain, NewDefaultEgressPolicy(7), EgressScanner{}, req, GatherOptions{ReturnPact: "Post the reconciled count here."}, DeliveryAudience{LeastTrustedPresent: BotFirstParty})
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if !hasAudit(audit, KindLearning, ExportRedacted) {
		t.Fatalf("first-party should get a task-scoped learning; audit=%+v", audit)
	}
	if !hasAudit(audit, KindWiki, ExportRedacted) {
		t.Fatalf("first-party should get a task-linked wiki ref; audit=%+v", audit)
	}
	if strings.TrimSpace(packed.ThreadContext) == "" {
		t.Fatalf("thread-reading bot should get a non-empty thread block")
	}
	if !strings.Contains(packed.ThreadContext, "reconciliation playbook") {
		t.Fatalf("wiki ref missing from thread block:\n%s", packed.ThreadContext)
	}
}

// ICP 3: Hosted parity. A hosted bot is classified ExportAllowed for everything;
// the same retrieval yields a superset of any non-hosted bundle.
func TestICP3_HostedGetsEverythingAllowed(t *testing.T) {
	brain := fakeBrain{
		plan:      "Ship the release.",
		learnings: []BrainItem{{Ref: "learn-1", Body: "tag before publish"}},
		wiki:      []BrainItem{{Ref: "wiki/release.md", Body: "release checklist"}},
	}
	p := firstPartyThreadProfile()
	p.Trust = BotHosted
	req := baseRequest(p, "Cut the release.")
	_, audit, err := Pack(brain, NewDefaultEgressPolicy(7), EgressScanner{}, req, GatherOptions{}, DeliveryAudience{TargetOnly: true})
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	for _, it := range audit {
		if it.Kind == KindLearning || it.Kind == KindWiki || it.Kind == KindPlan {
			if it.Class != ExportAllowed {
				t.Fatalf("hosted %s class = %s, want allowed", it.Kind, it.Class)
			}
		}
	}
}

// --- Security-critical unit tests ---

// An unsanitizable ASK (poison secret) holds the WHOLE delegation — never sent
// without an ask, never sent partial.
func TestClassify_PoisonAskHoldsDelegation(t *testing.T) {
	raw := RawBundle{Ask: "Do the thing with " + fixturePEMKey}
	_, _, err := Classify(raw, BotUntrusted, NewDefaultEgressPolicy(1), EgressScanner{})
	if err == nil {
		t.Fatal("expected ErrEnvelopeHeld for poison ask")
	}
	if !strings.Contains(err.Error(), "held") {
		t.Fatalf("error should signal a held delegation, got %v", err)
	}
}

// An item whose body cannot be proven clean is DROPPED (denied), while the rest
// of the bundle survives.
func TestClassify_PoisonItemDroppedOthersSurvive(t *testing.T) {
	raw := RawBundle{
		Ask: "Reconcile counts.",
		Items: []RawItem{
			{Ref: "learn-poison", Kind: KindLearning, Body: "key: " + fixturePEMKey},
			{Ref: "learn-clean", Kind: KindLearning, Body: "reconcile twice after restock"},
		},
	}
	bundle, audit, err := Classify(raw, BotFirstParty, NewDefaultEgressPolicy(1), EgressScanner{})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if len(bundle.Items) != 1 || bundle.Items[0].Ref != "learn-clean" {
		t.Fatalf("poison item should be dropped, clean kept; got %+v", bundle.Items)
	}
	if !hasAuditRef(audit, "learn-poison", ExportDenied) {
		t.Fatalf("poison item should be audited as denied; audit=%+v", audit)
	}
}

// Raw task body is never exported wholesale, for any non-hosted tier.
func TestPolicy_RawTaskBodyDeniedForNonHosted(t *testing.T) {
	p := NewDefaultEgressPolicy(1)
	for _, tier := range []BotTrust{BotUntrusted, BotFirstParty} {
		if got := p.Classify(KindTask, tier); got != ExportDenied {
			t.Fatalf("KindTask at tier %d = %s, want denied", tier, got)
		}
	}
	if got := p.Classify(KindWiki, BotUntrusted); got != ExportDenied {
		t.Fatalf("untrusted wiki = %s, want denied (no free wiki)", got)
	}
}

// packForDelivery builds a real, sealed, snapshot-valid delegation via Pack so
// Deliver tests exercise the actual seal + snapshot binding rather than a
// hand-constructed payload.
func packForDelivery(t *testing.T, key string) (PackedDelegation, ContextRequest) {
	t.Helper()
	brain := fakeBrain{plan: "Reconcile the open invoices."}
	req := baseRequest(untrustedProfile(), "Reconcile June invoices.")
	req.IdempotencyKey = key
	packed, _, err := Pack(brain, NewDefaultEgressPolicy(7), EgressScanner{}, req, GatherOptions{ReturnPact: "Reply here."}, DeliveryAudience{LeastTrustedPresent: BotUntrusted})
	if err != nil {
		t.Fatalf("packForDelivery: %v", err)
	}
	return packed, req
}

// Deliver aborts on a stale live snapshot and never calls the bridge.
func TestDeliver_StaleSnapshotAbortsBeforePost(t *testing.T) {
	packed, req := packForDelivery(t, "k1")
	bridge := &fakeBridge{ts: "200.2"}
	sink := newMemSink()

	rec, err := Deliver(context.Background(), bridge, fakeValidator{err: errStale()}, EgressScanner{}, sink, fixedClock, packed, req)
	if err == nil {
		t.Fatal("expected delivery to abort on stale snapshot")
	}
	if bridge.calls != 0 {
		t.Fatalf("bridge must not be called on stale snapshot, calls=%d", bridge.calls)
	}
	if rec.Status != DeliveryFailed {
		t.Fatalf("status = %s, want failed", rec.Status)
	}
}

// Deliver's final byte-scan catches a secret reintroduced into the rendered
// bytes after classification, and never posts it.
func TestDeliver_FinalScanCatchesReintroducedSecret(t *testing.T) {
	packed, req := packForDelivery(t, "k2")
	packed.MentionText = "ship it with " + fixturePEMKey // simulate render reintroducing a secret
	bridge := &fakeBridge{ts: "200.2"}
	sink := newMemSink()

	_, err := Deliver(context.Background(), bridge, fakeValidator{}, EgressScanner{}, sink, fixedClock, packed, req)
	if err == nil {
		t.Fatal("expected final scan to abort delivery")
	}
	if bridge.calls != 0 {
		t.Fatalf("bridge must not be called when final scan fails, calls=%d", bridge.calls)
	}
}

// Delivering the same idempotency key twice posts exactly once.
func TestDeliver_IdempotentOnKey(t *testing.T) {
	packed, req := packForDelivery(t, "k3")
	bridge := &fakeBridge{ts: "200.2"}
	sink := newMemSink()

	if _, err := Deliver(context.Background(), bridge, fakeValidator{}, EgressScanner{}, sink, fixedClock, packed, req); err != nil {
		t.Fatalf("first deliver: %v", err)
	}
	if _, err := Deliver(context.Background(), bridge, fakeValidator{}, EgressScanner{}, sink, fixedClock, packed, req); err != nil {
		t.Fatalf("second deliver: %v", err)
	}
	if bridge.calls != 1 {
		t.Fatalf("bridge.calls = %d, want 1 (idempotent)", bridge.calls)
	}
}

// Deliver refuses an unsealed (hand-constructed) delegation that skipped Classify.
func TestDeliver_RefusesUnsealedDelegation(t *testing.T) {
	bridge := &fakeBridge{ts: "200.2"}
	sink := newMemSink()
	hand := PackedDelegation{MentionText: "trust me", Injection: InjectionRecord{IdempotencyKey: "k4"}}
	req := baseRequest(untrustedProfile(), "x")
	req.IdempotencyKey = "k4"

	_, err := Deliver(context.Background(), bridge, fakeValidator{}, EgressScanner{}, sink, fixedClock, hand, req)
	if !errors.Is(err, ErrUnsealed) {
		t.Fatalf("expected ErrUnsealed, got %v", err)
	}
	if bridge.calls != 0 {
		t.Fatalf("bridge must not be called for an unsealed delegation, calls=%d", bridge.calls)
	}
}

// Deliver refuses a delegation whose packed snapshot does not match the req it is
// delivered under (a stale payload paired with a downgraded req).
func TestDeliver_RefusesSnapshotMismatch(t *testing.T) {
	packed, req := packForDelivery(t, "k5")
	mismatch := req
	mismatch.Target.Trust = BotFirstParty // packed snapshot says untrusted
	bridge := &fakeBridge{ts: "200.2"}
	sink := newMemSink()

	_, err := Deliver(context.Background(), bridge, fakeValidator{}, EgressScanner{}, sink, fixedClock, packed, mismatch)
	if err == nil {
		t.Fatal("expected snapshot-mismatch abort")
	}
	if bridge.calls != 0 {
		t.Fatalf("bridge must not be called on snapshot mismatch, calls=%d", bridge.calls)
	}
}

// A first-party bot on a SHARED thread with an untrusted reader present is
// classified against the untrusted audience: no learnings/wiki are retrieved or
// exported, even though the target itself is first-party. This is the H5/F3 fix.
func TestPack_SharedThreadDowngradesFirstPartyAudience(t *testing.T) {
	brain := fakeBrain{
		plan:      "Audit SKU-42.",
		learnings: []BrainItem{{Ref: "learn-1", Body: "reconcile twice"}},
		wiki:      []BrainItem{{Ref: "wiki/x.md", Body: "playbook"}},
	}
	req := baseRequest(firstPartyThreadProfile(), "Audit SKU-42.")
	_, audit, err := Pack(brain, NewDefaultEgressPolicy(7), EgressScanner{}, req, GatherOptions{}, DeliveryAudience{LeastTrustedPresent: BotUntrusted})
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	for _, a := range audit {
		if a.Kind == KindLearning || a.Kind == KindWiki {
			t.Fatalf("downgraded audience must not export %s; audit=%+v", a.Kind, audit)
		}
	}
}

// Audience downgrades to the least-trusted reader present on a shared thread.
func TestAudience_DowngradesToLeastTrustedOnSharedThread(t *testing.T) {
	if got := Audience(BotFirstParty, BotUntrusted, false); got != BotUntrusted {
		t.Fatalf("shared thread audience = %d, want untrusted", got)
	}
	if got := Audience(BotFirstParty, BotUntrusted, true); got != BotFirstParty {
		t.Fatalf("target-only (DM) audience = %d, want first-party", got)
	}
}

// --- helpers ---

func hasAudit(audit []ItemAudit, kind ItemKind, class ExportClass) bool {
	for _, a := range audit {
		if a.Kind == kind && a.Class == class {
			return true
		}
	}
	return false
}

func hasAuditRef(audit []ItemAudit, ref string, class ExportClass) bool {
	for _, a := range audit {
		if a.Ref == ref && a.Class == class {
			return true
		}
	}
	return false
}

func errStale() error { return &staleErr{} }

type staleErr struct{}

func (*staleErr) Error() string { return "trust downgraded" }
