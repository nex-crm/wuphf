package team

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
)

// integrationResolveRequest is what the action gate posts to /integrations/
// resolve before running an external action. Full args (not a digest) cross the
// wire so the resolver can build the preview raw envelope for the approval
// modal via a dry-run execute.
type integrationResolveRequest struct {
	Provider        string         `json:"provider,omitempty"`
	Platform        string         `json:"platform"`
	ActionID        string         `json:"action_id"`
	Agent           string         `json:"agent,omitempty"`
	Channel         string         `json:"channel,omitempty"`
	Summary         string         `json:"summary,omitempty"`
	Data            map[string]any `json:"data,omitempty"`
	PathVariables   map[string]any `json:"path_variables,omitempty"`
	QueryParameters map[string]any `json:"query_parameters,omitempty"`
	Headers         map[string]any `json:"headers,omitempty"`
}

type integrationResolveAccount struct {
	Name string `json:"name,omitempty"`
	Key  string `json:"key,omitempty"`
}

type integrationResolveEnvelope struct {
	Method  string         `json:"method,omitempty"`
	URL     string         `json:"url,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

// integrationResolveResponse is the resolver's verdict plus the structured
// render payload the dedicated approval modal needs. Decision is one of
// proceed/approve/connect/wait/fail_safe/fallback.
type integrationResolveResponse struct {
	Decision    string                      `json:"decision"`
	State       string                      `json:"state"`
	Provider    string                      `json:"provider"`
	Platform    string                      `json:"platform"`
	ActionID    string                      `json:"action_id"`
	Name        string                      `json:"name,omitempty"`
	LogoURL     string                      `json:"logo_url,omitempty"`
	ReadOnly    bool                        `json:"read_only"`
	Account     *integrationResolveAccount  `json:"account,omitempty"`
	RawEnvelope *integrationResolveEnvelope `json:"raw_envelope,omitempty"`
	Detail      string                      `json:"detail,omitempty"`
	// RequestID is the connect card the resolver raised for a `connect` decision.
	// The gate surfaces it to the agent so it can point the human at the waiting
	// card instead of retrying blind. Empty for every other decision.
	RequestID string `json:"request_id,omitempty"`
}

func (b *Broker) handleIntegrationResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req integrationResolveRequest
	if !decodeIntegrationRequest(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Platform) == "" || strings.TrimSpace(req.ActionID) == "" {
		http.Error(w, "platform and action_id are required", http.StatusBadRequest)
		return
	}
	resp := b.resolveExternalAction(r.Context(), req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// resolveExternalAction is the pre-flight gate: it probes the connection,
// folds in the cached registry (fail-safe on outage), and classifies the action
// into a single Decision. An unresolved connection can never reach the
// provider's execute call — the gate acts only on the returned Decision.
func (b *Broker) resolveExternalAction(ctx context.Context, req integrationResolveRequest) integrationResolveResponse {
	platform := strings.TrimSpace(req.Platform)
	actionID := strings.TrimSpace(req.ActionID)
	readOnly := action.ActionIsReadOnly(actionID)

	composio := action.NewComposioFromEnv()
	resp := integrationResolveResponse{
		Provider: "composio",
		Platform: platform,
		ActionID: actionID,
		ReadOnly: readOnly,
		Name:     action.DisplayPlatformName(platform),
		LogoURL:  curatedToolkitLogo(platform),
	}

	var (
		probeOK bool
		probed  action.ConnectionState
		connKey string
	)
	switch {
	case !composio.Configured():
		// Not set up: route to connect so the connect decision can guide setup,
		// rather than masquerading as a provider outage.
		probeOK = true
		probed = action.StateMissing
		resp.Detail = "Composio is not configured."
	default:
		status, err := composio.GetIntegrationConnectionStatus(ctx, action.IntegrationStatusRequest{
			Provider: "composio",
			Platform: platform,
		})
		if err != nil {
			// The probe CALL failed (provider unreachable): leave probeOK false
			// so Resolve serves last-known-good rather than a false connect.
			resp.Detail = "Composio is unreachable; using last-known connection state."
		} else {
			probeOK = true
			probed = action.MapConnectionState(status.Status)
			connKey = strings.TrimSpace(status.ConnectionKey)
			if probed == action.StateMissing && !b.composioPlatformSupported(ctx, composio, platform) {
				probed = action.StateUnsupported
			}
		}
	}

	lastEntry, hasLast := b.lookupConnectionRegistry(platform)
	lastKnown := action.StateUnknown
	if hasLast {
		lastKnown = action.ConnectionState(lastEntry.State)
	}
	lastFresh := hasLast && connectionRegistryFresh(lastEntry, time.Now().UTC())

	// A standing, human-issued grant for this exact (agent, platform, action_id)
	// lets a connected mutating action skip the approval modal. Read-only actions
	// already proceed without a grant, so only evaluate it for mutating actions.
	// The grant is re-read on every resolve, which runs immediately before
	// execute — the accepted TOCTOU window is just the resolve→execute gap.
	hasGrant := !readOnly && b.hasActiveActionGrant(req.Agent, platform, actionID, time.Now().UTC())

	decision, effective := action.Resolve(action.ResolveInput{
		ReadOnly:       readOnly,
		Probed:         probed,
		ProbeOK:        probeOK,
		LastKnown:      lastKnown,
		LastKnownFresh: lastFresh,
		HasGrant:       hasGrant,
	})
	resp.Decision = string(decision)
	resp.State = string(effective)

	// Refresh the registry on a successful probe. Never record indeterminate —
	// an outage must not overwrite the last-known-good the fail-safe relies on.
	if probeOK && probed != action.StateIndeterminate {
		b.upsertConnectionRegistry(connectionRegistryEntry{
			Platform:      platform,
			Provider:      "composio",
			State:         string(probed),
			ConnectionKey: connKey,
		})
	}

	acctKey := connKey
	if acctKey == "" && hasLast {
		acctKey = lastEntry.ConnectionKey
	}
	acctName := ""
	if hasLast {
		acctName = lastEntry.AccountName
	}
	if acctKey != "" || acctName != "" {
		resp.Account = &integrationResolveAccount{Name: acctName, Key: acctKey}
	}

	// A `connect` decision raises (or reattaches to) the blocking Connect card so
	// the human has a concrete OAuth call-to-action. Dedupe is workspace-wide, so
	// repeated resolves of the same missing platform collapse onto one card.
	if decision == action.DecisionConnect {
		resp.RequestID = b.ensureConnectRequest(platform, req.Channel, req.Agent, resp.Name, resp.LogoURL)
	}

	// A `fallback` decision means the platform has no Composio path at all: raise
	// a manual-handoff card scoped to this (platform, action) so the human can
	// complete it by hand and mark it done.
	if decision == action.DecisionFallback {
		resp.RequestID = b.ensureFallbackRequest(platform, actionID, req.Channel, req.Agent, resp.Name, resp.LogoURL, req.Summary)
	}

	// Build the preview envelope only when the human will actually see the modal.
	if decision == action.DecisionApprove {
		dry, err := composio.ExecuteAction(ctx, action.ExecuteRequest{
			Platform:        platform,
			ActionID:        actionID,
			ConnectionKey:   acctKey,
			Data:            req.Data,
			PathVariables:   req.PathVariables,
			QueryParameters: req.QueryParameters,
			Headers:         req.Headers,
			DryRun:          true,
		})
		if err == nil {
			resp.RawEnvelope = &integrationResolveEnvelope{
				Method:  dry.Request.Method,
				URL:     dry.Request.URL,
				Headers: maskSensitivePayload(dry.Request.Headers),
				Data:    maskSensitivePayload(dry.Request.Data),
			}
		} else {
			// Log so operators can diagnose a broken approval preview, but do not
			// block the modal — the human can still decide from the action id and
			// account alone (resp.RawEnvelope stays nil).
			fmt.Fprintf(os.Stderr, "broker: dry-run preview for %s/%s failed: %v\n", platform, actionID, err)
		}
	}
	return resp
}

// composioPlatformSupported reports whether a platform has an OAuth path via
// Composio (so a missing connection routes to connect, not the manual-handoff
// fallback). Curated toolkits are known-supported without a call; otherwise it
// best-effort checks the catalog and defaults to supported on uncertainty so a
// transient catalog error never forces a spurious fallback.
func (b *Broker) composioPlatformSupported(ctx context.Context, composio *action.ComposioREST, platform string) bool {
	if isCuratedComposioToolkit(platform) {
		return true
	}
	if composio == nil || !composio.Configured() {
		return true
	}
	catalog, err := composio.ListIntegrationCatalog(ctx, action.IntegrationCatalogOptions{Search: platform, Limit: 10})
	if err != nil {
		return true
	}
	want := connectionRegistryKey(platform)
	for _, item := range catalog.Items {
		if connectionRegistryKey(item.Platform) == want {
			return true
		}
	}
	return false
}

func isCuratedComposioToolkit(platform string) bool {
	key := connectionRegistryKey(platform)
	for _, toolkit := range curatedComposioToolkits {
		if connectionRegistryKey(toolkit.platform) == key {
			return true
		}
	}
	return false
}

func curatedToolkitLogo(platform string) string {
	key := connectionRegistryKey(platform)
	for _, toolkit := range curatedComposioToolkits {
		if connectionRegistryKey(toolkit.platform) == key {
			return toolkit.logoURL
		}
	}
	return ""
}

// sensitivePayloadKeys are masked in the raw envelope before it reaches the
// approval modal. The human approving a send needs to see the payload, but a
// secret leaving in cleartext through the modal would be a disclosure.
var sensitivePayloadKeys = map[string]struct{}{
	"authorization": {}, "api_key": {}, "apikey": {}, "token": {},
	"access_token": {}, "refresh_token": {}, "secret": {}, "client_secret": {},
	"password": {}, "private_key": {}, "bearer": {},
	// Composio-internal routing identifiers: they authenticate future calls
	// against the connected account and carry no human-readable meaning for the
	// approver, so they are masked rather than surfaced in the modal or logs.
	"connected_account_id": {}, "user_id": {},
}

// maskSensitivePayload returns a copy of m with sensitive values replaced by a
// fixed mask, recursing into nested maps AND into arrays of maps so a secret
// nested inside a list cannot escape the mask. The original is never mutated.
func maskSensitivePayload(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if _, sensitive := sensitivePayloadKeys[strings.ToLower(strings.TrimSpace(k))]; sensitive {
			out[k] = "***"
			continue
		}
		out[k] = maskSensitiveValue(v)
	}
	return out
}

// maskSensitiveValue masks nested maps and arrays-of-maps, leaving scalars
// untouched.
func maskSensitiveValue(v any) any {
	switch nested := v.(type) {
	case map[string]any:
		return maskSensitivePayload(nested)
	case []any:
		masked := make([]any, len(nested))
		for i, elem := range nested {
			masked[i] = maskSensitiveValue(elem)
		}
		return masked
	default:
		return v
	}
}
