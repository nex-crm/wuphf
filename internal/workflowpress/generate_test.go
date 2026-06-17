package workflowpress

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// goFormatCheck reports whether src is already gofmt-canonical. It returns an
// error only when src does not parse as Go.
func goFormatCheck(src []byte) (clean bool, err error) {
	formatted, err := format.Source(src)
	if err != nil {
		return false, err
	}
	return bytes.Equal(formatted, src), nil
}

// generate_test.go is the Phase-4 gate: for each ground-truth spec it Generates
// the local workflow and RUNS the kernel Runner over a fixture, asserting the
// expected transitions and the load-bearing behaviors —
//
//   - inbound-lead-dedupe-merge: a duplicate input MERGES (does not double-create);
//   - trial-to-ae-routing: an ICP-fit signup routes to the AE;
//   - renewal-risk-sweep: a >20% usage drop flags the account at-risk.
//
// It also pins generation determinism (same spec -> identical bytes) and
// adapter/runner parity (the durable LocalAdapter behaves exactly like the local
// Runner), the property the next phase's adapter-parity shipcheck builds on.

// runScenario builds a Runner over spec and runs one input, failing on error.
func runScenario(t *testing.T, spec *WorkflowSpec, in RunInput) *RunResult {
	t.Helper()
	r, err := NewRunner(spec, NewHostExecutor(), DefaultGuardEvaluator{})
	if err != nil {
		t.Fatalf("NewRunner(%s): %v", spec.ID, err)
	}
	res, err := r.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run(%s, %s): %v", spec.ID, in.Event, err)
	}
	return res
}

// assertTransitions asserts the recorded transition log matches want exactly.
func assertTransitions(t *testing.T, res *RunResult, want []Transition) {
	t.Helper()
	if len(res.Transitions) != len(want) {
		t.Fatalf("got %d transitions, want %d: %+v", len(res.Transitions), len(want), res.Transitions)
	}
	for i, w := range want {
		got := res.Transitions[i]
		if got.From != w.From || got.To != w.To {
			t.Errorf("transition %d = %s->%s, want %s->%s", i, got.From, got.To, w.From, w.To)
		}
	}
}

// countAction returns how many times the named action was recorded.
func countAction(res *RunResult, name string) int {
	n := 0
	for _, a := range res.Actions {
		if a.Name == name {
			n++
		}
	}
	return n
}

// TestGenerateThenRunEveryScenario is the core gate: for every example spec,
// Generate the tool, then replay every verification scenario through the kernel
// Runner and assert its expected transitions and approval expectation.
func TestGenerateThenRunEveryScenario(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := loadExample(t, name)

			// Generate must succeed and pin the output to the spec.
			gen, err := Generate(spec)
			if err != nil {
				t.Fatalf("Generate(%s): %v", name, err)
			}
			if gen.WorkflowID != spec.ID || gen.Version != spec.Version {
				t.Fatalf("generated id/version = %s/%d, want %s/%d", gen.WorkflowID, gen.Version, spec.ID, spec.Version)
			}
			// The generated runtime, types, state tables, durable adapter, docs,
			// fixtures and test must all be present.
			for _, suffix := range []string{
				"workflow.go", "types.go", "exceptions.go", "state.go",
				"inngest.go", "fixtures.json", "workflow.md", "workflow_test.go",
			} {
				if _, ok := gen.Files[spec.ID+"/"+suffix]; !ok {
					t.Errorf("generated output missing %s", suffix)
				}
			}

			// Run every scenario through the kernel Runner and assert it.
			for _, sc := range spec.VerificationScenarios {
				res := runScenario(t, spec, RunInput{Event: sc.When, Fields: sc.Given})
				assertTransitions(t, res, sc.ExpectTransitions)
				if res.ApprovalRequested != sc.ExpectApproval {
					t.Errorf("scenario %q: approval requested = %v, want %v", sc.Name, res.ApprovalRequested, sc.ExpectApproval)
				}
			}
		})
	}
}

// TestDedupeMergeDoesNotDoubleCreate proves the dedupe-merge idempotency
// invariant: a matching lead MERGES into the existing record and never creates a
// new one, and re-running the identical input does not double-apply.
func TestDedupeMergeDoesNotDoubleCreate(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "inbound-lead-dedupe-merge")
	in := RunInput{
		Event:  "lead_received",
		Fields: map[string]string{"existing_contact": "true", "match_score": "0.96"},
	}

	first := runScenario(t, spec, in)
	assertTransitions(t, first, []Transition{
		{From: "received", To: "matched"},
		{From: "matched", To: "merged"},
	})
	if got := countAction(first, "merge_into_existing"); got != 1 {
		t.Errorf("first run: merge_into_existing called %d times, want 1", got)
	}
	if got := countAction(first, "create_new_record"); got != 0 {
		t.Errorf("first run: a matched lead must NOT create a record, got %d create calls", got)
	}

	// Re-running the same input must produce the identical outcome (idempotent):
	// still exactly one merge, still zero creates — no double-create on redelivery.
	second := runScenario(t, spec, in)
	if got := countAction(second, "merge_into_existing"); got != 1 {
		t.Errorf("re-run: merge_into_existing called %d times, want 1 (idempotent)", got)
	}
	if got := countAction(second, "create_new_record"); got != 0 {
		t.Errorf("re-run: still must NOT create, got %d create calls", got)
	}

	// A genuinely new (unmatched) lead takes the create path, never the merge path.
	newLead := runScenario(t, spec, RunInput{
		Event:  "lead_received",
		Fields: map[string]string{"existing_contact": "false", "match_score": "0.10"},
	})
	assertTransitions(t, newLead, []Transition{
		{From: "received", To: "matched"},
		{From: "matched", To: "created"},
	})
	if got := countAction(newLead, "create_new_record"); got != 1 {
		t.Errorf("new lead: create_new_record called %d times, want 1", got)
	}
	if got := countAction(newLead, "merge_into_existing"); got != 0 {
		t.Errorf("new lead: must NOT merge, got %d merge calls", got)
	}
}

// TestTrialRoutesToAEWhenICPFit proves the trial-routing happy path: an ICP-fit
// signup advances all the way to routed and gates its external write (the deal-
// channel post) for approval.
func TestTrialRoutesToAEWhenICPFit(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "trial-to-ae-routing")

	routed := runScenario(t, spec, RunInput{
		Event:  "trial_signed_up",
		Fields: map[string]string{"company_size": "200", "industry": "saas", "icp_score": "82"},
	})
	assertTransitions(t, routed, []Transition{
		{From: "received", To: "enriched"},
		{From: "enriched", To: "scored"},
		{From: "scored", To: "routed"},
	})
	if routed.FinalState != "routed" {
		t.Errorf("final state = %q, want routed", routed.FinalState)
	}
	if got := countAction(routed, "route_to_ae"); got != 1 {
		t.Errorf("route_to_ae called %d times, want 1", got)
	}
	if got := countAction(routed, "post_to_deal_channel"); got != 1 {
		t.Errorf("post_to_deal_channel called %d times, want 1", got)
	}
	if !routed.ApprovalRequested {
		t.Errorf("an external-write post must request approval")
	}
	// The external write must be recorded as gated and refused by the fail-closed
	// host stub — never performed live.
	var post *ActionCall
	for i := range routed.Actions {
		if routed.Actions[i].Name == "post_to_deal_channel" {
			post = &routed.Actions[i]
		}
	}
	if post == nil {
		t.Fatal("post_to_deal_channel action not recorded")
	}
	if !post.Gated || !post.Refused {
		t.Errorf("external write recorded gated=%v refused=%v, want both true (fail-closed)", post.Gated, post.Refused)
	}

	// A below-threshold score must NOT route.
	notRouted := runScenario(t, spec, RunInput{
		Event:  "scoring_completed",
		Fields: map[string]string{"company_size": "3", "industry": "hobbyist", "icp_score": "20"},
	})
	if len(notRouted.Transitions) != 0 {
		t.Errorf("below-threshold signup must not route, got transitions: %+v", notRouted.Transitions)
	}
}

// TestRenewalFlagsAtRiskWhenUsageDown proves the renewal-risk-sweep flags an
// account at-risk only when usage is down more than 20% AND the renewal is within
// the window, and skips otherwise.
func TestRenewalFlagsAtRiskWhenUsageDown(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "renewal-risk-sweep")

	flagged := runScenario(t, spec, RunInput{
		Event:  "weekly_sweep",
		Fields: map[string]string{"renewal_in_days": "30", "delta_pct": "-0.35"},
	})
	assertTransitions(t, flagged, []Transition{
		{From: "idle", To: "selecting"},
		{From: "selecting", To: "analyzing"},
		{From: "analyzing", To: "flagged"},
	})
	if flagged.FinalState != "flagged" {
		t.Errorf("final state = %q, want flagged", flagged.FinalState)
	}
	if got := countAction(flagged, "create_cs_task"); got != 1 {
		t.Errorf("create_cs_task called %d times, want 1", got)
	}

	// A small usage dip (5%) must NOT flag — the guard requires >20%.
	stable := runScenario(t, spec, RunInput{
		Event:  "usage_analyzed",
		Fields: map[string]string{"renewal_in_days": "30", "delta_pct": "-0.05"},
	})
	if len(stable.Transitions) != 0 {
		t.Errorf("stable usage must not flag, got: %+v", stable.Transitions)
	}

	// A renewal too far out (120 days) must be skipped at the selection guard.
	farOut := runScenario(t, spec, RunInput{
		Event:  "accounts_selected",
		Fields: map[string]string{"renewal_in_days": "120", "delta_pct": "-0.40"},
	})
	if len(farOut.Transitions) != 0 {
		t.Errorf("renewal too far out must be skipped, got: %+v", farOut.Transitions)
	}
}

// TestGenerationIsDeterministic pins the kernel's core promise: the same spec
// produces byte-identical output across calls.
func TestGenerationIsDeterministic(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := loadExample(t, name)
			a, err := Generate(spec)
			if err != nil {
				t.Fatalf("Generate first: %v", err)
			}
			b, err := Generate(spec)
			if err != nil {
				t.Fatalf("Generate second: %v", err)
			}
			if len(a.Files) != len(b.Files) {
				t.Fatalf("file count drift: %d vs %d", len(a.Files), len(b.Files))
			}
			paths := make([]string, 0, len(a.Files))
			for p := range a.Files {
				paths = append(paths, p)
			}
			sort.Strings(paths)
			for _, p := range paths {
				if string(a.Files[p]) != string(b.Files[p]) {
					t.Errorf("%s: non-deterministic bytes", p)
				}
			}
		})
	}
}

// TestGeneratedGoIsGofmtClean asserts every emitted Go file is already canonical
// Go (the generator runs go/format), so a downstream commit of generated code
// does not trip a gofmt gate.
func TestGeneratedGoIsGofmtClean(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		spec := loadExample(t, name)
		gen, err := Generate(spec)
		if err != nil {
			t.Fatalf("Generate(%s): %v", name, err)
		}
		for path, content := range gen.Files {
			if !strings.HasSuffix(path, ".go") {
				continue
			}
			formatted, err := goFormatCheck(content)
			if err != nil {
				t.Errorf("%s: generated Go does not parse: %v", path, err)
				continue
			}
			if !formatted {
				t.Errorf("%s: generated Go is not gofmt-clean", path)
			}
		}
	}
}

// TestGenerateSurvivesBacktickInFreeText is the regression for the raw-string
// literal break: a backtick in any spec free-text field (json.MarshalIndent does
// NOT escape backticks) would terminate the generated `const specJSON = ` + "`" + `...` + "`" + ` early
// and produce un-parseable Go. The fix emits specJSON as a strconv.Quote-escaped
// double-quoted literal. This test injects a backtick (and the adversarial `" + “ + `${}` + “ + `"`
// sequence) into the Goal, then asserts the generated workflow.go still parses and
// the embedded contract decodes back to the exact Goal.
func TestGenerateSurvivesBacktickInFreeText(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "trial-to-ae-routing")
	hostile := "route a `trial` signup " + "`" + `${injection}` + "`" + ` and a "quote"`
	spec.Goal = hostile

	gen, err := Generate(spec)
	if err != nil {
		t.Fatalf("Generate with backtick in Goal: %v", err)
	}
	wf, ok := gen.Files[spec.ID+"/workflow.go"]
	if !ok {
		t.Fatal("generated file map has no workflow.go")
	}
	// The generated Go must still parse — the load-bearing assertion. A raw-string
	// literal broken by the backtick would fail here.
	if _, err := format.Source(wf); err != nil {
		t.Fatalf("generated workflow.go does not parse after backtick injection: %v\n%s", err, wf)
	}
	// And the embedded contract must decode back to the exact Goal: the quoted
	// literal preserves every byte, including the backtick.
	if !bytes.Contains(wf, []byte("const specJSON = ")) {
		t.Fatal("generated workflow.go lost its specJSON const")
	}
	roundTrip := extractAndDecodeSpec(t, wf)
	if roundTrip.Goal != hostile {
		t.Errorf("embedded Goal = %q, want %q (byte-exact round-trip)", roundTrip.Goal, hostile)
	}
}

// extractAndDecodeSpec parses generated workflow.go, evaluates the specJSON string
// literal, and decodes it back into a WorkflowSpec so a test can assert the
// embedded contract survived generation byte-for-byte.
func extractAndDecodeSpec(t *testing.T, src []byte) *WorkflowSpec {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "workflow.go", src, 0)
	if err != nil {
		t.Fatalf("parsing generated workflow.go: %v", err)
	}
	var literal string
	found := false
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != "specJSON" || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					t.Fatalf("specJSON value is not a literal: %T", vs.Values[i])
				}
				unquoted, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquoting specJSON literal: %v", err)
				}
				literal = unquoted
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no specJSON const found in generated workflow.go")
	}
	var spec WorkflowSpec
	if err := json.Unmarshal([]byte(literal), &spec); err != nil {
		t.Fatalf("decoding embedded specJSON: %v", err)
	}
	return &spec
}

// TestLocalAdapterMatchesRunner proves the durable LocalAdapter is behavior-
// equivalent to the local Runner for every scenario — the parity property the
// inngest adapter must uphold and the next phase's shipcheck asserts.
func TestLocalAdapterMatchesRunner(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := loadExample(t, name)
			adapter, err := NewLocalAdapter(spec)
			if err != nil {
				t.Fatalf("NewLocalAdapter(%s): %v", name, err)
			}
			for _, sc := range spec.VerificationScenarios {
				in := RunInput{Event: sc.When, Fields: sc.Given}
				viaRunner := runScenario(t, spec, in)
				viaAdapter, err := adapter.Run(context.Background(), in)
				if err != nil {
					t.Fatalf("adapter.Run(%s): %v", sc.When, err)
				}
				if viaAdapter.FinalState != viaRunner.FinalState {
					t.Errorf("scenario %q: adapter final %q != runner final %q", sc.Name, viaAdapter.FinalState, viaRunner.FinalState)
				}
				if len(viaAdapter.Transitions) != len(viaRunner.Transitions) {
					t.Fatalf("scenario %q: adapter %d transitions != runner %d", sc.Name, len(viaAdapter.Transitions), len(viaRunner.Transitions))
				}
				for i := range viaRunner.Transitions {
					if viaAdapter.Transitions[i] != viaRunner.Transitions[i] {
						t.Errorf("scenario %q transition %d: adapter %+v != runner %+v", sc.Name, i, viaAdapter.Transitions[i], viaRunner.Transitions[i])
					}
				}
				if viaAdapter.ApprovalRequested != viaRunner.ApprovalRequested {
					t.Errorf("scenario %q: adapter approval %v != runner %v", sc.Name, viaAdapter.ApprovalRequested, viaRunner.ApprovalRequested)
				}
			}
		})
	}
}

// TestGeneratorInterfaceMatchesPureGenerate proves the kernel Generator seam
// (NewGenerator) produces the same output as the pure Generate function, and that
// a cancelled context is honoured.
func TestGeneratorInterfaceMatchesPureGenerate(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "trial-to-ae-routing")

	gen := NewGenerator()
	viaSeam, err := gen.Generate(context.Background(), spec)
	if err != nil {
		t.Fatalf("Generator.Generate: %v", err)
	}
	pure, err := Generate(spec)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(viaSeam.Files) != len(pure.Files) {
		t.Fatalf("seam emitted %d files, pure %d", len(viaSeam.Files), len(pure.Files))
	}
	for p, b := range pure.Files {
		if string(viaSeam.Files[p]) != string(b) {
			t.Errorf("%s: seam output differs from pure Generate", p)
		}
	}

	// A cancelled context must short-circuit before emission.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gen.Generate(ctx, spec); err == nil {
		t.Error("expected an error for a cancelled context")
	}
}

// TestAdapterNames pins the adapter backend identifier used in audit.
func TestAdapterNames(t *testing.T) {
	t.Parallel()
	a, err := NewLocalAdapter(loadExample(t, "trial-to-ae-routing"))
	if err != nil {
		t.Fatalf("NewLocalAdapter: %v", err)
	}
	if a.Name() != "local" {
		t.Errorf("adapter name = %q, want local", a.Name())
	}
}

// TestInngestStepPlanMatchesEvents asserts the durable step plan is 1:1 with the
// spec's events, so the durable adapter exercises the same transition set the
// local runner does (the structural basis of adapter parity).
func TestInngestStepPlanMatchesEvents(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		spec := loadExample(t, name)
		steps := PlanInngestSteps(spec)
		if len(steps) != len(spec.Events) {
			t.Fatalf("%s: %d steps, want %d (one per event)", name, len(steps), len(spec.Events))
		}
		for i, ev := range spec.Events {
			s := steps[i]
			if s.Event != ev.Name || s.From != ev.From || s.To != ev.To {
				t.Errorf("%s step %d = %s(%s->%s), want %s(%s->%s)", name, i, s.Event, s.From, s.To, ev.Name, ev.From, ev.To)
			}
			if s.FunctionID != spec.ID+"."+ev.Name {
				t.Errorf("%s step %d function id = %q, want %q", name, i, s.FunctionID, spec.ID+"."+ev.Name)
			}
		}
	}
}
