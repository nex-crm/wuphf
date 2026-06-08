package teammcp

import (
	"context"
	"fmt"
	"strings"
)

// action_resolve_gate.go wires the deterministic connection resolver
// (broker: POST /integrations/resolve, internal/team/broker_integrations_resolve.go)
// into the external-action path. Before a mutating action reaches the provider,
// the gate classifies it against the live connection state so it can never fire
// blind into a missing, expired, or unreachable integration.

type actionResolveAccount struct {
	Name string `json:"name,omitempty"`
	Key  string `json:"key,omitempty"`
}

// actionResolveResponse mirrors the broker's integrationResolveResponse. Only
// the fields the gate acts on are decoded.
type actionResolveResponse struct {
	Decision string                `json:"decision"`
	State    string                `json:"state"`
	Platform string                `json:"platform"`
	ActionID string                `json:"action_id"`
	Name     string                `json:"name,omitempty"`
	ReadOnly bool                  `json:"read_only"`
	Account  *actionResolveAccount `json:"account,omitempty"`
	Detail   string                `json:"detail,omitempty"`
}

// resolveActionDecision asks the broker to classify an external action against
// the live connection state. Full args cross the wire so the broker can build
// the preview envelope for the approval modal.
func resolveActionDecision(ctx context.Context, slug, channel string, args TeamActionExecuteArgs) (actionResolveResponse, error) {
	var resp actionResolveResponse
	body := map[string]any{
		"provider":         "composio",
		"platform":         args.Platform,
		"action_id":        args.ActionID,
		"agent":            slug,
		"channel":          channel,
		"summary":          args.Summary,
		"data":             args.Data,
		"path_variables":   args.PathVariables,
		"query_parameters": args.QueryParameters,
		"headers":          args.Headers,
	}
	if err := brokerPostJSON(ctx, "/integrations/resolve", body, &resp); err != nil {
		return actionResolveResponse{}, err
	}
	return resp, nil
}

// actionResolveBlockMessage builds the agent-facing tool error for a resolver
// decision that must NOT execute. RULE ZERO: the agent is told to back off,
// connect, wait, or hand off — never to retry blindly into the same wall.
func actionResolveBlockMessage(decision actionResolveResponse, platformLabel string) string {
	suffix := ""
	if detail := strings.TrimSpace(decision.Detail); detail != "" {
		suffix = " (" + detail + ")"
	}
	switch strings.ToLower(strings.TrimSpace(decision.Decision)) {
	case "connect":
		return fmt.Sprintf(
			"%s is not connected%s. Ask the human to connect %s in the Integrations app, then resume. Do NOT retry this action until it is connected.",
			platformLabel, suffix, platformLabel,
		)
	case "wait":
		return fmt.Sprintf(
			"the %s connection state is still settling%s. Wait a few seconds, then retry once — do NOT loop.",
			platformLabel, suffix,
		)
	case "fail_safe":
		return fmt.Sprintf(
			"%s's integration provider is temporarily unreachable%s. Do NOT assume the action ran. Wait and retry later.",
			platformLabel, suffix,
		)
	case "fallback":
		return fmt.Sprintf(
			"%s is not available via Composio%s, so this action cannot be automated. Surface it to the human to perform manually.",
			platformLabel, suffix,
		)
	default:
		return fmt.Sprintf("action on %s cannot proceed (state=%s)%s.", platformLabel, strings.TrimSpace(decision.State), suffix)
	}
}
