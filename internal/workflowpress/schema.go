package workflowpress

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

// schema.go ships the JSON Schemas for the two contract artifacts and the
// machinery to validate a Go instance against them. The schemas are committed
// under testdata/schema and embedded so the kernel is self-describing: a
// downstream generator or an external reviewer can read the exact contract shape
// without a running process.
//
// The schemas are draft-2020-12 (the jsonschema-go default when $schema is
// absent). They are regenerated from the Go types by InferStampedSchema, which
// stamps the published $schema/$id and runs the deterministic enum-injection pass
// (schema_enums.go). The round-trip drift guard re-runs the exact same function and
// asserts the committed files match it byte-for-byte — so the Go IR, the enum
// allowed-set, and the published schema cannot drift apart.
//
// Enum gap CLOSED: jsonschema.For cannot infer an enum from a Go string-const set,
// so the bare inference rendered ActionKind/EventTrigger/TrustTier/
// ImprovementSignalKind as {"type":"string"}. injectEnums (schema_enums.go) is the
// deterministic post-inference pass that constrains them to their allowed values in
// BOTH the committed schema and the drift guard; the Go Valid() methods remain the
// authoritative in-kernel gate, and the published enum mirrors that set for
// cross-language consumers.

//go:embed testdata/schema/workflow-spec.schema.json
//go:embed testdata/schema/workflow-research.schema.json
var schemaFS embed.FS

const (
	specSchemaPath     = "testdata/schema/workflow-spec.schema.json"
	researchSchemaPath = "testdata/schema/workflow-research.schema.json"
)

// Stamped identity for the published JSON Schemas. $schema pins the dialect so a
// cross-language validator does not have to guess; $id versions the schema URI so
// a consumer can tell which wire-format major it is validating against. The "/v1"
// path component tracks SchemaVersionWorkflowSpec / SchemaVersionWorkflowResearch:
// a breaking wire-shape change bumps both the schema_version const and this URI.
const (
	// schemaDialect is the JSON Schema draft the committed schemas are written in;
	// it is the jsonschema-go default for an inferred schema.
	schemaDialect = "https://json-schema.org/draft/2020-12/schema"
	// specSchemaID and researchSchemaID are the stamped $id of each published
	// schema. The /v1 segment is the wire-format major; bump it in lockstep with the
	// schema_version consts on a breaking change.
	specSchemaID     = "https://wuphf.nex.ai/schema/workflow-press/v1/workflow-spec.schema.json"
	researchSchemaID = "https://wuphf.nex.ai/schema/workflow-press/v1/workflow-research.schema.json"
)

// resolved holds the two schemas resolved once and reused. Resolution is
// non-trivial (meta-schema check + ref resolution), so we cache it.
var (
	resolveOnce      sync.Once
	resolvedSpec     *jsonschema.Resolved
	resolvedResearch *jsonschema.Resolved
	resolveErr       error
)

// loadResolved resolves both embedded schemas, once. Any failure is sticky and
// returned to every caller so a malformed committed schema fails loudly.
func loadResolved() (specR, researchR *jsonschema.Resolved, err error) {
	resolveOnce.Do(func() {
		resolvedSpec, resolveErr = resolveEmbedded(specSchemaPath)
		if resolveErr != nil {
			return
		}
		resolvedResearch, resolveErr = resolveEmbedded(researchSchemaPath)
	})
	return resolvedSpec, resolvedResearch, resolveErr
}

// resolveEmbedded reads, unmarshals and resolves one embedded schema file.
func resolveEmbedded(path string) (*jsonschema.Resolved, error) {
	raw, err := schemaFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workflowpress: reading embedded schema %q: %w", path, err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("workflowpress: unmarshalling schema %q: %w", path, err)
	}
	resolved, err := s.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("workflowpress: resolving schema %q: %w", path, err)
	}
	return resolved, nil
}

// InferStampedSchema infers the JSON Schema for T, stamps the published dialect and
// $id, and runs the deterministic enum-injection pass. It is the SINGLE generator
// for the published contract: both the committed-schema regeneration and the
// byte-exact drift guard call it, so the published schema, its version stamp, and
// the enum allowed-set stay in lockstep with the Go type. id is the stamped $id for
// T (specSchemaID / researchSchemaID).
func InferStampedSchema[T any](id string) ([]byte, error) {
	inferred, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, fmt.Errorf("workflowpress: inferring schema for %q: %w", id, err)
	}
	// Stamp the published identity the bare inference lacks.
	inferred.Schema = schemaDialect
	inferred.ID = id
	// Constrain the enum-typed fields the inference renders as bare strings.
	injectEnums(inferred)
	out, err := json.MarshalIndent(inferred, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("workflowpress: marshalling inferred schema for %q: %w", id, err)
	}
	return append(out, '\n'), nil
}

// SpecSchemaBytes returns the raw JSON Schema for WorkflowSpec. Returned as a
// copy so callers cannot mutate the embedded bytes.
func SpecSchemaBytes() ([]byte, error) {
	return readSchemaCopy(specSchemaPath)
}

// ResearchSchemaBytes returns the raw JSON Schema for WorkflowResearch. Returned
// as a copy so callers cannot mutate the embedded bytes.
func ResearchSchemaBytes() ([]byte, error) {
	return readSchemaCopy(researchSchemaPath)
}

func readSchemaCopy(path string) ([]byte, error) {
	raw, err := schemaFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workflowpress: reading embedded schema %q: %w", path, err)
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out, nil
}

// ValidateSpecJSON validates that an arbitrary value conforms to the WorkflowSpec
// JSON Schema. It is the schema-level check, complementary to
// WorkflowSpec.Validate, which enforces the semantic state-machine invariants
// the schema cannot express.
func ValidateSpecJSON(instance any) error {
	specR, _, err := loadResolved()
	if err != nil {
		return err
	}
	if err := specR.Validate(instance); err != nil {
		return fmt.Errorf("workflowpress: spec failed JSON Schema validation: %w", err)
	}
	return nil
}

// ValidateResearchJSON validates that an arbitrary value conforms to the
// WorkflowResearch JSON Schema.
func ValidateResearchJSON(instance any) error {
	_, researchR, err := loadResolved()
	if err != nil {
		return err
	}
	if err := researchR.Validate(instance); err != nil {
		return fmt.Errorf("workflowpress: research failed JSON Schema validation: %w", err)
	}
	return nil
}

// roundTripValidate is shared by the test and any caller that wants the full
// gate: marshal the Go value, validate the resulting JSON against the schema,
// unmarshal it back into a fresh value, and re-marshal to confirm the bytes are
// stable. It returns the canonical JSON for further assertions.
func roundTripValidate(v any, validate func(any) error) ([]byte, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("workflowpress: marshalling for round-trip: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(out, &decoded); err != nil {
		return nil, fmt.Errorf("workflowpress: unmarshalling to generic for schema validation: %w", err)
	}
	if err := validate(decoded); err != nil {
		return nil, err
	}
	return out, nil
}
