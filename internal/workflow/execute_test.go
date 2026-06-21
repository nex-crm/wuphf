package workflow

import (
	"path/filepath"
	"testing"
)

func TestExecuteAndPersistRun(t *testing.T) {
	s := mustLoad(t)
	sc := scenarioByName(t, s, "happy_path")

	rec := Execute(s, "manual", sc.Events, nil)
	if rec.SpecID != s.ID || rec.Trigger != "manual" {
		t.Fatalf("record metadata wrong: %+v", rec)
	}
	if rec.Result.FinalState != "referred" {
		t.Fatalf("run final state %q, want referred", rec.Result.FinalState)
	}

	path := filepath.Join(t.TempDir(), "x.runs.jsonl")
	rec.At = "2026-06-17T00:00:00Z"
	if err := AppendRun(path, rec); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := AppendRun(path, rec); err != nil {
		t.Fatalf("append again: %v", err)
	}

	runs, err := ReadRuns(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(runs))
	}
	if runs[0].Result.FinalState != "referred" || len(runs[0].Result.Audit) != len(sc.Events) {
		t.Fatalf("persisted run lost detail: %+v", runs[0].Result)
	}
}

func TestExecuteRecordsExceptionInAudit(t *testing.T) {
	s := mustLoad(t)
	// Missing-owner guard fails -> the run records a guard_failed skip.
	rec := Execute(s, "manual", scenarioByName(t, s, "missing_owner_guard").Events, nil)
	if rec.Result.FinalState != "identified" {
		t.Fatalf("guard should block: %q", rec.Result.FinalState)
	}
	if len(rec.Result.Audit) != 1 || rec.Result.Audit[0].Skipped != "guard_failed" {
		t.Fatalf("audit should record the exception: %+v", rec.Result.Audit)
	}
}

func TestReadRunsMissingIsEmpty(t *testing.T) {
	got, err := ReadRuns(filepath.Join(t.TempDir(), "none.jsonl"))
	if err != nil || len(got) != 0 {
		t.Fatalf("want empty no-error, got %v err %v", got, err)
	}
}

// TestRunThreadsOutputAcrossTransitions is the regression for the chain
// data-flow bug: in a linear contract each step is its own transition, and the
// runner used to reset the data map per transition — so a summarize step on
// transition 2 never saw the emails a fetch produced on transition 1. Outputs
// must accumulate across the whole run.
func TestRunThreadsOutputAcrossTransitions(t *testing.T) {
	s := &Spec{
		Initial:  "start",
		Terminal: []string{"done"},
		States:   []State{{ID: "start"}, {ID: "s1"}, {ID: "done"}},
		Events:   []Event{{ID: "run"}, {ID: "next"}},
		Actions:  []Action{{ID: "fetch"}, {ID: "summarize"}},
		Transitions: []Transition{
			{From: "start", To: "s1", On: "run", Actions: []string{"fetch"}},
			{From: "s1", To: "done", On: "next", Actions: []string{"summarize"}},
		},
	}
	var sawBySummarize map[string]any
	exec := func(a Action, data map[string]any) ActionOutcome {
		switch a.ID {
		case "fetch":
			return ActionOutcome{OK: true, Output: map[string]any{"fetched": "10 emails"}}
		case "summarize":
			sawBySummarize = map[string]any{}
			for k, v := range data {
				sawBySummarize[k] = v
			}
			return ActionOutcome{OK: true, Output: map[string]any{"summary": "ok"}}
		}
		return ActionOutcome{OK: true}
	}

	res := Run(s, []ScenarioEvent{{Event: "run"}, {Event: "next"}}, exec)
	if res.FinalState != "done" {
		t.Fatalf("run must complete to done, got %q (audit %+v)", res.FinalState, res.Audit)
	}
	if sawBySummarize["fetched"] != "10 emails" {
		t.Fatalf("summarize (transition 2) must see fetch's output from transition 1, saw %+v", sawBySummarize)
	}
}
