package team

// task_dod_derive_test.go — deterministic DoD→verification derivation
// (done-integrity fix family; ICP-eval v2 [01:22]: Sam's verbatim DoD was
// never encoded as machine verification). The pattern set is conservative:
// false negatives are fine, false positives are not — the negative cases
// here are as load-bearing as the positives.

import (
	"strings"
	"testing"
)

func TestDeriveDoDVerification_FromDetails(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		details  string
		wantSpec string // empty = must NOT derive
	}{
		{
			name:     "file exists with DoD cue",
			details:  "Build the report. Definition of done: a file out/report.md exists.",
			wantSpec: "test -f 'out/report.md'",
		},
		{
			name:     "sam verbatim dont-tell-me phrasing",
			details:  "Build a landing page. Don't tell me it's done unless a file landing/index.html exists.",
			wantSpec: "test -f 'landing/index.html'",
		},
		{
			name:     "file exists and contains quoted text",
			details:  "DoD: the file landing/index.html exists and contains \"email capture form\".",
			wantSpec: "test -f 'landing/index.html' && grep -qF -e 'email capture form' 'landing/index.html'",
		},
		{
			name:     "unquoted contains falls back to exists-only",
			details:  "Definition of done: a file landing/index.html exists and contains the form.",
			wantSpec: "test -f 'landing/index.html'",
		},
		{
			name:     "backtick command after DoD cue",
			details:  "Definition of done: `go test ./internal/team` passes clean.",
			wantSpec: "go test ./internal/team",
		},
		{
			name:     "backticked file path is not adopted as a command",
			details:  "Definition of done: a file `out/report.md` exists.",
			wantSpec: "test -f 'out/report.md'",
		},
		{
			name:    "file phrasing without a DoD cue never fires from details",
			details: "Note that a file config/app.yaml exists already, reuse it.",
		},
		{
			name:    "DoD cue without a checkable pattern never fires",
			details: "Definition of done: the customer is happy and the launch goes well.",
		},
		{
			name:    "backtick path without spaces after cue never fires",
			details: "DoD: ship `landing/index.html` to the team.",
		},
		{
			name:    "path with spaces or quotes never matches",
			details: "Definition of done: a file out/'rm -rf'.md exists.",
		},
		{
			name:    "empty",
			details: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := deriveTaskVerificationFromDetails(tc.details)
			if tc.wantSpec == "" {
				if got != nil {
					t.Fatalf("must not derive (false positive); got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected derivation, got nil")
			}
			if got.Kind != taskVerificationKindCommand || !got.Required {
				t.Errorf("derived check must be a required command; got %+v", got)
			}
			if got.Spec != tc.wantSpec {
				t.Errorf("spec = %q, want %q", got.Spec, tc.wantSpec)
			}
		})
	}
}

func TestDeriveDoDVerification_FromDefinitionCriteria(t *testing.T) {
	t.Parallel()
	// Success criteria are check statements by construction: the file
	// pattern fires without a cue, backtick commands never fire here.
	def := &TaskDefinition{
		Goal: "Ship the weekly report",
		SuccessCriteria: []string{
			"Draft approved by the human",
			"a file out/report.md exists",
		},
	}
	got := deriveTaskVerificationFromDefinition(def)
	if got == nil || got.Spec != "test -f 'out/report.md'" || !got.Required {
		t.Fatalf("criteria file pattern must derive a required exists check; got %+v", got)
	}
	if v := deriveTaskVerificationFromDefinition(&TaskDefinition{
		Goal:            "Ship it",
		SuccessCriteria: []string{"`rm -rf /` passes", "the human signs off"},
	}); v != nil {
		t.Fatalf("backtick commands in criteria must never derive (no cue); got %+v", v)
	}
	if v := deriveTaskVerificationFromDefinition(nil); v != nil {
		t.Fatalf("nil definition must derive nothing; got %+v", v)
	}
}

// TestDoDVerification_AutoSetAtCreateAndDefine drives the broker paths: an
// explicit DoD in the create details (and in define success criteria) lands
// as a required Verification with the auto-derive action-log stamp; an
// explicitly passed verification is never overridden.
func TestDoDVerification_AutoSetAtCreateAndDefine(t *testing.T) {
	t.Parallel()
	b := newHumanNoteTestBroker(t)

	created, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Build the launch report",
		Details:   "Definition of done: a file out/report.md exists. Don't tell me it's done unless that check passes.",
		Owner:     "eng",
		CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	task := b.TaskByID(created.Task.ID)
	if task == nil || task.Verification == nil {
		t.Fatalf("DoD details must auto-set Verification; got %+v", task)
	}
	if task.Verification.Kind != taskVerificationKindCommand || !task.Verification.Required ||
		!strings.Contains(task.Verification.Spec, "out/report.md") {
		t.Errorf("derived verification wrong: %+v", task.Verification)
	}
	stampFound := false
	for _, a := range b.Actions() {
		if a.Kind == "verification_derived" && a.RelatedID == created.Task.ID &&
			strings.Contains(a.Summary, "verification auto-derived from DoD") {
			stampFound = true
		}
	}
	if !stampFound {
		t.Errorf("create must stamp the verification auto-derive action-log line")
	}

	// Explicit verification on create wins — the deriver never overrides.
	explicit, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Build the second report",
		Details:          "Definition of done: a file out/second.md exists.",
		Owner:            "eng",
		CreatedBy:        "ceo",
		VerificationKind: "command", VerificationSpec: "exit 0", VerificationRequired: true,
	})
	if err != nil {
		t.Fatalf("create explicit: %v", err)
	}
	if v := b.TaskByID(explicit.Task.ID).Verification; v == nil || v.Spec != "exit 0" {
		t.Errorf("explicit verification must win over derivation; got %+v", v)
	}

	// Define path: a machine-checkable success criterion without an explicit
	// verification derives the check and stamps the log.
	plain, err := b.MutateTask(TaskPostRequest{
		Action: "create", Channel: "general", Title: "Assemble the summary",
		Details: "Assemble the weekly summary.", Owner: "eng", CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("create plain: %v", err)
	}
	if v := b.TaskByID(plain.Task.ID).Verification; v != nil {
		t.Fatalf("plain details must not derive a check; got %+v", v)
	}
	if _, err := b.MutateTask(TaskPostRequest{
		Action: "define", ID: plain.Task.ID, Channel: "general", CreatedBy: "ceo",
		Definition: &TaskDefinition{
			Goal:            "Ship the weekly summary",
			SuccessCriteria: []string{"a file out/summary.md exists"},
		},
	}); err != nil {
		t.Fatalf("define: %v", err)
	}
	defined := b.TaskByID(plain.Task.ID)
	if defined.Verification == nil || !strings.Contains(defined.Verification.Spec, "out/summary.md") || !defined.Verification.Required {
		t.Errorf("define must derive the criterion check; got %+v", defined.Verification)
	}
}
