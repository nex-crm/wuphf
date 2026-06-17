package workflowpress

import (
	"errors"
	"testing"
)

// minimalValidSpec returns the smallest spec that passes Validate, so each
// negative case can mutate exactly one field and prove that field is enforced.
func minimalValidSpec() *WorkflowSpec {
	prov := Provenance{TrustTier: TrustOperatorStated, Confidence: 1.0}
	return &WorkflowSpec{
		SchemaVersion: SchemaVersionWorkflowSpec,
		ID:            "wf",
		Version:       1,
		Goal:          "do the thing",
		Operator:      "revops",
		States: []State{
			{Name: "start", Initial: true, Provenance: prov},
			{Name: "done", Terminal: true, Provenance: prov},
		},
		Events: []Event{
			{Name: "go", Trigger: TriggerExternal, From: "start", To: "done", Provenance: prov},
		},
		Actions: []Action{
			{Name: "read_it", Kind: ActionRead, On: "go", RequiresApproval: false, Provenance: prov},
		},
		VerificationScenarios: []VerificationScenario{
			{Name: "happy", When: "go", ExpectTransitions: []Transition{{From: "start", To: "done"}}},
		},
	}
}

func TestWorkflowSpecValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(s *WorkflowSpec)
		wantErr error // nil means "must validate"
	}{
		{
			name:    "minimal valid spec",
			mutate:  func(*WorkflowSpec) {},
			wantErr: nil,
		},
		{
			name:    "empty id",
			mutate:  func(s *WorkflowSpec) { s.ID = "" },
			wantErr: ErrEmptyField,
		},
		{
			name:    "empty goal",
			mutate:  func(s *WorkflowSpec) { s.Goal = "" },
			wantErr: ErrEmptyField,
		},
		{
			name:    "empty operator",
			mutate:  func(s *WorkflowSpec) { s.Operator = "" },
			wantErr: ErrEmptyField,
		},
		{
			name:    "no states",
			mutate:  func(s *WorkflowSpec) { s.States = nil },
			wantErr: ErrEmptyField,
		},
		{
			// Regression: Validate is the complete freeze gate, so it must require
			// exactly one initial state. A spec with zero initial states has no entry
			// point and must be rejected here, not only at NewRunner.
			name: "zero initial states",
			mutate: func(s *WorkflowSpec) {
				for i := range s.States {
					s.States[i].Initial = false
				}
			},
			wantErr: ErrEmptyField,
		},
		{
			// Two initial states are ambiguous: a sound machine has a single entry.
			name: "two initial states",
			mutate: func(s *WorkflowSpec) {
				for i := range s.States {
					s.States[i].Initial = true
				}
			},
			wantErr: ErrEmptyField,
		},
		{
			// At least one terminal state is required so a run has a defined exit.
			name: "no terminal state",
			mutate: func(s *WorkflowSpec) {
				for i := range s.States {
					s.States[i].Terminal = false
				}
			},
			wantErr: ErrEmptyField,
		},
		{
			name:    "event from-state undefined",
			mutate:  func(s *WorkflowSpec) { s.Events[0].From = "ghost" },
			wantErr: ErrUndefinedState,
		},
		{
			name:    "event to-state undefined",
			mutate:  func(s *WorkflowSpec) { s.Events[0].To = "ghost" },
			wantErr: ErrUndefinedState,
		},
		{
			name:    "action missing trust tier",
			mutate:  func(s *WorkflowSpec) { s.Actions[0].Provenance.TrustTier = "" },
			wantErr: ErrMissingProvenance,
		},
		{
			name:    "action confidence above one",
			mutate:  func(s *WorkflowSpec) { s.Actions[0].Provenance.Confidence = 1.5 },
			wantErr: ErrMissingProvenance,
		},
		{
			name:    "state missing provenance",
			mutate:  func(s *WorkflowSpec) { s.States[0].Provenance.TrustTier = "bogus" },
			wantErr: ErrMissingProvenance,
		},
		{
			name: "inferred external-write without approval",
			mutate: func(s *WorkflowSpec) {
				s.Actions[0] = Action{
					Name:             "post_it",
					Kind:             ActionExternalWrite,
					On:               "go",
					RequiresApproval: false,
					Provenance:       Provenance{TrustTier: TrustInferred, Confidence: 0.5},
				}
			},
			wantErr: ErrWriteNeedsApproval,
		},
		{
			name: "observed internal-write without approval",
			mutate: func(s *WorkflowSpec) {
				s.Actions[0] = Action{
					Name:             "write_it",
					Kind:             ActionInternalWrite,
					On:               "go",
					RequiresApproval: false,
					Provenance:       Provenance{TrustTier: TrustObserved, Confidence: 0.8},
				}
			},
			wantErr: ErrWriteNeedsApproval,
		},
		{
			name: "operator-stated write may skip approval",
			mutate: func(s *WorkflowSpec) {
				s.Actions[0] = Action{
					Name:             "write_it",
					Kind:             ActionInternalWrite,
					On:               "go",
					RequiresApproval: false,
					Provenance:       Provenance{TrustTier: TrustOperatorStated, Confidence: 1.0},
				}
			},
			wantErr: nil,
		},
		{
			// Regression: an unknown action kind must be rejected. IsWrite fails
			// open (treats an unrecognised kind as a read), so a write smuggled in
			// under an unknown kind with RequiresApproval=false would otherwise pass
			// Validate and bypass the approval gate entirely.
			name: "unknown action kind bypassing write approval",
			mutate: func(s *WorkflowSpec) {
				s.Actions[0] = Action{
					Name:             "send_blast",
					Kind:             ActionKind("send-email-blast"),
					On:               "go",
					RequiresApproval: false,
					Provenance:       Provenance{TrustTier: TrustInferred, Confidence: 0.9},
				}
			},
			wantErr: ErrInvalidEnum,
		},
		{
			name:    "unknown event trigger",
			mutate:  func(s *WorkflowSpec) { s.Events[0].Trigger = EventTrigger("on-full-moon") },
			wantErr: ErrInvalidEnum,
		},
		{
			name:    "scenario references unknown event",
			mutate:  func(s *WorkflowSpec) { s.VerificationScenarios[0].When = "nope" },
			wantErr: ErrBadScenarioRef,
		},
		{
			name: "scenario transition references unknown state",
			mutate: func(s *WorkflowSpec) {
				s.VerificationScenarios[0].ExpectTransitions = []Transition{{From: "start", To: "ghost"}}
			},
			wantErr: ErrBadScenarioRef,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := minimalValidSpec()
			tc.mutate(s)
			err := s.Validate()
			switch {
			case tc.wantErr == nil:
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
			default:
				if err == nil {
					t.Fatalf("Validate() = nil, want error wrapping %v", tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Validate() = %v, want wrapping %v", err, tc.wantErr)
				}
				if !errors.Is(err, ErrInvalidSpec) {
					t.Fatalf("Validate() = %v, want wrapping ErrInvalidSpec umbrella", err)
				}
			}
		})
	}
}

// TestValidateRejectsUnsafeID is the regression for the path-traversal risk: the
// spec ID is used verbatim as a path component when the Generator builds its
// file-map keys, so an ID with slashes, dots, or traversal sequences must be
// rejected. The three example IDs (and other plain slugs) must still pass.
func TestValidateRejectsUnsafeID(t *testing.T) {
	t.Parallel()

	unsafe := []string{
		"../../etc",
		"a/b",
		"a.b",
		"..",
		"/abs",
		"trailing/",
		"UPPER",           // uppercase not allowed by the slug
		"-leading-hyphen", // must start with an alphanumeric
		"",                // empty (caught as empty field, still an error)
		"with space",
	}
	for _, id := range unsafe {
		id := id
		t.Run("reject_"+id, func(t *testing.T) {
			t.Parallel()
			s := minimalValidSpec()
			s.ID = id
			if err := s.Validate(); err == nil {
				t.Fatalf("Validate() accepted unsafe id %q", id)
			} else if !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("Validate() err = %v, want wrapping ErrInvalidSpec", err)
			}
		})
	}

	safe := []string{"trial-to-ae-routing", "renewal-risk-sweep", "inbound-lead-dedupe-merge", "wf", "a", "x1"}
	for _, id := range safe {
		id := id
		t.Run("accept_"+id, func(t *testing.T) {
			t.Parallel()
			s := minimalValidSpec()
			s.ID = id
			if err := s.Validate(); err != nil {
				t.Fatalf("Validate() rejected safe id %q: %v", id, err)
			}
		})
	}
}

func TestValidateNilSpec(t *testing.T) {
	t.Parallel()
	var s *WorkflowSpec
	if err := s.Validate(); !errors.Is(err, ErrEmptyField) {
		t.Fatalf("nil spec Validate() = %v, want ErrEmptyField", err)
	}
}

func TestTrustTierValid(t *testing.T) {
	t.Parallel()
	valid := []TrustTier{TrustObserved, TrustOperatorStated, TrustInferred}
	for _, tt := range valid {
		if !tt.Valid() {
			t.Errorf("%q.Valid() = false, want true", tt)
		}
	}
	if TrustTier("nonsense").Valid() {
		t.Errorf("nonsense trust tier reported valid")
	}
}

func TestActionKindIsWrite(t *testing.T) {
	t.Parallel()
	if ActionRead.IsWrite() {
		t.Errorf("read classified as write")
	}
	if !ActionInternalWrite.IsWrite() || !ActionExternalWrite.IsWrite() {
		t.Errorf("write kinds not classified as write")
	}
}

func TestActionKindValid(t *testing.T) {
	t.Parallel()
	valid := []ActionKind{ActionRead, ActionInternalWrite, ActionExternalWrite}
	for _, k := range valid {
		if !k.Valid() {
			t.Errorf("%q.Valid() = false, want true", k)
		}
	}
	if ActionKind("send-email-blast").Valid() {
		t.Errorf("unknown action kind reported valid")
	}
	if ActionKind("").Valid() {
		t.Errorf("empty action kind reported valid")
	}
}

func TestEventTriggerValid(t *testing.T) {
	t.Parallel()
	valid := []EventTrigger{TriggerExternal, TriggerScheduled, TriggerInternal}
	for _, tr := range valid {
		if !tr.Valid() {
			t.Errorf("%q.Valid() = false, want true", tr)
		}
	}
	if EventTrigger("on-full-moon").Valid() {
		t.Errorf("unknown event trigger reported valid")
	}
	if EventTrigger("").Valid() {
		t.Errorf("empty event trigger reported valid")
	}
}
