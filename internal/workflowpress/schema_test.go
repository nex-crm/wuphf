package workflowpress

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestSpecSchemaMatchesType is the drift guard: the committed WorkflowSpec JSON
// Schema must equal what jsonschema.For infers from the Go type today, stamped
// with the published $schema dialect and $id. If the IR changes without
// regenerating the schema — or the stamped identity drifts — this fails, keeping
// the published contract, its version stamp, and the kernel type in lockstep.
func TestSpecSchemaMatchesType(t *testing.T) {
	t.Parallel()
	assertSchemaMatchesType[WorkflowSpec](t, SpecSchemaBytes, specSchemaID)
}

func TestResearchSchemaMatchesType(t *testing.T) {
	t.Parallel()
	assertSchemaMatchesType[WorkflowResearch](t, ResearchSchemaBytes, researchSchemaID)
}

func assertSchemaMatchesType[T any](t *testing.T, committed func() ([]byte, error), id string) {
	t.Helper()
	// InferStampedSchema is the SAME generator the committed files are regenerated
	// from: infer the Go type, stamp the published $schema/$id, and run the
	// deterministic enum-injection pass. Driving the guard through it keeps the
	// published contract, its version stamp, and the enum allowed-set in lockstep —
	// the committed file cannot drift from the type or from the enum table.
	want, err := InferStampedSchema[T](id)
	if err != nil {
		t.Fatalf("inferring stamped schema: %v", err)
	}

	got, err := committed()
	if err != nil {
		t.Fatalf("reading committed schema: %v", err)
	}
	if !bytes.Equal(bytes.TrimRight(got, "\n"), bytes.TrimRight(want, "\n")) {
		t.Fatalf("committed schema drifted from Go type.\nRegenerate the schema in testdata/schema.\n--- committed ---\n%s\n--- inferred ---\n%s", got, want)
	}
}

// TestSpecRoundTripsThroughSchema marshals each ground-truth fixture, validates
// the JSON against the committed schema, decodes it back into a WorkflowSpec, and
// confirms the re-marshalled bytes are stable. This proves Go <-> JSON round-trips
// and that the fixtures validate against the schema.
func TestSpecRoundTripsThroughSchema(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := loadExample(t, name)

			canonical, err := roundTripValidate(spec, ValidateSpecJSON)
			if err != nil {
				t.Fatalf("round-trip/schema validation failed: %v", err)
			}

			var back WorkflowSpec
			if err := json.Unmarshal(canonical, &back); err != nil {
				t.Fatalf("decoding canonical JSON back into WorkflowSpec: %v", err)
			}
			again, err := json.Marshal(&back)
			if err != nil {
				t.Fatalf("re-marshalling decoded spec: %v", err)
			}
			if !bytes.Equal(canonical, again) {
				t.Fatalf("round-trip not stable.\nfirst:  %s\nsecond: %s", canonical, again)
			}
		})
	}
}

// TestResearchRoundTripsThroughSchema does the same Go <-> JSON <-> schema
// round-trip for a representative WorkflowResearch value.
func TestResearchRoundTripsThroughSchema(t *testing.T) {
	t.Parallel()
	research := WorkflowResearch{
		SchemaVersion:  SchemaVersionWorkflowResearch,
		WorkflowID:     "trial-to-ae-routing",
		SessionContext: "operator routing a trial signup by hand across CRM + Slack",
		OperatorNotes:  []string{"always posts the routing decision to #deals"},
		SampleRecords: []SampleRecord{
			{Entity: "TrialSignup", Fields: map[string]string{"email": "sam@acme.test"}, Source: "webhook"},
		},
		ObservedExceptions: []ObservedException{
			{Description: "enrichment provider missed the domain", Frequency: 3, HandledAs: "manual review"},
		},
		OperatorEdits: []OperatorEdit{
			{Path: "actions[route_to_ae].target", Before: "crm-v1", After: "crm-v2", Reason: "migration"},
		},
		ToolTraces: []ToolTrace{
			{Tool: "browser", Action: "click", Request: "select AE", Result: "ok"},
		},
	}
	if _, err := roundTripValidate(research, ValidateResearchJSON); err != nil {
		t.Fatalf("research round-trip/schema validation failed: %v", err)
	}
}

// TestSchemaBytesAreCopies confirms the exported accessors return copies, so a
// caller mutating the slice cannot corrupt the embedded schema for the next
// caller.
func TestSchemaBytesAreCopies(t *testing.T) {
	t.Parallel()
	a, err := SpecSchemaBytes()
	if err != nil {
		t.Fatalf("SpecSchemaBytes: %v", err)
	}
	if len(a) == 0 {
		t.Fatal("SpecSchemaBytes returned empty")
	}
	a[0] = 'X'
	b, err := SpecSchemaBytes()
	if err != nil {
		t.Fatalf("SpecSchemaBytes: %v", err)
	}
	if b[0] == 'X' {
		t.Fatal("mutating returned bytes corrupted the embedded schema")
	}
}
