package workflowpress

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// version_test.go is the regression suite for Phase A wire-format versioning. It
// pins the three load-bearing guarantees:
//
//   - a spec whose schema_version is unknown/newer fails Validate, FAIL-CLOSED;
//   - the strict loader (the function the generated tool's loadSpec delegates to)
//     rejects an extra/renamed field instead of silently zero-valuing a guard or a
//     RequiresApproval flag;
//   - the three ground-truth examples carry the current schema_version and still
//     pass end-to-end.

// TestValidateRejectsUnknownSchemaVersion proves Validate fails CLOSED on any
// schema_version that is not the current supported one — both an older 0 (an
// artifact written before the field existed) and a newer value (a producer ahead
// of this kernel). The rejection wraps ErrUnsupportedSchemaVersion and the
// ErrInvalidSpec umbrella.
func TestValidateRejectsUnknownSchemaVersion(t *testing.T) {
	t.Parallel()

	for _, bad := range []int{0, SchemaVersionWorkflowSpec - 1, SchemaVersionWorkflowSpec + 1, 99} {
		// 0 and -1 can collide once the current version is 1; keep the distinct ones.
		if bad == SchemaVersionWorkflowSpec {
			continue
		}
		bad := bad
		t.Run(strconv.Itoa(bad), func(t *testing.T) {
			t.Parallel()
			spec := loadExample(t, "trial-to-ae-routing")
			spec.SchemaVersion = bad
			err := spec.Validate()
			if err == nil {
				t.Fatalf("Validate accepted schema_version %d, want rejection", bad)
			}
			if !errors.Is(err, ErrUnsupportedSchemaVersion) {
				t.Fatalf("Validate err = %v, want wrapping ErrUnsupportedSchemaVersion", err)
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("Validate err = %v, want wrapping ErrInvalidSpec umbrella", err)
			}
		})
	}
}

// TestValidateAcceptsCurrentSchemaVersion is the positive control: the current
// version validates (the fixtures already carry it).
func TestValidateAcceptsCurrentSchemaVersion(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "trial-to-ae-routing")
	if spec.SchemaVersion != SchemaVersionWorkflowSpec {
		t.Fatalf("fixture schema_version = %d, want %d", spec.SchemaVersion, SchemaVersionWorkflowSpec)
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("Validate rejected current schema_version: %v", err)
	}
}

// TestDecodeSpecStrictRejectsUnknownField is the core "generated tool fails to
// load on a renamed/removed field" regression. The generated loadSpec delegates
// to DecodeSpecStrict, which uses a json.Decoder with DisallowUnknownFields. An
// extra field (e.g. a guard renamed in the wire payload, leaving an orphan key)
// must surface as a loud strict-decode error rather than a lenient json.Unmarshal
// silently zero-valuing the corresponding Go field.
func TestDecodeSpecStrictRejectsUnknownField(t *testing.T) {
	t.Parallel()

	// Start from a valid example, marshal it, then inject an unknown top-level key
	// to simulate a removed/renamed field whose old name lingers in the payload.
	spec := loadExample(t, "trial-to-ae-routing")
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshalling fixture: %v", err)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshalling to generic: %v", err)
	}
	generic["requires_approvall"] = json.RawMessage(`true`) // typo'd/renamed field
	mutated, err := json.Marshal(generic)
	if err != nil {
		t.Fatalf("re-marshalling mutated spec: %v", err)
	}

	// Lenient Unmarshal accepts the unknown key silently — this is exactly the
	// failure the strict loader exists to prevent. Asserting it documents the gap.
	var lenient WorkflowSpec
	if err := json.Unmarshal(mutated, &lenient); err != nil {
		t.Fatalf("lenient Unmarshal unexpectedly failed: %v", err)
	}

	// Strict decode must reject it.
	if _, err := DecodeSpecStrict(mutated); err == nil {
		t.Fatal("DecodeSpecStrict accepted an unknown field; a renamed/removed field would zero-value silently")
	} else if !errors.Is(err, ErrStrictDecode) {
		t.Fatalf("DecodeSpecStrict err = %v, want wrapping ErrStrictDecode", err)
	}
}

// TestDecodeSpecStrictRejectsWrongSchemaVersion proves the strict loader also
// enforces the version gate: a payload whose schema_version is wrong fails to
// load even though every other field is well-formed.
func TestDecodeSpecStrictRejectsWrongSchemaVersion(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "trial-to-ae-routing")
	spec.SchemaVersion = SchemaVersionWorkflowSpec + 1
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshalling fixture: %v", err)
	}
	if _, err := DecodeSpecStrict(raw); err == nil {
		t.Fatal("DecodeSpecStrict accepted a newer schema_version, want rejection")
	} else if !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("DecodeSpecStrict err = %v, want wrapping ErrUnsupportedSchemaVersion", err)
	}
}

// TestDecodeSpecStrictRoundTripsExamples proves the strict loader accepts every
// well-formed ground-truth example and returns a spec that re-validates — the
// path the generated tool exercises at runtime.
func TestDecodeSpecStrictRoundTripsExamples(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := loadExample(t, name)
			raw, err := json.Marshal(spec)
			if err != nil {
				t.Fatalf("marshalling %s: %v", name, err)
			}
			got, err := DecodeSpecStrict(raw)
			if err != nil {
				t.Fatalf("DecodeSpecStrict(%s): %v", name, err)
			}
			if got.ID != name || got.SchemaVersion != SchemaVersionWorkflowSpec {
				t.Fatalf("decoded id/schema_version = %s/%d, want %s/%d", got.ID, got.SchemaVersion, name, SchemaVersionWorkflowSpec)
			}
		})
	}
}

// TestDecodeSpecStrictRejectsTrailingData proves the loader rejects a second
// concatenated document, which a single Decode would otherwise ignore.
func TestDecodeSpecStrictRejectsTrailingData(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "trial-to-ae-routing")
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshalling fixture: %v", err)
	}
	withTrailer := append(append([]byte{}, raw...), []byte(`{"id":"sneaky"}`)...)
	if _, err := DecodeSpecStrict(withTrailer); err == nil {
		t.Fatal("DecodeSpecStrict accepted trailing data, want rejection")
	} else if !errors.Is(err, ErrStrictDecode) {
		t.Fatalf("DecodeSpecStrict err = %v, want wrapping ErrStrictDecode", err)
	}
}

// TestGeneratedLoadSpecUsesStrictDecoder pins that the generated workflow.go wires
// its loadSpec to the kernel's strict loader rather than a lenient json.Unmarshal.
// If a future template edit reintroduces a lenient decode, this fails — the
// strict-decode guarantee is a generated-code property, not just a kernel one.
func TestGeneratedLoadSpecUsesStrictDecoder(t *testing.T) {
	t.Parallel()
	spec := loadExample(t, "trial-to-ae-routing")
	gen, err := Generate(spec)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	wf, ok := gen.Files[spec.ID+"/workflow.go"]
	if !ok {
		t.Fatal("generated file map has no workflow.go")
	}
	src := string(wf)
	if !strings.Contains(src, "wp.DecodeSpecStrict(") {
		t.Error("generated loadSpec does not call wp.DecodeSpecStrict; the strict-decode guarantee is missing")
	}
	if strings.Contains(src, "json.Unmarshal(") {
		t.Error("generated workflow.go uses lenient json.Unmarshal; loadSpec must decode strictly")
	}
}
