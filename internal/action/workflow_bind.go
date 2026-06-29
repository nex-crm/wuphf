package action

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Binding a semantic operator plan to a runnable Composio workflow.
//
// The planner (pi) emits a SEMANTIC plan: ordered steps with a kind, a title, a
// human detail, and an optional integration — no Composio mechanics, because pi
// is stateless and cannot reach the action catalog. This binder turns that plan
// into a composioWorkflowDefinition the existing executor runs deterministically.
//
// The mechanical decisions that need the catalog and judgment — which action_id
// a step maps to, how its params bind, and the run_if gate for a threshold — are
// delegated to a WorkflowActionResolver. That is the one place AI lives (it can
// call SearchActions + an LLM); everything downstream of binding is deterministic.
// The interface keeps the assembler unit-testable with a fake resolver.

// PlanStep is one step of the semantic plan pi registers (snake_case wire shape).
type PlanStep struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // trigger|enrich|ai|decision|action|branch
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	Integration string `json:"integration,omitempty"`
	Gated       bool   `json:"gated,omitempty"`
}

// Plan is the semantic workflow pi registers, as the broker receives it.
type Plan struct {
	Name   string         `json:"name"`
	ToolID string         `json:"tool_id"`
	Steps  []PlanStep     `json:"steps"`
	Inputs map[string]any `json:"inputs,omitempty"`
}

// BoundStep is the resolver's verdict for one plan step: the Composio mechanics
// to execute it. Only the fields relevant to Type need to be set.
type BoundStep struct {
	// Type is the composio step type: action|template|nex_ask|nex_insights.
	Type string
	// Skip drops the step from the runnable workflow entirely (e.g. a pure UI
	// trigger marker with no executable counterpart). Distinct from run_if, which
	// keeps the step but gates it at run time.
	Skip bool

	Platform      string
	ActionID      string
	Params        map[string]any
	ConnectionKey any
	QueryTemplate string
	Template      string

	// RunIf is the deterministic gate authored by the resolver (it knows the
	// bound output shapes), e.g. "steps.score.result.fit >= 80". Empty = always.
	RunIf string
}

// WorkflowActionResolver binds one semantic step to its Composio mechanics. The
// production implementation calls SearchActions + an LLM; tests use a fake.
type WorkflowActionResolver interface {
	Resolve(ctx context.Context, plan Plan, step PlanStep) (BoundStep, error)
}

// BindWorkflowPlan assembles a runnable, validated composioWorkflowDefinition
// from a semantic plan. The resolver supplies each step's mechanics; this
// function owns structure, ordering, run_if pass-through, and validation (so a
// malformed binding is rejected here, before it can ever run). It is provider
// independent — the broker passes the result straight to prov.CreateWorkflow.
func BindWorkflowPlan(ctx context.Context, plan Plan, resolver WorkflowActionResolver) (composioWorkflowDefinition, error) {
	if resolver == nil {
		return composioWorkflowDefinition{}, fmt.Errorf("bind workflow: resolver is required")
	}
	if len(plan.Steps) == 0 {
		return composioWorkflowDefinition{}, fmt.Errorf("bind workflow: plan has no steps")
	}

	steps := make([]workflowStep, 0, len(plan.Steps))
	for _, planStep := range plan.Steps {
		id := strings.TrimSpace(planStep.ID)
		if id == "" {
			return composioWorkflowDefinition{}, fmt.Errorf("bind workflow: a step is missing id")
		}
		bound, err := resolver.Resolve(ctx, plan, planStep)
		if err != nil {
			return composioWorkflowDefinition{}, fmt.Errorf("bind workflow step %q: %w", id, err)
		}
		if bound.Skip {
			continue
		}
		steps = append(steps, workflowStep{
			ID:            id,
			Type:          bound.Type,
			Description:   strings.TrimSpace(planStep.Title),
			RunIf:         strings.TrimSpace(bound.RunIf),
			Platform:      bound.Platform,
			ActionID:      bound.ActionID,
			ConnectionKey: bound.ConnectionKey,
			Data:          bound.Params,
			QueryTemplate: bound.QueryTemplate,
			Template:      bound.Template,
		})
	}
	if len(steps) == 0 {
		return composioWorkflowDefinition{}, fmt.Errorf("bind workflow: every step was skipped, nothing to run")
	}

	def := composioWorkflowDefinition{
		Version:     composioWorkflowVersion,
		Title:       strings.TrimSpace(plan.Name),
		Description: strings.TrimSpace(plan.ToolID),
		Inputs:      plan.Inputs,
		Steps:       steps,
	}

	// Round-trip through the canonical decoder so a bad binding (missing
	// action_id, invalid run_if, duplicate id) fails here, not at run time.
	raw, err := json.Marshal(def)
	if err != nil {
		return composioWorkflowDefinition{}, fmt.Errorf("bind workflow: marshal: %w", err)
	}
	// Validate through the canonical decoder (receiver-independent) so a bad
	// binding fails here, not at run time.
	validated, err := (&ComposioREST{}).decodeWorkflowDefinition(raw)
	if err != nil {
		return composioWorkflowDefinition{}, fmt.Errorf("bind workflow: %w", err)
	}
	return validated, nil
}

// StubWorkflowResolver is a deterministic, network-free resolver used to wire the
// build->run loop end to end before the real SearchActions+LLM resolver lands. It
// drops pure-UI triggers and renders every other step as a template that narrates
// what it will do, so a dry run never reaches the network. The real resolver
// replaces this with bound Composio action steps.
type StubWorkflowResolver struct{}

// NewStubWorkflowResolver returns the placeholder resolver for the FE-first loop.
func NewStubWorkflowResolver() StubWorkflowResolver { return StubWorkflowResolver{} }

func (StubWorkflowResolver) Resolve(_ context.Context, _ Plan, step PlanStep) (BoundStep, error) {
	if strings.EqualFold(strings.TrimSpace(step.Kind), "trigger") {
		return BoundStep{Skip: true}, nil
	}
	label := strings.TrimSpace(step.Title)
	if label == "" {
		label = strings.TrimSpace(step.Detail)
	}
	if integration := strings.TrimSpace(step.Integration); integration != "" {
		label = label + " (via " + integration + ")"
	}
	if label == "" {
		label = "step"
	}
	return BoundStep{Type: "template", Template: label}, nil
}
