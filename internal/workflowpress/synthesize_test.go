package workflowpress

import (
	"encoding/json"
	"errors"
	"sort"
	"testing"
)

// synthesize_test.go is the first half of the Phase 3 proof: for each
// ground-truth example, Synthesize(Discover(evidence)) produces a DRAFT whose
// STRUCTURE matches the hand-authored contract under testdata/examples — same
// entities, states, events, action set and guards — with provenance present on
// every inferable element, and the draft is NOT yet frozen.

// names returns the Name field of each element, sorted, for set comparison. The
// generic accessor keeps the per-type boilerplate out of the assertions.
func sortedNames[T any](items []T, name func(T) string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = name(it)
	}
	sort.Strings(out)
	return out
}

func entityNames(es []Entity) []string {
	return sortedNames(es, func(e Entity) string { return e.Name })
}
func stateNames(ss []State) []string { return sortedNames(ss, func(s State) string { return s.Name }) }
func eventNames(es []Event) []string { return sortedNames(es, func(e Event) string { return e.Name }) }
func actionNames(as []Action) []string {
	return sortedNames(as, func(a Action) string { return a.Name })
}
func guardNames(gs []Guard) []string { return sortedNames(gs, func(g Guard) string { return g.Name }) }

// equalStringSets reports whether two name sets are identical.
func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// synthDraft runs the real Phase-2 -> Phase-3 path for one example: distil the
// raw evidence, then synthesise a draft from it.
func synthDraft(t *testing.T, name string) WorkflowSpec {
	t.Helper()
	research, err := Discover(loadEvidence(t, name))
	if err != nil {
		t.Fatalf("Discover(%s): %v", name, err)
	}
	draft, err := Synthesize(research)
	if err != nil {
		t.Fatalf("Synthesize(%s): %v", name, err)
	}
	return draft
}

// TestSynthesizeStructureMatchesExample is the core Phase-3 synthesis test: for
// each ground-truth example the synthesised draft has the SAME entities, states,
// events, action set and guards as the hand-authored contract. This proves the
// research signals are carried into a structurally-correct draft deterministically.
func TestSynthesizeStructureMatchesExample(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			draft := synthDraft(t, name)
			want := loadExample(t, name)

			if draft.ID != name {
				t.Errorf("draft id = %q, want %q", draft.ID, name)
			}
			if draft.Goal != want.Goal {
				t.Errorf("draft goal = %q, want %q", draft.Goal, want.Goal)
			}
			if draft.Operator != want.Operator {
				t.Errorf("draft operator = %q, want %q", draft.Operator, want.Operator)
			}

			cases := []struct {
				label    string
				got, exp []string
			}{
				{"entities", entityNames(draft.Entities), entityNames(want.Entities)},
				{"states", stateNames(draft.States), stateNames(want.States)},
				{"events", eventNames(draft.Events), eventNames(want.Events)},
				{"actions", actionNames(draft.Actions), actionNames(want.Actions)},
				{"guards", guardNames(draft.Guards), guardNames(want.Guards)},
			}
			for _, c := range cases {
				if !equalStringSets(c.got, c.exp) {
					t.Errorf("%s set mismatch:\n got  %v\n want %v", c.label, c.got, c.exp)
				}
			}
		})
	}
}

// TestSynthesizeActionKindsAndApprovalMatchExample proves the synthesised action
// graph carries the same kinds, the same idempotency marks, and the same
// approval gating as the hand-authored contract. This is where the trust-tier
// security rule shows up: every inferred/observed write requires approval.
func TestSynthesizeActionKindsAndApprovalMatchExample(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			draft := synthDraft(t, name)
			want := loadExample(t, name)

			wantByName := make(map[string]Action, len(want.Actions))
			for _, a := range want.Actions {
				wantByName[a.Name] = a
			}
			for _, got := range draft.Actions {
				exp, ok := wantByName[got.Name]
				if !ok {
					t.Errorf("synthesised unexpected action %q", got.Name)
					continue
				}
				if got.Kind != exp.Kind {
					t.Errorf("action %q kind = %q, want %q", got.Name, got.Kind, exp.Kind)
				}
				if got.Idempotent != exp.Idempotent {
					t.Errorf("action %q idempotent = %v, want %v", got.Name, got.Idempotent, exp.Idempotent)
				}
				if got.RequiresApproval != exp.RequiresApproval {
					t.Errorf("action %q requires_approval = %v, want %v", got.Name, got.RequiresApproval, exp.RequiresApproval)
				}
			}
		})
	}
}

// TestSynthesizeForcesApprovalOnInferredAndObservedWrites is the security
// invariant stated directly: every write-action the operator did NOT explicitly
// state must require approval. An operator-stated write may be looser; an
// inferred or merely-observed one may not.
func TestSynthesizeForcesApprovalOnInferredAndObservedWrites(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		draft := synthDraft(t, name)
		for _, a := range draft.Actions {
			if a.Kind.IsWrite() && !a.Provenance.IsOperatorStated() && !a.RequiresApproval {
				t.Errorf("%s: %s write %q is %s but does not require approval", name, a.Kind, a.Name, a.Provenance.TrustTier)
			}
		}
	}
}

// TestSynthesizeMarksProvenanceEverywhere proves every inferable element of the
// draft carries a valid trust tier and a confidence in [0,1] — the provenance
// the spec requires "present" on the draft.
func TestSynthesizeMarksProvenanceEverywhere(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			draft := synthDraft(t, name)

			check := func(label string, p Provenance) {
				if !p.TrustTier.Valid() {
					t.Errorf("%s: %s has invalid trust tier %q", name, label, p.TrustTier)
				}
				if p.Confidence < 0 || p.Confidence > 1 {
					t.Errorf("%s: %s confidence %v outside [0,1]", name, label, p.Confidence)
				}
			}
			for _, e := range draft.Entities {
				check("entity "+e.Name, e.Provenance)
			}
			for _, s := range draft.States {
				check("state "+s.Name, s.Provenance)
			}
			for _, ev := range draft.Events {
				check("event "+ev.Name, ev.Provenance)
			}
			for _, g := range draft.Guards {
				check("guard "+g.Name, g.Provenance)
			}
			for _, a := range draft.Actions {
				check("action "+a.Name, a.Provenance)
			}
			for _, ex := range draft.Exceptions {
				check("exception "+ex.Name, ex.Provenance)
			}
			for _, sla := range draft.SLAs {
				check("sla "+sla.Name, sla.Provenance)
			}
		})
	}
}

// TestSynthesizeMarksInferredWhereNotOperatorStated proves the draft is honest
// about what it does not know: every example must contain at least one inferred
// element (the spec says "mark inferred where not operator-stated"), and the
// inferred writes are the ones forced to require approval.
func TestSynthesizeMarksInferredWhereNotOperatorStated(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		draft := synthDraft(t, name)
		var inferredActions int
		for _, a := range draft.Actions {
			if a.Provenance.TrustTier == TrustInferred {
				inferredActions++
			}
		}
		if inferredActions == 0 {
			t.Errorf("%s: no action marked inferred; synthesis must mark inferred where not operator-stated", name)
		}
	}
}

// TestSynthesizeDerivesExceptionsFromEvidence proves the draft's exceptions come
// from the research's ObservedExceptions, not the blueprint — one exception per
// observed exception, carrying the observed handling.
func TestSynthesizeDerivesExceptionsFromEvidence(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ev := loadEvidence(t, name)
			draft := synthDraft(t, name)
			if len(draft.Exceptions) != len(ev.ObservedExceptions) {
				t.Fatalf("exceptions = %d, want %d (one per observed exception)", len(draft.Exceptions), len(ev.ObservedExceptions))
			}
			wantHandling := make(map[string]bool)
			for _, oe := range ev.ObservedExceptions {
				wantHandling[oe.HandledAs] = true
			}
			for _, ex := range draft.Exceptions {
				if ex.Provenance.TrustTier != TrustObserved {
					t.Errorf("exception %q tier = %q, want observed (it was seen in evidence)", ex.Name, ex.Provenance.TrustTier)
				}
				if !wantHandling[ex.Handling] {
					t.Errorf("exception %q handling %q not traced to any observed exception", ex.Name, ex.Handling)
				}
			}
		})
	}
}

// TestSynthesizeDerivesImprovementSignalsFromEdits proves an operator edit in
// the research becomes an operator-edit improvement signal watching the edited
// path — the live signal that should later propose an overlay. The signal is
// derived from the (already redacted) research the synthesis is handed, so it is
// compared against research.OperatorEdits, not the raw pre-redaction evidence:
// if redaction scrubbed a token out of the edited path, the signal faithfully
// watches the carried path.
func TestSynthesizeDerivesImprovementSignalsFromEdits(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			research, err := Discover(loadEvidence(t, name))
			if err != nil {
				t.Fatalf("Discover(%s): %v", name, err)
			}
			draft, err := Synthesize(research)
			if err != nil {
				t.Fatalf("Synthesize(%s): %v", name, err)
			}
			for _, edit := range research.OperatorEdits {
				found := false
				for _, sig := range draft.ImprovementSignals {
					if sig.Kind == SignalOperatorEdit && sig.Watch == edit.Path {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: operator edit on %q did not produce an operator-edit signal", name, edit.Path)
				}
			}
		})
	}
}

// TestSynthesizeIsDeterministic proves Synthesize is pure: the same research
// yields byte-identical drafts across runs.
func TestSynthesizeIsDeterministic(t *testing.T) {
	t.Parallel()
	research, err := Discover(loadEvidence(t, "trial-to-ae-routing"))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	first, err := Synthesize(research)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	second, err := Synthesize(research)
	if err != nil {
		t.Fatalf("Synthesize (second): %v", err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatalf("Synthesize not deterministic:\nfirst:  %s\nsecond: %s", a, b)
	}
}

// TestSynthesizeDraftCarriesVersionOne proves a fresh draft is version 1 — a
// draft that freezes becomes the first contract version.
func TestSynthesizeDraftCarriesVersionOne(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		draft := synthDraft(t, name)
		if draft.Version != draftVersion {
			t.Errorf("%s: draft version = %d, want %d", name, draft.Version, draftVersion)
		}
	}
}

// TestSynthesizeDoesNotAliasBlueprint proves a synthesised draft does not share
// backing slices with the package-global blueprint registry: mutating one
// draft's elements must not leak into the next synthesis.
func TestSynthesizeDoesNotAliasBlueprint(t *testing.T) {
	t.Parallel()
	research, err := Discover(loadEvidence(t, "trial-to-ae-routing"))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	first, err := Synthesize(research)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// Mutate the first draft's state and action in place.
	first.States[0].Name = "MUTATED"
	first.Actions[0].RequiresApproval = !first.Actions[0].RequiresApproval

	second, err := Synthesize(research)
	if err != nil {
		t.Fatalf("Synthesize (second): %v", err)
	}
	if second.States[0].Name == "MUTATED" {
		t.Error("mutating a draft state leaked into a later synthesis (blueprint aliased)")
	}
}

// TestSynthesizeRejectsEmptyWorkflowID proves the one structural input gate.
func TestSynthesizeRejectsEmptyWorkflowID(t *testing.T) {
	t.Parallel()
	_, err := Synthesize(WorkflowResearch{WorkflowID: "  "})
	if !errors.Is(err, ErrEmptyField) {
		t.Fatalf("Synthesize(empty id) = %v, want ErrEmptyField", err)
	}
}

// TestSynthesizeRejectsUnknownWorkflow proves synthesis needs a registered
// blueprint: an unknown workflow id has no skeleton and cannot be synthesised.
func TestSynthesizeRejectsUnknownWorkflow(t *testing.T) {
	t.Parallel()
	_, err := Synthesize(WorkflowResearch{WorkflowID: "no-such-workflow"})
	if !errors.Is(err, ErrNoBlueprint) {
		t.Fatalf("Synthesize(unknown) = %v, want ErrNoBlueprint", err)
	}
}

// TestSynthEntitiesDefaultOrderAndProvenance white-box-covers the synthEntities
// fallback paths the registered blueprints do not hit: when a blueprint leaves
// EntityOrder empty, entities are emitted sorted-by-name; an entity seen in real
// records is observed; an entity named only in the blueprint order (never seen)
// degrades to inferred; and EntityProvenance, when set, wins over both defaults.
func TestSynthEntitiesDefaultOrderAndProvenance(t *testing.T) {
	t.Parallel()
	research := WorkflowResearch{
		InferredSchemas: []InferredSchema{
			{Entity: "Zebra", SampleCount: 1, Fields: []InferredField{{Name: "a", PresentCount: 1}}},
			{Entity: "Apple", SampleCount: 3, Fields: []InferredField{{Name: "b", PresentCount: 3}}},
		},
	}

	t.Run("empty order sorts by name", func(t *testing.T) {
		t.Parallel()
		got := synthEntities(research, SynthesisBlueprint{})
		if len(got) != 2 || got[0].Name != "Apple" || got[1].Name != "Zebra" {
			t.Fatalf("default order = %v, want [Apple Zebra]", entityNames(got))
		}
		// Apple seen in 3 records -> observed 0.9; Zebra in 1 -> observed 0.8.
		if got[0].Provenance.TrustTier != TrustObserved || got[0].Provenance.Confidence != 0.9 {
			t.Errorf("Apple provenance = %+v, want observed 0.9", got[0].Provenance)
		}
		if got[1].Provenance.Confidence != 0.8 {
			t.Errorf("Zebra confidence = %v, want 0.8 (single record)", got[1].Provenance.Confidence)
		}
	})

	t.Run("entity named only in blueprint is inferred", func(t *testing.T) {
		t.Parallel()
		got := synthEntities(research, SynthesisBlueprint{EntityOrder: []string{"Ghost"}})
		if len(got) != 1 || got[0].Name != "Ghost" {
			t.Fatalf("got %v, want [Ghost]", entityNames(got))
		}
		if got[0].Provenance.TrustTier != TrustInferred {
			t.Errorf("unseen entity tier = %q, want inferred", got[0].Provenance.TrustTier)
		}
		if len(got[0].Fields) != 0 {
			t.Errorf("unseen entity has fields %v, want none", got[0].Fields)
		}
	})

	t.Run("EntityProvenance override wins", func(t *testing.T) {
		t.Parallel()
		bp := SynthesisBlueprint{
			EntityOrder:      []string{"Apple"},
			EntityProvenance: map[string]Provenance{"Apple": stated(1.0)},
		}
		got := synthEntities(research, bp)
		if got[0].Provenance.TrustTier != TrustOperatorStated {
			t.Errorf("override tier = %q, want operator-stated", got[0].Provenance.TrustTier)
		}
	})
}

// TestObservedEntityConfidence pins the record-count -> confidence ladder.
func TestObservedEntityConfidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		count int
		want  float64
	}{
		{6, 0.9}, {3, 0.9}, {2, 0.85}, {1, 0.8}, {0, lowConfidence},
	}
	for _, tc := range tests {
		if got := observedEntityConfidence(InferredSchema{SampleCount: tc.count}); got != tc.want {
			t.Errorf("observedEntityConfidence(count=%d) = %v, want %v", tc.count, got, tc.want)
		}
	}
}

// TestObservedExceptionConfidence pins the frequency -> confidence ladder.
func TestObservedExceptionConfidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		freq int
		want float64
	}{
		{5, 0.85}, {3, 0.85}, {2, 0.8}, {1, 0.7}, {0, 0.7},
	}
	for _, tc := range tests {
		if got := observedExceptionConfidence(tc.freq); got != tc.want {
			t.Errorf("observedExceptionConfidence(freq=%d) = %v, want %v", tc.freq, got, tc.want)
		}
	}
}

// TestSynthImprovementSignalsSkipsBlankEditPath proves an operator edit whose
// path redaction blanked out does not produce an empty-watch signal.
func TestSynthImprovementSignalsSkipsBlankEditPath(t *testing.T) {
	t.Parallel()
	research := WorkflowResearch{
		OperatorEdits: []OperatorEdit{
			{Path: "   ", After: "x"},
			{Path: "guards[g].expr", After: "y"},
		},
	}
	sigs := synthImprovementSignals(research, SynthesisBlueprint{})
	for _, s := range sigs {
		if s.Kind == SignalOperatorEdit && s.Watch == "" {
			t.Errorf("produced an operator-edit signal with an empty watch: %+v", s)
		}
	}
	var editSignals int
	for _, s := range sigs {
		if s.Kind == SignalOperatorEdit {
			editSignals++
		}
	}
	if editSignals != 1 {
		t.Errorf("operator-edit signals = %d, want 1 (blank path skipped)", editSignals)
	}
}

// TestSlugify covers the exception-name slugifier the synthesis relies on for
// stable, deterministic exception names.
func TestSlugify(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"The enrichment provider had no record.", "the_enrichment_provider_had_no_record"},
		{"  Two or more candidates tied!  ", "two_or_more_candidates_tied"},
		{"acct_1001 down 37%", "acct_1001_down_37"},
		{"", "exception"},
		{"!!!", "exception"},
	}
	for _, tc := range tests {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
