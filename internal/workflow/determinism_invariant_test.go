package workflow

import (
	"reflect"
	"testing"
)

// determinism_invariant_test.go locks the invariant the large-I/O framework
// depends on (RFC §7, D4): the real integration/LLM/reduction work runs ONLY in
// the broker's live ActionExec. shipcheck/replay uses the pure, inert
// recordingExec, so a spec's Params / Platform / ActionID can never make a proof
// reach a provider or perturb its result. If a future change breaks this, these
// tests fail.

// recordingExec must be pure and inert: OK with no Output, regardless of the
// action (even one carrying a provider target + params) or the data.
func TestRecordingExecIsPureAndInert(t *testing.T) {
	a := Action{
		ID:       "fetch",
		Kind:     ActionDeterministic,
		Platform: "gmail",
		ActionID: "GMAIL_FETCH_EMAILS",
		Params:   map[string]any{"query": "is:unread", "max_results": 25},
	}
	out := recordingExec(a, map[string]any{"prior": "data"})
	if !out.OK {
		t.Fatal("recordingExec must report OK")
	}
	if out.Output != nil {
		t.Fatalf("recordingExec must produce no Output (it is a pure recorder), got %v", out.Output)
	}
	if out.Err != "" {
		t.Fatalf("recordingExec must not error, got %q", out.Err)
	}
}

// integrationReadSpec is a minimal valid contract with a deterministic
// integration-read action (Platform+ActionID+Params), its allow-list entry, and
// a replay scenario — the exact shape S3 introduces.
func integrationReadSpec() *Spec {
	return &Spec{
		ID:      "wf-int",
		Initial: "start",
		States:  []State{{ID: "start"}, {ID: "done"}},
		Events:  []Event{{ID: "run"}},
		Transitions: []Transition{
			{From: "start", To: "done", On: "run", Actions: []string{"fetch"}},
		},
		Actions: []Action{
			{ID: "fetch", Kind: ActionDeterministic, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS",
				Params:     map[string]any{"query": "is:unread", "max_results": 25, "verbose": false},
				ResultPath: "data.messages", Expose: []string{"sender", "subject"}},
		},
		AllowedReads: []ActionRef{{Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS"}},
		Scenarios: []Scenario{
			{Name: "happy_path", Events: []ScenarioEvent{{Event: "run", DedupKey: "s1"}},
				ExpectStates: []string{"start", "done"}, ExpectActions: []string{"fetch"}},
		},
	}
}

// Shipcheck must be byte-identical with and without Params/ResultPath/Expose —
// proving those fields are invisible to the proof (they only matter in the live
// exec). This also guards adapter-parity from silently regressing on Params.
func TestShipcheckIgnoresParams(t *testing.T) {
	withParams := integrationReadSpec()
	if err := withParams.Validate(); err != nil {
		t.Fatalf("integration-read spec should validate: %v", err)
	}
	rWith := Shipcheck(withParams)

	// Strip the live-exec-only fields; the proof must not change.
	bare := integrationReadSpec()
	bare.Actions[0].Params = nil
	bare.Actions[0].ResultPath = ""
	bare.Actions[0].Expose = nil
	rBare := Shipcheck(bare)

	if rWith.Passed != rBare.Passed {
		t.Fatalf("Params perturbed the proof: withParams.Passed=%v bare.Passed=%v", rWith.Passed, rBare.Passed)
	}
	if !rWith.Passed {
		t.Fatalf("integration-read spec should shipcheck: %+v", rWith.Checks)
	}
	if !reflect.DeepEqual(rWith.Checks, rBare.Checks) {
		t.Fatalf("Params changed the shipcheck checks:\nwith=%+v\nbare=%+v", rWith.Checks, rBare.Checks)
	}
}

// An integration read NOT on the allow-list must fail Validate (so it can never
// freeze), independent of execution. (D6 at the contract layer.)
func TestValidateRejectsUnlistedIntegrationRead(t *testing.T) {
	s := integrationReadSpec()
	s.AllowedReads = nil // drop the allow-list entry
	if err := s.Validate(); err == nil {
		t.Fatal("an integration read without an allow-list entry must fail validation")
	}
}
