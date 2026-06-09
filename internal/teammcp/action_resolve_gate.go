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

// actionResolveEnvelope mirrors the broker's masked preview envelope — the real
// HTTP request the action would send, secrets already masked. Carried onto the
// approval card so the raw toggle shows what actually goes over the wire.
type actionResolveEnvelope struct {
	Method  string         `json:"method,omitempty"`
	URL     string         `json:"url,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

// actionResolveResponse mirrors the broker's integrationResolveResponse. Only
// the fields the gate acts on are decoded.
type actionResolveResponse struct {
	Decision    string                 `json:"decision"`
	State       string                 `json:"state"`
	Platform    string                 `json:"platform"`
	ActionID    string                 `json:"action_id"`
	Name        string                 `json:"name,omitempty"`
	ReadOnly    bool                   `json:"read_only"`
	LogoURL     string                 `json:"logo_url,omitempty"`
	Account     *actionResolveAccount  `json:"account,omitempty"`
	RawEnvelope *actionResolveEnvelope `json:"raw_envelope,omitempty"`
	Detail      string                 `json:"detail,omitempty"`
	// RequestID is the connect card the broker raised for a `connect` decision,
	// so the gate can point the human at the waiting card by name.
	RequestID string `json:"request_id,omitempty"`
}

// actionCardPayload is the structured external-action payload the gate attaches
// to an approval request (slice 4b). It marshals to the broker's
// integration_action shape. The raw envelope is the masked preview the resolver
// already built — the gate only relays it, never the unmasked args.
type actionCardPayload struct {
	Platform    string                 `json:"platform,omitempty"`
	ActionID    string                 `json:"action_id,omitempty"`
	Verb        string                 `json:"verb,omitempty"`
	Name        string                 `json:"name,omitempty"`
	LogoURL     string                 `json:"logo_url,omitempty"`
	Account     *actionResolveAccount  `json:"account,omitempty"`
	RawEnvelope *actionResolveEnvelope `json:"raw_envelope,omitempty"`
}

// buildActionCardPayload composes the structured approval payload from the args
// the agent passed and the resolver's verdict (account + masked envelope).
func buildActionCardPayload(args TeamActionExecuteArgs, decision actionResolveResponse) *actionCardPayload {
	platform := strings.TrimSpace(args.Platform)
	actionID := strings.TrimSpace(args.ActionID)
	return &actionCardPayload{
		Platform:    platform,
		ActionID:    actionID,
		Verb:        actionVerbLabel(platform, actionID),
		Name:        strings.TrimSpace(decision.Name),
		LogoURL:     strings.TrimSpace(decision.LogoURL),
		Account:     decision.Account,
		RawEnvelope: decision.RawEnvelope,
	}
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
		card := ""
		if id := strings.TrimSpace(decision.RequestID); id != "" {
			card = fmt.Sprintf(" A Connect card (%s) is now waiting for the human; it resumes this action automatically once connected.", id)
		} else {
			card = fmt.Sprintf(" Ask the human to connect %s in the Integrations app.", platformLabel)
		}
		return fmt.Sprintf(
			"%s is not connected%s.%s Do NOT retry this action until it is connected.",
			platformLabel, suffix, card,
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
		card := ""
		if id := strings.TrimSpace(decision.RequestID); id != "" {
			card = fmt.Sprintf(" A manual-handoff card (%s) is now waiting for the human.", id)
		}
		return fmt.Sprintf(
			"%s is not available via Composio%s, so this action cannot be automated.%s Do NOT retry — the human will complete it manually.",
			platformLabel, suffix, card,
		)
	default:
		return fmt.Sprintf("action on %s cannot proceed (state=%s)%s.", platformLabel, strings.TrimSpace(decision.State), suffix)
	}
}
