package workflowpress

import (
	"encoding/json"
	"strings"
	"testing"
)

// schema_enums_test.go is the regression suite for the JSON-Schema ENUM gap
// (Phase B). It proves the PUBLISHED schema — the contract a non-Go, cross-language
// consumer validates against on its own — now rejects a spec whose ActionKind or
// TrustTier is outside the allowed set, the way the in-kernel Go Valid() gate
// already does. Before the enum-injection pass these specs validated against the
// bare {"type":"string"} schema, silently bypassing the security rule the enum
// encodes (an unknown action kind fails open past the write-approval rule; an
// unknown trust tier escapes the inferred/observed write-approval degrade).

// decodeGeneric marshals a Go value and decodes it back into the generic any-shape
// the JSON-Schema validator works over. This mirrors how a cross-language consumer
// sees the wire bytes — it validates the JSON, not the Go type.
func decodeGeneric(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshalling to generic: %v", err)
	}
	return out
}

// TestSpecSchemaRejectsOutOfEnum is the cross-language-style check: a spec whose
// kind or trust_tier is outside the enum must fail ValidateSpecJSON (the published
// schema), while a structurally identical spec with in-enum values passes.
func TestSpecSchemaRejectsOutOfEnum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(s *WorkflowSpec)
		wantErr bool
	}{
		{
			name:    "in-enum spec passes",
			mutate:  func(*WorkflowSpec) {},
			wantErr: false,
		},
		{
			name: "action kind outside enum is rejected",
			// "write" is not one of read/internal-write/external-write. A bare-string
			// schema would accept it; the enum must not.
			mutate:  func(s *WorkflowSpec) { s.Actions[0].Kind = ActionKind("write") },
			wantErr: true,
		},
		{
			name:    "empty action kind is rejected",
			mutate:  func(s *WorkflowSpec) { s.Actions[0].Kind = ActionKind("") },
			wantErr: true,
		},
		{
			name: "trust_tier outside enum is rejected",
			// "trusted" is not one of observed/operator-stated/inferred.
			mutate:  func(s *WorkflowSpec) { s.Actions[0].Provenance.TrustTier = TrustTier("trusted") },
			wantErr: true,
		},
		{
			name:    "event trigger outside enum is rejected",
			mutate:  func(s *WorkflowSpec) { s.Events[0].Trigger = EventTrigger("manual") },
			wantErr: true,
		},
		{
			name: "improvement signal kind outside enum is rejected",
			mutate: func(s *WorkflowSpec) {
				s.ImprovementSignals = []ImprovementSignal{{Kind: ImprovementSignalKind("noisy"), Watch: "x"}}
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := minimalValidSpec()
			tc.mutate(spec)
			err := ValidateSpecJSON(decodeGeneric(t, spec))
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateSpecJSON accepted an out-of-enum spec; the published schema must reject it")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateSpecJSON rejected a valid in-enum spec: %v", err)
			}
		})
	}
}

// TestGroundTruthExamplesPassEnumSchema is the positive half: every one of the three
// ground-truth RevOps fixtures must still validate against the enum-constrained
// published schema. This guards against the enum table being too tight (an allowed
// value the corpus actually uses being left out).
func TestGroundTruthExamplesPassEnumSchema(t *testing.T) {
	t.Parallel()
	for _, name := range exampleNames {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := loadExample(t, name)
			if err := ValidateSpecJSON(decodeGeneric(t, spec)); err != nil {
				t.Fatalf("ground-truth example %q failed enum-constrained schema: %v", name, err)
			}
		})
	}
}

// TestSchemaEnumsMatchValid asserts the injected enum table is the EXACT allowed set
// each type's Valid() accepts — every listed value is Valid, and a representative
// out-of-set value is not. This keeps the published wire contract in lockstep with
// the in-kernel authoritative gate, so the two cannot diverge silently.
func TestSchemaEnumsMatchValid(t *testing.T) {
	t.Parallel()

	for _, v := range trustTierEnum {
		if !v.Valid() {
			t.Errorf("trustTierEnum lists %q but TrustTier.Valid rejects it", v)
		}
	}
	if TrustTier("trusted").Valid() {
		t.Error("TrustTier.Valid accepted an out-of-set value")
	}
	for _, v := range actionKindEnum {
		if !v.Valid() {
			t.Errorf("actionKindEnum lists %q but ActionKind.Valid rejects it", v)
		}
	}
	if ActionKind("write").Valid() {
		t.Error("ActionKind.Valid accepted an out-of-set value")
	}
	for _, v := range eventTriggerEnum {
		if !v.Valid() {
			t.Errorf("eventTriggerEnum lists %q but EventTrigger.Valid rejects it", v)
		}
	}
	if EventTrigger("manual").Valid() {
		t.Error("EventTrigger.Valid accepted an out-of-set value")
	}
	// ImprovementSignalKind has no in-kernel Valid() gate today (it is not enforced
	// by WorkflowSpec.Validate), so there is no Go method to round-trip against. The
	// published schema still constrains it for cross-language consumers; assert only
	// that the table is non-empty and its values are non-blank so a malformed entry
	// cannot slip in.
	if len(improvementSignalKindEnum) == 0 {
		t.Error("improvementSignalKindEnum is empty")
	}
	for _, v := range improvementSignalKindEnum {
		if v == "" {
			t.Error("improvementSignalKindEnum contains a blank value")
		}
	}
	for _, v := range overlayOpKindEnum {
		if !v.Valid() {
			t.Errorf("overlayOpKindEnum lists %q but OverlayOpKind.Valid rejects it", v)
		}
	}
	if OverlayOpKind("rewrite").Valid() {
		t.Error("OverlayOpKind.Valid accepted an out-of-set value")
	}
}

// TestPublishedSpecSchemaCarriesEnums is a direct assertion on the committed bytes:
// the published workflow-spec schema must literally contain the enum constraint for
// each security-load-bearing field, so a consumer reading the file (not the Go type)
// gets the constraint.
func TestPublishedSpecSchemaCarriesEnums(t *testing.T) {
	t.Parallel()
	raw, err := SpecSchemaBytes()
	if err != nil {
		t.Fatalf("SpecSchemaBytes: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		`"internal-write"`, `"external-write"`, // ActionKind
		`"operator-stated"`,                   // TrustTier
		`"scheduled"`,                         // EventTrigger
		`"recurring-exception"`, `"sla-miss"`, // ImprovementSignalKind
	} {
		if !strings.Contains(got, want) {
			t.Errorf("published spec schema is missing enum value %s", want)
		}
	}
	// And the bare-string-only rendering of an enum field must be gone: a `kind`
	// without an adjacent enum would mean the injection regressed.
	if !strings.Contains(got, `"enum"`) {
		t.Error("published spec schema carries no enum constraints at all")
	}
}
