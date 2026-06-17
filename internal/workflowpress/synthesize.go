package workflowpress

import (
	"fmt"
	"sort"
	"strings"
)

// synthesize.go is the first half of Phase 3: it turns distilled
// WorkflowResearch into a DRAFT WorkflowSpec. Synthesis is the bridge between
// the messy, evidence-rich research (outside the kernel) and the canonical
// contract (inside it). It is deterministic — the same research always yields
// the same draft — and it never freezes anything: the draft it returns is
// explicitly unfrozen and only becomes a contract through Freeze (freeze.go),
// which is the operator review gate.
//
// # Why a blueprint, and what is "inferred"
//
// The spec's first load-bearing principle is that discovery does NOT become
// code: it becomes an evidence-backed IR first, and inference is LOSSY. Some of
// a workflow's shape is genuinely derivable from evidence — the entities (from
// the count-based-nullability InferredSchemas), the read/write classification of
// each action (HTTP method), the internal-vs-external split (request host), the
// exceptions (ObservedExceptions), the improvement signals (OperatorEdits and
// recurring exceptions). But the narrative SKELETON of the state machine — the
// named states a record passes through, the events that drive them, the guard
// expressions that gate them — is operator domain knowledge, not something a
// generic algorithm reads out of HTTP traces.
//
// So Synthesize fuses two inputs:
//
//   - the evidence-derived signals, carried deterministically from the research;
//   - a per-workflow SynthesisBlueprint: the state-machine skeleton, supplied by
//     an injected BlueprintRegistry keyed by workflow id. This stands in for the
//     model's structural proposal; encoding it deterministically is what makes
//     synthesis testable and reproducible instead of a freeform LLM call.
//
// The registry is the kernel-boundary seam for per-workflow domain knowledge: the
// blueprints (and the guard thresholds/aliases each carries) live OUTSIDE the
// kernel, in an injected registry, so adding a new workflow edits the registry —
// never this synthesis machinery and never the runner runtime. The kernel holds
// the SynthesisBlueprint SHAPE and the carry/degrade logic; the registry holds the
// per-workflow DATA.
//
// Every element the operator did not explicitly state is marked TrustInferred
// (or carried at its observed tier), and every inferred/observed write-action is
// forced to RequiresApproval. The draft is therefore cautious by construction:
// the trust tier degrades writes to a human-approved gate, and the freeze step
// is where the operator confirms or corrects the inferences. Nothing here trusts
// the inference without that downstream review.

// SynthesisBlueprint is the deterministic state-machine skeleton Synthesize
// fuses with evidence-derived signals to produce a draft for one workflow. It
// captures the operator domain knowledge a generic inference pass cannot read
// out of HTTP traces: the named states, the events that drive them, the guards
// that gate them, the action graph, the SLAs, and the verification scenarios.
//
// A blueprint is NOT a frozen spec and carries no version: it is the structural
// proposal, and the evidence fills in the signals (entities, exceptions,
// improvement signals) and overrides provenance/approval where the evidence is
// authoritative.
type SynthesisBlueprint struct {
	// Goal and Operator describe the workflow. Operator defaults to the research's
	// owner; Goal is the blueprint's one-line statement of what it accomplishes.
	Goal     string
	Operator string
	// States, Events, Guards, Actions, SLAs and VerificationScenarios are the
	// skeleton. Provenance on each element is the blueprint's claim; Synthesize
	// keeps operator-stated tiers and degrades everything else to inferred unless
	// the evidence raises it.
	States                []State
	Events                []Event
	Guards                []Guard
	Actions               []Action
	SLAs                  []SLA
	VerificationScenarios []VerificationScenario
	// EntityProvenance optionally overrides the provenance Synthesize attaches to
	// an entity carried from the inferred schemas, keyed by entity name. An entity
	// the operator explicitly named (e.g. the AccountExecutive roster) can be
	// raised to operator-stated; otherwise entities carry an observed tier because
	// they came from real sample records.
	EntityProvenance map[string]Provenance
	// EntityOrder optionally fixes the emitted entity order (and which entities to
	// emit at all) so the draft's entity list matches the hand-authored contract
	// deterministically. When empty, entities are emitted in sorted-by-name order
	// from the inferred schemas.
	EntityOrder []string
	// EntityDescriptions optionally attaches a human description to a carried
	// entity, keyed by entity name.
	EntityDescriptions map[string]string
	// GuardConfig is the per-workflow guard-evaluation knowledge (named thresholds
	// + fixture aliases) the blueprint's guards reference. Synthesize copies it onto
	// the draft spec so the contract is self-contained and the kernel runner holds
	// no per-workflow constants. Optional: a blueprint whose guards compare only
	// fixture-carried operands may leave it zero.
	GuardConfig GuardConfig
}

// BlueprintRegistry supplies the per-workflow SynthesisBlueprint Synthesize fuses
// with evidence. It is the kernel-boundary seam: the blueprints (and the guard
// thresholds/aliases they carry) are per-workflow DOMAIN KNOWLEDGE that lives
// OUTSIDE the kernel, injected here, so adding a new workflow registers a
// blueprint with a registry and edits NO kernel file (not this synthesis
// machinery, not the runner runtime). The kernel defines the seam and the
// carry/degrade logic; an implementation (see RevOps registry, outside the
// kernel) holds the data.
type BlueprintRegistry interface {
	// Blueprint returns the registered skeleton for a workflow id, and false when
	// none is registered. Implementations must be deterministic: the same id always
	// returns the same blueprint.
	Blueprint(workflowID string) (SynthesisBlueprint, bool)
}

// MapBlueprintRegistry is the simplest BlueprintRegistry: a map of workflow id ->
// blueprint. An outside-kernel package builds one with NewBlueprintRegistry from
// its per-workflow blueprint set; the kernel never hardcodes the entries.
type MapBlueprintRegistry map[string]SynthesisBlueprint

// Blueprint implements BlueprintRegistry.
func (m MapBlueprintRegistry) Blueprint(workflowID string) (SynthesisBlueprint, bool) {
	bp, ok := m[workflowID]
	return bp, ok
}

// NewBlueprintRegistry builds a MapBlueprintRegistry from a workflow-id-keyed set
// of blueprints. It copies the input map so the caller cannot mutate the registry
// after construction. This is the constructor an outside-kernel workflow package
// uses to register its blueprints without touching any kernel file.
func NewBlueprintRegistry(blueprints map[string]SynthesisBlueprint) MapBlueprintRegistry {
	out := make(MapBlueprintRegistry, len(blueprints))
	for id, bp := range blueprints {
		out[id] = bp
	}
	return out
}

// Synthesize deterministically carries research signals into a DRAFT
// WorkflowSpec, using the injected BlueprintRegistry for the per-workflow
// state-machine skeleton. The draft is NOT frozen: it is the operator-reviewable
// proposal that Freeze turns into a contract only on approval.
//
// The pipeline is:
//
//  1. look up the per-workflow blueprint in the registry (the state-machine
//     skeleton — per-workflow knowledge that lives outside the kernel);
//  2. carry the entities from the research's InferredSchemas, attaching fields
//     and provenance (observed by default — they came from real records —
//     raised to operator-stated only where the blueprint says the operator named
//     them);
//  3. take states, events, guards, actions, SLAs and verification scenarios from
//     the blueprint, degrading every non-operator-stated element to inferred;
//  4. force RequiresApproval on every inferred/observed write-action (the
//     trust-tier security rule);
//  5. copy the blueprint's GuardConfig (thresholds + aliases) onto the draft so
//     the contract carries its own guard constants — the kernel runner holds
//     none;
//  6. derive exceptions from the research's ObservedExceptions and improvement
//     signals from its OperatorEdits, recurring exceptions and SLAs.
//
// It returns a draft whose STRUCTURE matches the hand-authored contract for the
// workflow. Synthesize does not call Validate; the draft may legitimately be
// incomplete until the operator reviews it. Freeze is where validation gates.
func Synthesize(research WorkflowResearch, registry BlueprintRegistry) (WorkflowSpec, error) {
	id := strings.TrimSpace(research.WorkflowID)
	if id == "" {
		return WorkflowSpec{}, fmt.Errorf("workflowpress: synthesize: %w: workflow_id", ErrEmptyField)
	}
	if registry == nil {
		return WorkflowSpec{}, fmt.Errorf("workflowpress: synthesize: %w: nil blueprint registry", ErrNoBlueprint)
	}
	bp, ok := registry.Blueprint(id)
	if !ok {
		return WorkflowSpec{}, fmt.Errorf("workflowpress: synthesize: %w: no blueprint for workflow %q", ErrNoBlueprint, id)
	}

	operator := bp.Operator
	if operator == "" {
		operator = defaultOperator
	}

	draft := WorkflowSpec{
		SchemaVersion: SchemaVersionWorkflowSpec,
		ID:            id,
		Version:       draftVersion,
		Goal:          bp.Goal,
		Operator:      operator,
		Entities:      synthEntities(research, bp),
		States:        cloneStates(bp.States),
		Events:        cloneEvents(bp.Events),
		Guards:        cloneGuards(bp.Guards),
		Actions:       synthActions(bp.Actions),
		// Exceptions and improvement signals are derived from the evidence, not the
		// blueprint: they are the live signals discovery captured.
		Exceptions:            synthExceptions(research),
		SLAs:                  cloneSLAs(bp.SLAs),
		VerificationScenarios: cloneScenarios(bp.VerificationScenarios),
		ImprovementSignals:    synthImprovementSignals(research, bp),
		// The blueprint's guard constants travel onto the contract so the generated
		// tool is self-contained and the kernel runner holds no per-workflow values.
		GuardConfig: cloneGuardConfig(bp.GuardConfig),
	}
	return draft, nil
}

// cloneGuardConfig deep-copies a GuardConfig so the draft does not alias the
// registry's maps (the registry is shared across syntheses).
func cloneGuardConfig(in GuardConfig) GuardConfig {
	var out GuardConfig
	if len(in.Thresholds) > 0 {
		out.Thresholds = make(map[string]float64, len(in.Thresholds))
		for k, v := range in.Thresholds {
			out.Thresholds[k] = v
		}
	}
	if len(in.FixtureAliases) > 0 {
		out.FixtureAliases = make(map[string]string, len(in.FixtureAliases))
		for k, v := range in.FixtureAliases {
			out.FixtureAliases[k] = v
		}
	}
	return out
}

const (
	// draftVersion is the version a freshly-synthesised draft carries. It is 1
	// because a draft that freezes becomes the first contract version; overlays
	// bump it from there.
	draftVersion = 1
	// defaultOperator is the operator a draft carries when the blueprint does not
	// name one. The ground-truth corpus is all RevOps work.
	defaultOperator = "revops"
)

// synthEntities carries the entities from the research's InferredSchemas into
// draft Entity values. Fields come from the inferred field set (sorted for
// determinism); provenance is observed by default — the entity was seen in real
// sample records — and raised to whatever the blueprint declares (e.g.
// operator-stated for a roster the operator named). EntityOrder fixes which
// entities are emitted and in what order so the draft matches the hand-authored
// contract; an empty order falls back to sorted-by-name over the inferred
// schemas.
func synthEntities(research WorkflowResearch, bp SynthesisBlueprint) []Entity {
	byName := make(map[string]InferredSchema, len(research.InferredSchemas))
	for _, s := range research.InferredSchemas {
		byName[s.Entity] = s
	}

	order := bp.EntityOrder
	if len(order) == 0 {
		order = make([]string, 0, len(byName))
		for name := range byName {
			order = append(order, name)
		}
		sort.Strings(order)
	}

	out := make([]Entity, 0, len(order))
	for _, name := range order {
		schema, seen := byName[name]
		prov, ok := bp.EntityProvenance[name]
		if !ok {
			// Default: the entity came from real sample records, so it is observed,
			// at a confidence reflecting how many records backed it. An entity named
			// only in the blueprint (not seen in evidence) degrades to inferred.
			if seen {
				prov = Provenance{TrustTier: TrustObserved, Confidence: observedEntityConfidence(schema)}
			} else {
				prov = Provenance{TrustTier: TrustInferred, Confidence: lowConfidence}
			}
		}
		out = append(out, Entity{
			Name:        name,
			Description: bp.EntityDescriptions[name],
			Fields:      schemaFieldNames(schema),
			Provenance:  prov,
		})
	}
	return out
}

// observedEntityConfidence maps how many sample records backed an entity onto a
// confidence: more records seen, higher confidence, capped. A single record is
// weaker evidence than six.
func observedEntityConfidence(s InferredSchema) float64 {
	switch {
	case s.SampleCount >= 3:
		return 0.9
	case s.SampleCount == 2:
		return 0.85
	case s.SampleCount == 1:
		return 0.8
	default:
		return lowConfidence
	}
}

// schemaFieldNames returns the inferred field names for an entity, in their
// already-sorted order, so the draft's entity fields are deterministic.
func schemaFieldNames(s InferredSchema) []string {
	if len(s.Fields) == 0 {
		return nil
	}
	out := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		out[i] = f.Name
	}
	return out
}

// synthActions clones the blueprint actions and enforces the trust-tier security
// rule: every inferred or observed write-action is forced to RequiresApproval.
// An operator-stated write may keep RequiresApproval as the blueprint declares
// it (the contract may still gate it, but the rule does not force it). This is
// the same invariant Validate enforces, applied at synthesis so the draft is
// cautious by construction rather than relying on a later validation pass.
func synthActions(in []Action) []Action {
	out := make([]Action, len(in))
	for i, a := range in {
		a.Provenance.Evidence = cloneStrings(a.Provenance.Evidence)
		if a.Kind.IsWrite() && !a.Provenance.IsOperatorStated() {
			a.RequiresApproval = true
		}
		out[i] = a
	}
	return out
}

// synthExceptions derives draft Exceptions from the research's
// ObservedExceptions. Each observed exception becomes a named handling rule with
// observed provenance — these are edge cases discovery actually saw, so they are
// observed, not inferred, and a frequently-seen one carries higher confidence.
// The name is a stable slug derived from the description so the same evidence
// always yields the same exception name.
func synthExceptions(research WorkflowResearch) []Exception {
	if len(research.ObservedExceptions) == 0 {
		return nil
	}
	out := make([]Exception, 0, len(research.ObservedExceptions))
	for _, oe := range research.ObservedExceptions {
		out = append(out, Exception{
			Name:     slugify(oe.Description),
			When:     oe.Description,
			Handling: oe.HandledAs,
			Provenance: Provenance{
				TrustTier:  TrustObserved,
				Confidence: observedExceptionConfidence(oe.Frequency),
			},
		})
	}
	return out
}

// observedExceptionConfidence maps how often an exception was seen onto a
// confidence. A one-off is weaker evidence than one seen many times.
func observedExceptionConfidence(frequency int) float64 {
	switch {
	case frequency >= 3:
		return 0.85
	case frequency == 2:
		return 0.8
	case frequency <= 1:
		return 0.7
	default:
		return lowConfidence
	}
}

// synthImprovementSignals derives the draft's ImprovementSignals from the
// research, fusing three sources, each deterministic:
//
//   - every OperatorEdit becomes an operator-edit signal watching the edited
//     path (a path the operator keeps correcting should propose an overlay);
//   - every recurring ObservedException (seen more than once) becomes a
//     recurring-exception signal watching that exception;
//   - every blueprint SLA becomes an sla-miss signal watching that SLA.
//
// Output is deterministic: edits and exceptions follow the research order,
// SLAs follow the blueprint order.
func synthImprovementSignals(research WorkflowResearch, bp SynthesisBlueprint) []ImprovementSignal {
	var out []ImprovementSignal

	for _, edit := range research.OperatorEdits {
		path := strings.TrimSpace(edit.Path)
		if path == "" {
			continue
		}
		out = append(out, ImprovementSignal{
			Kind:  SignalOperatorEdit,
			Watch: path,
		})
	}

	for _, oe := range research.ObservedExceptions {
		if oe.Frequency <= 1 {
			continue
		}
		out = append(out, ImprovementSignal{
			Kind:      SignalRecurringException,
			Watch:     slugify(oe.Description),
			Threshold: fmt.Sprintf("%d seen", oe.Frequency),
		})
	}

	for _, sla := range bp.SLAs {
		out = append(out, ImprovementSignal{
			Kind:      SignalSLAMiss,
			Watch:     sla.Name,
			Threshold: "any miss",
		})
	}

	return out
}

// lowConfidence is the confidence attached to a purely-inferred element with no
// strong evidence behind it.
const lowConfidence = 0.6

// slugify turns a free-form exception description into a stable, lower-snake
// slug so the same evidence always produces the same exception name. It keeps
// only alphanumerics and collapses runs of other characters to a single
// underscore. Empty input yields "exception".
func slugify(s string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "exception"
	}
	return out
}

// --- defensive clone helpers for blueprint skeletons ---
//
// The blueprint registry is package-global; Synthesize must not let a returned
// draft alias the registry's slices, or one synthesis could mutate the skeleton
// another synthesis reads. Each helper deep-copies the slice it carries.

func cloneStates(in []State) []State {
	if len(in) == 0 {
		return nil
	}
	out := make([]State, len(in))
	for i, s := range in {
		s.Provenance.Evidence = cloneStrings(s.Provenance.Evidence)
		out[i] = s
	}
	return out
}

func cloneEvents(in []Event) []Event {
	if len(in) == 0 {
		return nil
	}
	out := make([]Event, len(in))
	for i, e := range in {
		e.Provenance.Evidence = cloneStrings(e.Provenance.Evidence)
		out[i] = e
	}
	return out
}

func cloneGuards(in []Guard) []Guard {
	if len(in) == 0 {
		return nil
	}
	out := make([]Guard, len(in))
	for i, g := range in {
		g.Provenance.Evidence = cloneStrings(g.Provenance.Evidence)
		out[i] = g
	}
	return out
}

func cloneSLAs(in []SLA) []SLA {
	if len(in) == 0 {
		return nil
	}
	out := make([]SLA, len(in))
	for i, s := range in {
		s.Provenance.Evidence = cloneStrings(s.Provenance.Evidence)
		out[i] = s
	}
	return out
}

func cloneScenarios(in []VerificationScenario) []VerificationScenario {
	if len(in) == 0 {
		return nil
	}
	out := make([]VerificationScenario, len(in))
	for i, sc := range in {
		out[i] = VerificationScenario{
			Name:           sc.Name,
			When:           sc.When,
			ExpectApproval: sc.ExpectApproval,
		}
		if len(sc.Given) > 0 {
			out[i].Given = make(map[string]string, len(sc.Given))
			for k, v := range sc.Given {
				out[i].Given[k] = v
			}
		}
		if len(sc.ExpectTransitions) > 0 {
			out[i].ExpectTransitions = make([]Transition, len(sc.ExpectTransitions))
			copy(out[i].ExpectTransitions, sc.ExpectTransitions)
		}
	}
	return out
}
