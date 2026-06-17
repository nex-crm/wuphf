package workflowpress

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// improvement_test.go locks down the overlay loop — the fifth press artifact:
//
//   - an overlay that BREAKS a fixture is REJECTED on replay, and the live spec is
//     left untouched (the kernel never accepts an unproven overlay);
//   - an overlay that PASSES replay is accepted, folded into the EXISTING spec, and
//     bumps the version (prefer-update, not a new workflow);
//   - base-version scoping rejects a stale overlay;
//   - a malformed / unsound overlay is rejected before replay;
//   - the kernel's own source files are UNCHANGED by any overlay activity (overlays
//     patch the per-workflow spec, never the kernel).

// mustEncodePatch encodes an OverlayPatch or fails the test.
func mustEncodePatch(t *testing.T, p OverlayPatch) []byte {
	t.Helper()
	b, err := EncodeOverlayPatch(p)
	if err != nil {
		t.Fatalf("EncodeOverlayPatch: %v", err)
	}
	return b
}

// TestOverlayThatPassesIsAcceptedAndBumpsVersion proves the happy path: an overlay
// that adds an improvement signal (a behaviour-neutral tune) replays cleanly and
// is accepted, updating the EXISTING spec in place at version+1.
func TestOverlayThatPassesIsAcceptedAndBumpsVersion(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "trial-to-ae-routing")
	store := NewOverlayStore(base)

	ov := Overlay{
		WorkflowID:  base.ID,
		BaseVersion: base.Version,
		Origin:      string(SignalRecurringException),
		Patch: mustEncodePatch(t, OverlayPatch{Ops: []OverlayOp{
			{
				Kind: OpAddImprovementSignal,
				Signal: &ImprovementSignal{
					Kind:      SignalRecurringException,
					Watch:     "enrichment-provider timeout",
					Threshold: "3/week",
				},
				Provenance: inferred(0.7),
			},
		}}),
	}

	if err := store.Propose(context.Background(), ov); err != nil {
		t.Fatalf("Propose: %v", err)
	}

	newSpec, report, err := store.AcceptIfProven(context.Background(), ov)
	if err != nil {
		t.Fatalf("AcceptIfProven: %v", err)
	}
	if report == nil || !report.Passed {
		t.Fatalf("expected a passing replay report, got %+v", report)
	}
	if newSpec == nil {
		t.Fatal("expected the new live spec on a passing overlay")
	}
	// prefer-update: same id, version bumped by exactly one.
	if newSpec.ID != base.ID {
		t.Errorf("accepted spec id = %q, want %q (prefer-update: same workflow)", newSpec.ID, base.ID)
	}
	if newSpec.Version != base.Version+1 {
		t.Errorf("accepted spec version = %d, want %d", newSpec.Version, base.Version+1)
	}
	// The new signal is folded in.
	if len(newSpec.ImprovementSignals) != len(base.ImprovementSignals)+1 {
		t.Errorf("expected one more improvement signal, base had %d, new has %d",
			len(base.ImprovementSignals), len(newSpec.ImprovementSignals))
	}
	// The live spec in the store is the updated one (UPDATE in place, not a new id).
	live, ok := store.Spec(base.ID)
	if !ok {
		t.Fatal("live spec missing after accept")
	}
	if live.Version != base.Version+1 {
		t.Errorf("store live version = %d, want %d", live.Version, base.Version+1)
	}
	// The accepted overlay is dropped from the proposed queue.
	if pq := store.Proposed(base.ID); len(pq) != 0 {
		t.Errorf("proposed queue should be empty after accept, has %d", len(pq))
	}
}

// TestOverlayThatBreaksFixtureIsRejectedOnReplay is the load-bearing negative
// path: an overlay that tightens the ICP guard so high that the contract's own
// "icp_fit_routes_and_posts" scenario no longer routes is REJECTED on replay — the
// fixture no longer reproduces — and the live spec is left UNCHANGED.
func TestOverlayThatBreaksFixtureIsRejectedOnReplay(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "trial-to-ae-routing")
	store := NewOverlayStore(base)

	// Tighten the guard to an unreachable threshold. The icp_fit scenario (score 82)
	// expects routing; with this guard it will not route, so fixture-replay fails.
	ov := Overlay{
		WorkflowID:  base.ID,
		BaseVersion: base.Version,
		Origin:      string(SignalOperatorEdit),
		Patch: mustEncodePatch(t, OverlayPatch{Ops: []OverlayOp{
			{
				Kind:       OpSetGuardExpr,
				Target:     "score_meets_icp",
				Value:      "icp_score >= 999",
				Provenance: stated(1.0),
			},
		}}),
	}

	newSpec, report, err := store.AcceptIfProven(context.Background(), ov)
	if err != nil {
		t.Fatalf("AcceptIfProven returned an unexpected error: %v", err)
	}
	if newSpec != nil {
		t.Fatal("a fixture-breaking overlay must NOT be accepted")
	}
	if report == nil || report.Passed {
		t.Fatalf("expected a FAILING replay report, got %+v", report)
	}
	// The fixture-replay check is the one that must catch the broken scenario.
	replay := findingByName(t, report, checkFixtureReplay)
	if replay.Passed {
		t.Errorf("fixture-replay passed despite the broken guard; detail=%s", replay.Detail)
	}
	// The live spec is untouched: same version, original guard expression.
	live, ok := store.Spec(base.ID)
	if !ok {
		t.Fatal("live spec missing")
	}
	if live.Version != base.Version {
		t.Errorf("live version changed to %d on a rejected overlay; must stay %d", live.Version, base.Version)
	}
	for _, g := range live.Guards {
		if g.Name == "score_meets_icp" && g.Expr == "icp_score >= 999" {
			t.Error("live spec's guard was mutated by a REJECTED overlay")
		}
	}
	// A rejected overlay cannot be force-accepted: Accept refuses it (never replayed).
	if _, err := store.Accept(context.Background(), ov); err == nil {
		t.Error("Accept must refuse an overlay that did not pass a replay")
	}
}

// TestOverlayRejectsStaleBaseVersion proves base-version scoping: an overlay
// targeting a version the live spec has moved past is rejected.
func TestOverlayRejectsStaleBaseVersion(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "renewal-risk-sweep")
	store := NewOverlayStore(base)
	stale := Overlay{
		WorkflowID:  base.ID,
		BaseVersion: base.Version + 5, // never matches
		Origin:      string(SignalSLAMiss),
		Patch: mustEncodePatch(t, OverlayPatch{Ops: []OverlayOp{
			{Kind: OpSetSLAThreshold, Target: "usage_freshness", Value: "12h", Provenance: stated(1.0)},
		}}),
	}
	if _, _, err := store.AcceptIfProven(context.Background(), stale); err == nil {
		t.Fatal("expected a base-version mismatch error")
	}
}

// TestOverlayRejectsUnsoundPatch proves a patch that produces an unsound or unsafe
// state machine is rejected BEFORE replay — the structural half of the gate still
// applies to a patched contract. Here the overlay adds a verification scenario
// that references an undefined event, which Validate rejects.
func TestOverlayRejectsUnsoundPatch(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "inbound-lead-dedupe-merge")
	store := NewOverlayStore(base)
	bad := Overlay{
		WorkflowID:  base.ID,
		BaseVersion: base.Version,
		Origin:      string(SignalOperatorEdit),
		Patch: mustEncodePatch(t, OverlayPatch{Ops: []OverlayOp{
			{
				Kind: OpAddVerificationScenario,
				Scenario: &VerificationScenario{
					Name: "references_a_ghost_event",
					When: "no_such_event", // undefined -> Validate rejects the candidate
					ExpectTransitions: []Transition{
						{From: "received", To: "matched"},
					},
				},
				Provenance: inferred(0.5),
			},
		}}),
	}
	live, _ := store.Spec(base.ID)
	if _, err := store.Apply(context.Background(), live, bad); err == nil {
		t.Fatal("expected Apply to reject a patch that produces an unsound spec")
	}
}

// TestOverlayRejectsSetOnUnknownElement proves a set-* op that names an element the
// contract does not carry is rejected (an overlay must address real elements).
func TestOverlayRejectsSetOnUnknownElement(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "trial-to-ae-routing")
	store := NewOverlayStore(base)
	bad := Overlay{
		WorkflowID:  base.ID,
		BaseVersion: base.Version,
		Origin:      string(SignalOperatorEdit),
		Patch: mustEncodePatch(t, OverlayPatch{Ops: []OverlayOp{
			{Kind: OpSetGuardExpr, Target: "no_such_guard", Value: "x >= 1", Provenance: stated(1.0)},
		}}),
	}
	live, _ := store.Spec(base.ID)
	if _, err := store.Apply(context.Background(), live, bad); err == nil {
		t.Fatal("expected Apply to reject a set-guard op on an unknown guard")
	}
}

// TestProposeRejectsMalformedPatch proves a proposal with an empty or undecodable
// patch is rejected at the door, and an overlay for an unknown workflow is refused.
func TestProposeRejectsMalformedPatch(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "trial-to-ae-routing")
	store := NewOverlayStore(base)

	if err := store.Propose(context.Background(), Overlay{WorkflowID: base.ID, BaseVersion: base.Version, Patch: nil}); err == nil {
		t.Error("expected an error for an empty patch")
	}
	if err := store.Propose(context.Background(), Overlay{WorkflowID: base.ID, BaseVersion: base.Version, Patch: []byte("{not json")}); err == nil {
		t.Error("expected an error for an undecodable patch")
	}
	goodPatch := mustEncodePatch(t, OverlayPatch{Ops: []OverlayOp{
		{Kind: OpAddImprovementSignal, Signal: &ImprovementSignal{Kind: SignalSLAMiss, Watch: "x"}, Provenance: inferred(0.5)},
	}})
	if err := store.Propose(context.Background(), Overlay{WorkflowID: "unknown-workflow", BaseVersion: 1, Patch: goodPatch}); err == nil {
		t.Error("expected an error for an overlay naming an unknown workflow")
	}
}

// TestOverlayPreferUpdateDoesNotCreateNewWorkflow proves the convergence rule: two
// successive accepted overlays leave exactly ONE workflow id in the store at the
// twice-bumped version — accepting an overlay updates the existing workflow, it
// never proliferates a new one.
func TestOverlayPreferUpdateDoesNotCreateNewWorkflow(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "trial-to-ae-routing")
	store := NewOverlayStore(base)

	first := Overlay{
		WorkflowID: base.ID, BaseVersion: base.Version, Origin: string(SignalOperatorEdit),
		Patch: mustEncodePatch(t, OverlayPatch{Ops: []OverlayOp{
			{Kind: OpAddImprovementSignal, Signal: &ImprovementSignal{Kind: SignalOperatorEdit, Watch: "a"}, Provenance: inferred(0.6)},
		}}),
	}
	if _, _, err := store.AcceptIfProven(context.Background(), first); err != nil {
		t.Fatalf("first AcceptIfProven: %v", err)
	}
	// The second overlay must target the NEW (bumped) version — prefer-update means
	// the workflow moved on.
	second := Overlay{
		WorkflowID: base.ID, BaseVersion: base.Version + 1, Origin: string(SignalSLAMiss),
		Patch: mustEncodePatch(t, OverlayPatch{Ops: []OverlayOp{
			{Kind: OpSetSLAThreshold, Target: "route_freshness", Value: "3m", Provenance: stated(1.0)},
		}}),
	}
	newSpec, _, err := store.AcceptIfProven(context.Background(), second)
	if err != nil {
		t.Fatalf("second AcceptIfProven: %v", err)
	}
	if newSpec.Version != base.Version+2 {
		t.Errorf("after two accepts version = %d, want %d", newSpec.Version, base.Version+2)
	}
	// Exactly one workflow id remains: the original, updated in place.
	store.mu.Lock()
	ids := make([]string, 0, len(store.specs))
	for id := range store.specs {
		ids = append(ids, id)
	}
	store.mu.Unlock()
	if len(ids) != 1 || ids[0] != base.ID {
		t.Errorf("store holds %v, want exactly [%s] (prefer-update, no proliferation)", ids, base.ID)
	}
}

// TestOverlaysNeverMutateTheKernel proves the architectural invariant: the kernel's
// own source files are UNCHANGED by overlay propose/apply/accept activity. Overlays
// patch the per-workflow spec only; the kernel's code is never a patch target.
//
// It hashes every Go source file in the package before and after a full overlay
// loop (propose -> apply -> replay -> accept, plus a rejected overlay) and asserts
// not one byte changed.
func TestOverlaysNeverMutateTheKernel(t *testing.T) {
	t.Parallel()
	kernelFiles := kernelSourceFiles(t)
	before := hashFiles(t, kernelFiles)

	base := loadExample(t, "trial-to-ae-routing")
	store := NewOverlayStore(base)

	// A passing overlay (accepted) ...
	good := Overlay{
		WorkflowID: base.ID, BaseVersion: base.Version, Origin: string(SignalOperatorEdit),
		Patch: mustEncodePatch(t, OverlayPatch{Ops: []OverlayOp{
			{Kind: OpAddImprovementSignal, Signal: &ImprovementSignal{Kind: SignalOperatorEdit, Watch: "k"}, Provenance: inferred(0.6)},
		}}),
	}
	if err := store.Propose(context.Background(), good); err != nil {
		t.Fatalf("Propose good: %v", err)
	}
	if _, _, err := store.AcceptIfProven(context.Background(), good); err != nil {
		t.Fatalf("AcceptIfProven good: %v", err)
	}
	// ... and a rejected overlay (broken fixture) both run against the store.
	bad := Overlay{
		WorkflowID: base.ID, BaseVersion: base.Version + 1, Origin: string(SignalOperatorEdit),
		Patch: mustEncodePatch(t, OverlayPatch{Ops: []OverlayOp{
			{Kind: OpSetGuardExpr, Target: "score_meets_icp", Value: "icp_score >= 999", Provenance: stated(1.0)},
		}}),
	}
	if _, report, _ := store.AcceptIfProven(context.Background(), bad); report == nil || report.Passed {
		t.Fatal("expected the broken overlay to be rejected")
	}

	after := hashFiles(t, kernelFiles)
	if len(before) != len(after) {
		t.Fatalf("kernel file set changed: %d -> %d", len(before), len(after))
	}
	for path, h := range before {
		if after[path] != h {
			t.Errorf("kernel file %s was mutated by overlay activity", path)
		}
	}
}

// replayedLen returns the number of entries in the store's internal replayed map,
// under the lock. It is the test accessor for fix #6's leak assertion.
func replayedLen(s *MemoryOverlayStore) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.replayed)
}

// TestAcceptDoesNotLeakReplayedOnRaceLoser is the regression for the replayed-map
// leak: AcceptIfProven records a proven candidate under overlayKey(ov) and then
// calls Accept, which re-checks base-version scoping against the CURRENT live spec.
// On the race-loser path (a concurrent accept moved the live version on) Accept
// used to return the base-mismatch error BEFORE deleting the replayed entry, so the
// candidate leaked forever. After the fix the entry is consumed on every path past
// the not-replayed guard. We drive the loser path deterministically: record a
// proven candidate, then move the live spec on so the overlay's base version no
// longer matches, and assert Accept fails AND the replayed map is empty.
func TestAcceptDoesNotLeakReplayedOnRaceLoser(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "trial-to-ae-routing")
	store := NewOverlayStore(base)

	ov := Overlay{
		WorkflowID:  base.ID,
		BaseVersion: base.Version,
		Origin:      string(SignalOperatorEdit),
		Patch: mustEncodePatch(t, OverlayPatch{Ops: []OverlayOp{
			{Kind: OpAddImprovementSignal, Signal: &ImprovementSignal{Kind: SignalOperatorEdit, Watch: "race"}, Provenance: inferred(0.6)},
		}}),
	}

	// Build and record a proven candidate exactly as AcceptIfProven would, then
	// simulate a concurrent accept by bumping the live spec's version so this
	// overlay's BaseVersion no longer matches.
	live, _ := store.Spec(base.ID)
	cand, err := store.Apply(context.Background(), live, ov)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	store.mu.Lock()
	store.replayed[overlayKey(ov)] = cand
	store.specs[base.ID].Version = base.Version + 1 // a concurrent accept moved it on
	store.mu.Unlock()

	if _, err := store.Accept(context.Background(), ov); err == nil {
		t.Fatal("Accept must fail when the live version has moved past the overlay's base")
	}
	if n := replayedLen(store); n != 0 {
		t.Errorf("replayed map leaked a stale entry on the race-loser path: len = %d, want 0", n)
	}
}

// TestAcceptNotReplayedDoesNotConsume proves the not-replayed guard stays pure: an
// Accept with no recorded candidate returns ErrOverlayNotReplayed and does not
// delete anything (it has nothing to consume). This guards the fix #6 reorder from
// accidentally consuming entries it should not.
func TestAcceptNotReplayedDoesNotConsume(t *testing.T) {
	t.Parallel()
	base := loadExample(t, "trial-to-ae-routing")
	store := NewOverlayStore(base)

	// Seed an UNRELATED replayed entry; an Accept for a different overlay must not
	// touch it.
	other := Overlay{WorkflowID: base.ID, BaseVersion: base.Version, Origin: "other", Patch: []byte("x")}
	store.mu.Lock()
	store.replayed[overlayKey(other)] = cloneSpec(*base)
	store.mu.Unlock()

	unrelated := Overlay{WorkflowID: base.ID, BaseVersion: base.Version, Origin: "unrelated", Patch: []byte("y")}
	if _, err := store.Accept(context.Background(), unrelated); err == nil {
		t.Fatal("expected ErrOverlayNotReplayed for an overlay with no recorded candidate")
	}
	if n := replayedLen(store); n != 1 {
		t.Errorf("not-replayed Accept consumed an unrelated entry: len = %d, want 1", n)
	}
}

// TestOverlayKeyIsCollisionResistant is the regression for the separator-collision
// in overlayKey. The old scheme concatenated WorkflowID + "@" + version + "/" +
// Origin + "#" + patch with no escaping, so a separator inside one field could be
// absorbed into an adjacent field and make two DISTINCT overlays share a key. The
// classic collision: Origin "x#y" with an empty patch vs Origin "x" with patch
// "#y" — both rendered "...#x#y" under the old scheme. The fix hashes the canonical
// JSON of the identity fields, so the keys must now differ.
func TestOverlayKeyIsCollisionResistant(t *testing.T) {
	t.Parallel()

	// Each pair is two DISTINCT overlays that produced the SAME key under the old
	// unescaped "WorkflowID@version/Origin#Patch" scheme, because an unescaped
	// separator in one field is absorbed into the adjacent field. These are the
	// load-bearing assertions: they fail under the old scheme, pass under the hash.
	collisions := []struct {
		name string
		a, b Overlay
	}{
		{
			// Origin/Patch boundary: old key for both was "wf@1/x#y#z".
			name: "origin_absorbs_patch_hash",
			a:    Overlay{WorkflowID: "wf", BaseVersion: 1, Origin: "x#y", Patch: []byte("z")},
			b:    Overlay{WorkflowID: "wf", BaseVersion: 1, Origin: "x", Patch: []byte("y#z")},
		},
		{
			// A longer split at the same boundary: old key for both was
			// "wf@1/a#b#c#d".
			name: "origin_absorbs_more_patch_hash",
			a:    Overlay{WorkflowID: "wf", BaseVersion: 1, Origin: "a#b", Patch: []byte("c#d")},
			b:    Overlay{WorkflowID: "wf", BaseVersion: 1, Origin: "a", Patch: []byte("b#c#d")},
		},
	}
	for _, c := range collisions {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if overlayKey(c.a) == overlayKey(c.b) {
				t.Errorf("overlayKey collided for distinct overlays: %q", overlayKey(c.a))
			}
		})
	}

	// Identical overlays must still produce identical keys (stable identity).
	e := Overlay{WorkflowID: "wf", BaseVersion: 2, Origin: "z", Patch: []byte("same")}
	f := Overlay{WorkflowID: "wf", BaseVersion: 2, Origin: "z", Patch: []byte("same")}
	if overlayKey(e) != overlayKey(f) {
		t.Error("overlayKey is not stable for identical overlays")
	}
}

// kernelSourceFiles lists the package's non-test Go source files (the kernel's
// code). They are what "the kernel never changes" means concretely.
func kernelSourceFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package dir: %v", err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" {
			continue
		}
		if len(name) > len("_test.go") && name[len(name)-len("_test.go"):] == "_test.go" {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no kernel source files found")
	}
	return files
}

// hashFiles returns a path->sha256 map for the given files.
func hashFiles(t *testing.T, files []string) map[string][32]byte {
	t.Helper()
	out := make(map[string][32]byte, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		out[f] = sha256.Sum256(b)
	}
	return out
}
