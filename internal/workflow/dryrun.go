package workflow

import "fmt"

// DryRunResult describes what an action WOULD do without executing it.
type DryRunResult struct {
	Provider    string         `json:"provider"`
	Action      string         `json:"action"`
	Description string         `json:"description"`
	Data        map[string]any `json:"data,omitempty"`
}

// PreviewAction returns a DryRunResult describing what an ExecuteSpec would do.
// It renders template expressions in the execute data using the supplied data store.
func PreviewAction(exec ExecuteSpec, dataStore map[string]any) DryRunResult {
	result := DryRunResult{
		Provider: exec.Provider,
	}

	switch exec.Provider {
	case ProviderComposio:
		result.Action = exec.Action
		result.Description = fmt.Sprintf("Would execute %s via Composio", exec.Action)
		if exec.ConnectionKey != "" {
			result.Description += fmt.Sprintf(" (connection: %s)", exec.ConnectionKey)
		}
	case ProviderBroker:
		result.Action = exec.Method
		result.Description = fmt.Sprintf("Would call broker method %s", exec.Method)
	case ProviderAgent:
		result.Action = exec.Slug
		result.Description = fmt.Sprintf("Would dispatch to agent %q", exec.Slug)
		if exec.Prompt != "" {
			result.Description += fmt.Sprintf(" with prompt: %s", exec.Prompt)
		}
	default:
		result.Action = exec.Action
		result.Description = fmt.Sprintf("Would execute action via %s", exec.Provider)
	}

	// Include the data payload, resolving any template references where possible.
	if len(exec.Data) > 0 {
		resolved := make(map[string]any, len(exec.Data))
		for k, v := range exec.Data {
			resolved[k] = v
		}
		result.Data = resolved
	}

	return result
}
