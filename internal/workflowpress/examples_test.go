package workflowpress

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// exampleNames are the three ground-truth RevOps workflows every phase targets.
var exampleNames = []string{
	"trial-to-ae-routing",
	"renewal-risk-sweep",
	"inbound-lead-dedupe-merge",
}

// loadExample reads and decodes a ground-truth fixture into a WorkflowSpec.
func loadExample(t *testing.T, name string) *WorkflowSpec {
	t.Helper()
	path := filepath.Join("testdata", "examples", name+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %q: %v", path, err)
	}
	var spec WorkflowSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decoding fixture %q: %v", path, err)
	}
	return &spec
}

// TestExamplesLoadAndValidate proves all three ground-truth fixtures are valid
// WorkflowSpec instances: they pass both the semantic state-machine validator and
// the JSON Schema.
func TestExamplesLoadAndValidate(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := loadExample(t, name)
			if spec.ID != name {
				t.Errorf("fixture id = %q, want %q (filename must match spec id)", spec.ID, name)
			}
			// Every committed fixture must carry the current wire-format version, so a
			// fixture written before the field existed (schema_version 0) is caught here
			// rather than silently failing the fail-closed Validate gate for the wrong
			// reason.
			if spec.SchemaVersion != SchemaVersionWorkflowSpec {
				t.Errorf("fixture schema_version = %d, want %d", spec.SchemaVersion, SchemaVersionWorkflowSpec)
			}
			if err := spec.Validate(); err != nil {
				t.Fatalf("semantic Validate() failed: %v", err)
			}
			if _, err := roundTripValidate(spec, ValidateSpecJSON); err != nil {
				t.Fatalf("JSON Schema validation failed: %v", err)
			}
		})
	}
}

// TestExamplesCoverGroundTruthShapes asserts the corpus as a whole exercises the
// three load-bearing shapes the grader scores against:
//
//   - at least one scheduled trigger (renewal-risk-sweep is weekly);
//   - at least one idempotent action (the dedupe-merge);
//   - at least one external-write action requiring approval.
func TestExamplesCoverGroundTruthShapes(t *testing.T) {
	t.Parallel()

	var (
		scheduledTriggers      int
		idempotentActions      int
		externalWriteApprovals int
	)

	for _, name := range exampleNames {
		spec := loadExample(t, name)
		for _, ev := range spec.Events {
			if ev.Trigger == TriggerScheduled {
				scheduledTriggers++
				if ev.Schedule == "" {
					t.Errorf("%s: scheduled event %q has no schedule", name, ev.Name)
				}
			}
		}
		for _, a := range spec.Actions {
			if a.Idempotent {
				idempotentActions++
			}
			if a.Kind == ActionExternalWrite && a.RequiresApproval {
				externalWriteApprovals++
			}
		}
	}

	if scheduledTriggers < 1 {
		t.Errorf("ground-truth corpus has %d scheduled triggers, want >= 1", scheduledTriggers)
	}
	if idempotentActions < 1 {
		t.Errorf("ground-truth corpus has %d idempotent actions, want >= 1", idempotentActions)
	}
	if externalWriteApprovals < 1 {
		t.Errorf("ground-truth corpus has %d external-write actions requiring approval, want >= 1", externalWriteApprovals)
	}
}

// TestEachExampleHasInitialAndTerminalState is a state-machine soundness check on
// the fixtures: each must have exactly one initial state and at least one
// terminal state, so a generated runner has a defined entry and exit.
func TestEachExampleHasInitialAndTerminalState(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := loadExample(t, name)
			var initial, terminal int
			for _, st := range spec.States {
				if st.Initial {
					initial++
				}
				if st.Terminal {
					terminal++
				}
			}
			if initial != 1 {
				t.Errorf("%s: %d initial states, want exactly 1", name, initial)
			}
			if terminal < 1 {
				t.Errorf("%s: %d terminal states, want >= 1", name, terminal)
			}
		})
	}
}
