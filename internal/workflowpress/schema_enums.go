package workflowpress

import "github.com/google/jsonschema-go/jsonschema"

// schema_enums.go closes the JSON-Schema ENUM gap. jsonschema.For cannot infer an
// enum from a Go string-const set (TrustTier, ActionKind, EventTrigger,
// ImprovementSignalKind, OverlayOpKind are all `type X string`, so the inferred
// schema renders them as a bare {"type":"string"}). That gap is
// SECURITY-LOAD-BEARING for a cross-language consumer: ActionKind gates the
// write-approval rule and TrustTier degrades inferred/observed writes to human
// approval, so a consumer validating against the bare-string schema would accept a
// spec whose kind or trust_tier is outside the allowed set and silently bypass the
// rule the enum encodes.
//
// injectEnums is the deterministic post-inference pass that fixes this. It is the
// SINGLE source of truth for the allowed values and is used for BOTH the committed
// schema (when it is regenerated) AND the drift-guard comparison, so the published
// contract and the kernel type cannot drift apart. The Go enum's Valid() method
// remains the authoritative gate inside the kernel; this pass mirrors that allowed
// set into the published wire contract so a non-Go consumer enforces the same set.
//
// The pass is purely additive and order-independent: it walks the inferred schema
// tree and, for each enum-typed field it recognises by its position, sets Enum to a
// fixed, deterministically-ordered slice. It never reorders, removes, or otherwise
// mutates the inferred shape — the only delta from the bare inference is the Enum
// arrays — so generation stays deterministic.

// The allowed value sets, declared in a fixed order so the emitted schema is
// byte-stable. These mirror the Go const blocks; the round-trip with each type's
// Valid() is asserted by TestSchemaEnumsMatchValid so this table cannot silently
// drift from the kernel's authoritative gate.
var (
	trustTierEnum = []TrustTier{
		TrustObserved, TrustOperatorStated, TrustInferred,
	}
	actionKindEnum = []ActionKind{
		ActionRead, ActionInternalWrite, ActionExternalWrite,
	}
	eventTriggerEnum = []EventTrigger{
		TriggerExternal, TriggerScheduled, TriggerInternal,
	}
	improvementSignalKindEnum = []ImprovementSignalKind{
		SignalRecurringException, SignalOperatorEdit, SignalSLAMiss,
	}
	overlayOpKindEnum = []OverlayOpKind{
		OpSetGuardExpr, OpSetSLAThreshold, OpAddException,
		OpAddImprovementSignal, OpAddVerificationScenario,
	}
)

// enumScope is the object context an enum-typed property lives in. `kind` is
// ambiguous on its own — it names both an Action.Kind and an ImprovementSignal.Kind
// with DIFFERENT allowed sets — so the pass disambiguates by the name of the array
// property whose items the object is (owner). trust_tier is unambiguous (it only
// ever appears inside a provenance object), so its scope matches any owner.
type enumRule struct {
	// owner is the array property whose items hold this field (e.g. "actions",
	// "events", "improvement_signals"). Empty owner matches any object — used for
	// trust_tier, which only appears inside provenance and is never ambiguous.
	owner string
	// field is the property name to constrain (e.g. "kind", "trigger",
	// "trust_tier").
	field string
	// values are the allowed enum strings in their fixed emit order.
	values []any
}

// enumRules is the deterministic rule table the pass applies. Order does not affect
// output (each rule targets a distinct owner+field), but is kept stable for
// readability. The owner names match the JSON property names on WorkflowSpec.
var enumRules = []enumRule{
	{owner: "", field: "trust_tier", values: stringEnum(trustTierEnum)},
	{owner: "actions", field: "kind", values: stringEnum(actionKindEnum)},
	{owner: "events", field: "trigger", values: stringEnum(eventTriggerEnum)},
	{owner: "improvement_signals", field: "kind", values: stringEnum(improvementSignalKindEnum)},
	// OverlayOpKind lives on OverlayPatch.Ops[].kind, not on the two published
	// artifacts; the rule is declared here so the pass constrains it too whenever an
	// OverlayPatch schema is inferred through injectEnums.
	{owner: "ops", field: "kind", values: stringEnum(overlayOpKindEnum)},
}

// stringEnum converts a typed string-enum slice into the []any of strings the
// jsonschema Enum field wants, preserving order.
func stringEnum[T ~string](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

// injectEnums is the deterministic post-inference pass. It walks the schema tree
// from the root and sets Enum on every enum-typed leaf it recognises, scoping the
// ambiguous `kind` field by its owning array property. It mutates s in place and is
// idempotent: re-running it produces the identical schema.
func injectEnums(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	// The root object is owned by nothing; descend with an empty owner so trust_tier
	// (owner-agnostic) is reached wherever it nests, while owner-scoped rules only
	// fire once the walk passes through the named array.
	walkInjectEnums(s, "")
}

// walkInjectEnums recurses over the schema, carrying the name of the array property
// whose items the current object is (owner). At each object it applies any matching
// rule to its direct properties, then descends into nested properties and array
// items, updating owner as it crosses an array boundary.
func walkInjectEnums(s *jsonschema.Schema, owner string) {
	if s == nil {
		return
	}
	// Apply rules to this object's direct properties.
	for name, prop := range s.Properties {
		for _, r := range enumRules {
			if r.field != name {
				continue
			}
			// owner-agnostic rule (trust_tier) matches anywhere; owner-scoped rules only
			// fire when this object is the items of the named array.
			if r.owner != "" && r.owner != owner {
				continue
			}
			if prop != nil {
				prop.Enum = r.values
			}
		}
	}
	// Descend into each property. Crossing into an array's items changes the owner to
	// that array property's name, so `kind` under "actions" and `kind` under
	// "improvement_signals" resolve to different rules.
	for name, prop := range s.Properties {
		if prop == nil {
			continue
		}
		if prop.Items != nil {
			walkInjectEnums(prop.Items, name)
		}
		// A property may itself be an object (e.g. provenance) carrying nested
		// properties; keep the current owner so trust_tier under provenance is still
		// reached, and array-typed nested fields update owner via the Items branch above.
		if len(prop.Properties) > 0 {
			walkInjectEnums(prop, owner)
		}
	}
	// Also descend through array items at this level (when s itself is an array
	// schema), preserving owner.
	if s.Items != nil {
		walkInjectEnums(s.Items, owner)
	}
}
