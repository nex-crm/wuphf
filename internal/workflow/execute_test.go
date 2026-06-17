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
