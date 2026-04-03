package workflow

import (
	"fmt"
	"strings"
)

// ValidateSpec checks a workflow spec for structural correctness.
// Returns nil if valid, or the first error found.
//
// Checks performed:
//   - Spec has an ID and at least one step
//   - All step IDs are unique
//   - All step types are valid (select, confirm, edit, submit, run)
//   - All transition targets reference valid step IDs or "done"
//   - No circular transitions unless step has allowLoop: true
//   - DataRef paths start with "/"
//   - Actions have non-empty key and label
//   - Execute specs have a valid provider
func ValidateSpec(spec WorkflowSpec) error {
	if strings.TrimSpace(spec.ID) == "" {
		return fmt.Errorf("workflow spec missing id")
	}
	if len(spec.Steps) == 0 {
		return fmt.Errorf("workflow spec must include at least one step")
	}

	stepIDs := map[string]bool{}
	for i, step := range spec.Steps {
		if err := validateStep(step, i); err != nil {
			return err
		}
		if stepIDs[step.ID] {
			return fmt.Errorf("duplicate step id %q", step.ID)
		}
		stepIDs[step.ID] = true
	}

	// Validate all transition targets exist.
	for _, step := range spec.Steps {
		// Step-level transition (auto-transition for submit/run steps).
		if target := strings.TrimSpace(step.Transition); target != "" && target != TransitionDone {
			if !stepIDs[target] {
				return fmt.Errorf("step %q transitions to unknown step %q", step.ID, target)
			}
		}
		for _, action := range step.Actions {
			target := strings.TrimSpace(action.Transition)
			if target == "" || target == TransitionDone {
				continue
			}
			if !stepIDs[target] {
				return fmt.Errorf("step %q action %q transitions to unknown step %q", step.ID, action.Key, target)
			}
		}
		if target := strings.TrimSpace(step.OnError); target != "" && target != TransitionDone {
			if !stepIDs[target] {
				return fmt.Errorf("step %q onError transitions to unknown step %q", step.ID, target)
			}
		}
	}

	// Validate no circular transitions (unless allowLoop).
	if err := validateNoCycles(spec); err != nil {
		return err
	}

	// Validate data sources.
	dsIDs := map[string]bool{}
	for _, ds := range spec.DataSources {
		if strings.TrimSpace(ds.ID) == "" {
			return fmt.Errorf("data source missing id")
		}
		if dsIDs[ds.ID] {
			return fmt.Errorf("duplicate data source id %q", ds.ID)
		}
		dsIDs[ds.ID] = true
		if strings.TrimSpace(ds.Provider) == "" {
			return fmt.Errorf("data source %q missing provider", ds.ID)
		}
		if strings.TrimSpace(ds.Action) == "" {
			return fmt.Errorf("data source %q missing action", ds.ID)
		}
	}

	return nil
}

func validateStep(step StepSpec, index int) error {
	if strings.TrimSpace(step.ID) == "" {
		return fmt.Errorf("step %d missing id", index+1)
	}
	switch step.Type {
	case StepSelect, StepConfirm, StepEdit, StepSubmit, StepRun:
		// valid
	default:
		return fmt.Errorf("step %q has invalid type %q (must be select, confirm, edit, submit, or run)", step.ID, step.Type)
	}

	// DataRef must start with "/" if present.
	if step.DataRef != "" && !strings.HasPrefix(step.DataRef, "/") {
		return fmt.Errorf("step %q dataRef must start with / (got %q)", step.ID, step.DataRef)
	}
	if step.Display != nil && step.Display.DataRef != "" && !strings.HasPrefix(step.Display.DataRef, "/") {
		return fmt.Errorf("step %q display.dataRef must start with / (got %q)", step.ID, step.Display.DataRef)
	}

	// Validate actions.
	keys := map[string]bool{}
	for _, action := range step.Actions {
		if strings.TrimSpace(action.Key) == "" {
			return fmt.Errorf("step %q has action with empty key", step.ID)
		}
		if strings.TrimSpace(action.Label) == "" {
			return fmt.Errorf("step %q action %q has empty label", step.ID, action.Key)
		}
		if keys[action.Key] {
			return fmt.Errorf("step %q has duplicate action key %q", step.ID, action.Key)
		}
		keys[action.Key] = true

		if action.Execute != nil {
			if err := validateExecute(step.ID, action.Key, *action.Execute); err != nil {
				return err
			}
		}
	}

	// Validate step-level execute.
	if step.Execute != nil {
		if err := validateExecute(step.ID, "(step)", *step.Execute); err != nil {
			return err
		}
	}

	// Run steps need an agent/workflow target or a step-level execute.
	if step.Type == StepRun {
		hasTarget := step.Agent != "" || step.Workflow != "" || step.Execute != nil
		if !hasTarget {
			return fmt.Errorf("step %q (type run) must specify agent, workflow, or execute", step.ID)
		}
	}

	return nil
}

func validateExecute(stepID, actionKey string, exec ExecuteSpec) error {
	switch exec.Provider {
	case ProviderComposio:
		if strings.TrimSpace(exec.Action) == "" {
			return fmt.Errorf("step %q action %q: composio execute missing action", stepID, actionKey)
		}
	case ProviderBroker:
		if strings.TrimSpace(exec.Method) == "" {
			return fmt.Errorf("step %q action %q: broker execute missing method", stepID, actionKey)
		}
	case ProviderAgent:
		if strings.TrimSpace(exec.Slug) == "" {
			return fmt.Errorf("step %q action %q: agent execute missing slug", stepID, actionKey)
		}
	default:
		return fmt.Errorf("step %q action %q: unknown provider %q (must be composio, broker, or agent)", stepID, actionKey, exec.Provider)
	}
	return nil
}

// validateNoCycles does a DFS from each step to detect cycles.
// Steps with allowLoop: true are permitted to be part of cycles.
func validateNoCycles(spec WorkflowSpec) error {
	// Build adjacency: step → set of target step IDs.
	adj := map[string][]string{}
	allowLoop := map[string]bool{}
	for _, step := range spec.Steps {
		allowLoop[step.ID] = step.AllowLoop
		// Step-level auto-transition.
		if target := strings.TrimSpace(step.Transition); target != "" && target != TransitionDone {
			adj[step.ID] = append(adj[step.ID], target)
		}
		for _, action := range step.Actions {
			target := strings.TrimSpace(action.Transition)
			if target != "" && target != TransitionDone {
				adj[step.ID] = append(adj[step.ID], target)
			}
		}
	}

	// DFS cycle detection.
	const (
		white = 0 // unvisited
		gray  = 1 // in current path
		black = 2 // fully explored
	)
	color := map[string]int{}

	var dfs func(node string) error
	dfs = func(node string) error {
		color[node] = gray
		for _, next := range adj[node] {
			if color[next] == gray {
				// Cycle found. Check if the back-edge target allows loops.
				if !allowLoop[next] {
					return fmt.Errorf("circular transition detected: step %q transitions back to %q (set allowLoop: true on %q to permit)", node, next, next)
				}
				continue
			}
			if color[next] == white {
				if err := dfs(next); err != nil {
					return err
				}
			}
		}
		color[node] = black
		return nil
	}

	for _, step := range spec.Steps {
		if color[step.ID] == white {
			if err := dfs(step.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
