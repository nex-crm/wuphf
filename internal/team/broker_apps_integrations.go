package team

// broker_apps_integrations.go is "Bridge v2": a GENERIC integration + LLM
// surface for sandboxed Apps, replacing the bespoke per-feature endpoints
// (the hand-coded /apps/gmail/recent is gone). An App can now call any
// connected integration's READ actions and ask the workspace LLM to reason
// over data it already holds — without a new broker endpoint per feature.
//
// ─────────────────────────── WIDENED SURFACE ───────────────────────────────
// SECURITY: this materially widens what a sandboxed App can reach. It needs a
// security-reviewer sign-off before it ships broadly. The hard invariants the
// review must confirm hold:
//
//  1. READ-ONLY CLASSIFICATION IS SERVER-SIDE. The App sends {platform, action,
//     params}; the broker — never the App — decides read-vs-mutate via
//     action.ActionIsReadOnly (the SAME deterministic table the agent gate
//     uses). A read executes; a mutate is REFUSED execution and instead raises
//     the human ExternalActionApprovalCard. The App cannot smuggle a write by
//     lying about the verb: it only supplies the action_id, and the verb table
//     reclassifies it here.
//  2. MUTATIONS REQUIRE THE SAME APPROVAL CARD AS THE AGENT PATH. We mint a
//     `kind:approval` request carrying the structured integration_action
//     payload (masked envelope) and return {status:"needs_approval",
//     request_id} — the App gets NO execution, only a card the human must
//     approve. There is no code path from an App's call to ExecuteAction for a
//     mutating action.
//  3. ai() IS NOT A NETWORK ESCAPE HATCH. The frame already has connect-src
//     'none'; ai() reasons over data the App fetched through this same bridge.
//     We bound prompt + input size and the App's call rate so it cannot become
//     an exfil/cost channel. It is read-only reasoning, never a tool-call loop.
//
// The HOST (web/src/components/apps/CustomAppFrame.tsx) re-validates every
// inbound message before it reaches these handlers; the App is hostile by
// assumption. These handlers re-validate again — defense in depth.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/provider"
)

const (
	// appIntegrationMaxParamBytes bounds the JSON params an App may pass to a
	// single integration call. Read actions take small filter/limit objects, not
	// payloads.
	appIntegrationMaxParamBytes = 16 << 10 // 16 KiB

	// appAIMaxPromptBytes / appAIMaxInputBytes bound the ai() request so it
	// cannot become a cost or exfil channel. The input is data the App already
	// fetched through this bridge; a digest-sized slice fits comfortably.
	appAIMaxPromptBytes = 8 << 10   // 8 KiB
	appAIMaxInputBytes  = 200 << 10 // 200 KiB
	// appAIMaxOutputChars caps the returned text so a runaway completion cannot
	// bloat the reply.
	appAIMaxOutputChars = 16 << 10 // 16 KiB
	// appAITimeout bounds a single ai() completion.
	appAITimeout = 60 * time.Second

	// appAIRateLimit / appAIRateWindow cap how many ai() completions a single
	// app may trigger per minute. The broker token exempts the web host from the
	// IP bucket, so without this a hostile or buggy App (e.g. one that re-runs an
	// ai() summary on every tab refocus) could loop ai() and burn LLM credits
	// (security review H2). Each completion already costs ~seconds, so a small
	// allowance is plenty for a real digest/score app.
	appAIRateLimit  = 8
	appAIRateWindow = time.Minute
	// appAIDailyLimit / appAIDailyWindow are the HARD per-app spend ceiling: the
	// per-minute cap bounds bursts, but a tab-switch loop that paces itself under
	// 8/min could still grind through tokens all day. The daily cap is the
	// deterministic backstop the build-time guard pairs with.
	appAIDailyLimit  = 150
	appAIDailyWindow = 24 * time.Hour

	// appIntegrationReadLimit / appIntegrationReadDailyLimit bound how many
	// integration READ actions one app may run (per minute / per rolling day).
	// Reads are cheaper than ai() but hit the upstream provider (Composio), which
	// has its OWN rate limits — an app looping reads on focus would risk those for
	// the whole workspace. Mutations are not metered here: they already route
	// through a human approval card and a workspace-wide dedupe.
	appIntegrationReadLimit       = 30
	appIntegrationReadWindow      = time.Minute
	appIntegrationReadDailyLimit  = 1500
	appIntegrationReadDailyWindow = 24 * time.Hour

	// appDBWriteLimit bounds how many DB WRITE ops (define/upsert/clear) one app
	// may run per minute. The store already caps table/row/column GROWTH, but not
	// write FREQUENCY — a buggy app looping upserts would otherwise keep
	// rewriting db.json under the store mutex. 120/min (2/s sustained) is far
	// above any legitimate app's write rate while still stopping tight loops.
	appDBWriteLimit  = 120
	appDBWriteWindow = time.Minute
)

// appsLLMCompleter is the seam the ai() endpoint calls for a one-shot
// completion. Production wires it to provider.RunConfiguredOneShotCtx (the
// workspace's own configured LLM provider); tests substitute a deterministic
// fake so the bounds + JSON handling are unit-testable without a live model.
type appsLLMCompleter func(ctx context.Context, systemPrompt, prompt string) (string, error)

// defaultAppsLLMCompleter runs the workspace's configured LLM provider for a
// bounded one-shot completion.
//
// SECURITY NOTE (security review H1/H3): this routes through the SAME shared
// one-shot path every other broker LLM caller uses (company seed, lint, pam,
// templates). cwd is passed empty; the provider promotes it to the broker's
// working dir, and in CLI-auth mode (no ANTHROPIC_API_KEY) the Claude
// subprocess inherits the user's --setting-sources user MCP config. The system
// prompt pins the model to pure reasoning with no tools, but that is a soft
// guardrail, not a hard sandbox. Hardening the subprocess (e.g. --allowedTools
// "") is a provider-level change affecting all one-shot callers and is tracked
// as a follow-up, not scoped to this surface. ai() input is data the app
// already fetched through this bridge, so it adds no new data the model could
// not already see; the frame's connect-src 'none' still blocks exfil.
func defaultAppsLLMCompleter(ctx context.Context, systemPrompt, prompt string) (string, error) {
	return provider.RunConfiguredOneShotCtx(ctx, systemPrompt, prompt, "")
}

// appsLLMCompleterFn lets tests override the completer. Guarded by a mutex so a
// parallel test setting it never races the handler reading it.
var (
	appsLLMCompleterMu sync.RWMutex
	appsLLMCompleterFn appsLLMCompleter = defaultAppsLLMCompleter
)

func currentAppsLLMCompleter() appsLLMCompleter {
	appsLLMCompleterMu.RLock()
	defer appsLLMCompleterMu.RUnlock()
	return appsLLMCompleterFn
}

// ─────────────────────────── A. integration call ───────────────────────────

// appIntegrationCallRequest is the App's {platform, action, params} call. The
// App supplies only these three; the broker decides everything that matters
// (read vs mutate, connection state, approval).
type appIntegrationCallRequest struct {
	Platform string         `json:"platform"`
	Action   string         `json:"action"`
	Params   map[string]any `json:"params,omitempty"`
	// AppID identifies the calling app so the read budget is per-app, not shared
	// across every app the human has open. Host-supplied (the sealed iframe never
	// sees it), so it is a trustworthy budget key, not a security decision.
	AppID string `json:"app_id,omitempty"`
}

// appIntegrationCallResponse is the business outcome of an App integration call.
// All of these are HTTP 200 — they are expected states an App renders, not
// transport errors:
//
//	{connected:false}                       -> integration not connected
//	{status:"needs_approval", request_id}   -> mutating action, card raised
//	{status:"ok", result}                   -> read executed, result returned
type appIntegrationCallResponse struct {
	Connected bool            `json:"connected"`
	Status    string          `json:"status,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	ReadOnly  bool            `json:"read_only"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// handleAppsIntegrationsCall serves POST /apps/integrations/call. It is the
// generic replacement for every bespoke per-integration App endpoint.
//
//	read action  + connected  -> execute via Composio, return {status:"ok", result}
//	mutate action             -> NEVER execute; raise the approval card,
//	                             return {status:"needs_approval", request_id}
//	not connected             -> {connected:false}
//
// Business outcomes are HTTP 200 so the App renders a state, not an error toast.
func (b *Broker) handleAppsIntegrationsCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req appIntegrationCallRequest
	if !decodeIntegrationRequest(w, r, &req) {
		return
	}
	platform := strings.TrimSpace(req.Platform)
	actionID := strings.TrimSpace(req.Action)
	if platform == "" || actionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "platform and action are required"})
		return
	}
	// Bound the params payload. Read actions take small filter objects; a large
	// blob is either a mistake or an attempt to abuse the upstream call.
	if !appIntegrationParamsWithinBounds(req.Params) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "params payload too large"})
		return
	}

	// READ-VS-MUTATE IS DECIDED HERE, SERVER-SIDE, from the SAME table the agent
	// gate uses. The App never gets to assert "this is read-only."
	readOnly := action.ActionIsReadOnly(actionID)

	// MUTATING ACTION: do NOT execute. Raise the human approval card (the same
	// ExternalActionApprovalCard the agent path raises) and hand the App a
	// request_id to poll. The App gets no execution, ever, without a human click.
	if !readOnly {
		requestID := b.raiseAppActionApproval(r, platform, actionID, req.Params)
		writeJSON(w, http.StatusOK, appIntegrationCallResponse{
			Connected: true,
			Status:    "needs_approval",
			RequestID: requestID,
			ReadOnly:  false,
		})
		return
	}

	// READ ACTION: budget it per-app BEFORE touching the provider. A read loop
	// (e.g. an app refetching on every tab refocus) would otherwise hammer
	// Composio's own rate limits for the whole workspace. Over budget → a clean
	// product error the app renders (HTTP 200), not a thrown transport error.
	if _, limited := b.consumeAppIntegrationReadBudget(appBudgetKey(req.AppID, r)); limited {
		writeJSON(w, http.StatusOK, appIntegrationCallResponse{
			Connected: true,
			ReadOnly:  true,
			Error:     "rate_limited",
		})
		return
	}

	// Resolve the connection and execute. A not-connected / unreachable
	// integration surfaces as {connected:false} (HTTP 200) so the App can render a
	// connect-state.
	composio := action.NewComposioFromEnv()
	connKey, connected := b.resolveAppIntegrationConnection(r.Context(), composio, platform)
	if !connected {
		writeJSON(w, http.StatusOK, appIntegrationCallResponse{
			Connected: false,
			ReadOnly:  true,
			Error:     action.DisplayPlatformName(platform) + " is not connected.",
		})
		return
	}

	res, err := composio.ExecuteAction(r.Context(), action.ExecuteRequest{
		Platform:      platform,
		ActionID:      actionID,
		ConnectionKey: connKey,
		Data:          req.Params,
	})
	if err != nil {
		// Do not echo the upstream error body — it may carry request context.
		fmt.Fprintf(os.Stderr, "broker: apps integration read failed for %s/%s: %v\n", platform, actionID, err)
		writeJSON(w, http.StatusOK, appIntegrationCallResponse{
			Connected: true,
			ReadOnly:  true,
			Error:     "Could not read " + action.DisplayPlatformName(platform) + " right now.",
		})
		return
	}
	// Pass the upstream result straight through. The Composio read layer
	// guarantees valid-JSON-or-clean-error (it errors instead of truncating an
	// oversized response), so the broker does not re-encode or trim content —
	// shaping an app's payload is app policy, not a platform concern. An app that
	// over-fetches should request a leaner payload (see the scaffold's getEmails),
	// not lean on the broker to trim it.
	writeJSON(w, http.StatusOK, appIntegrationCallResponse{
		Connected: true,
		Status:    "ok",
		ReadOnly:  true,
		Result:    res.Response,
	})
}

// appIntegrationParamsWithinBounds reports whether the params object serializes
// under the size cap. Nil/empty is fine.
func appIntegrationParamsWithinBounds(params map[string]any) bool {
	if len(params) == 0 {
		return true
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return false
	}
	return len(raw) <= appIntegrationMaxParamBytes
}

// resolveAppIntegrationConnection probes the platform connection the same way
// the action gate does and returns (connectionKey, connected). A missing
// provider config, a probe failure, or a non-connected state all return
// connected=false so the App renders a connect-state rather than an error.
func (b *Broker) resolveAppIntegrationConnection(ctx context.Context, composio *action.ComposioREST, platform string) (string, bool) {
	if composio == nil || !composio.Configured() {
		return "", false
	}
	status, err := composio.GetIntegrationConnectionStatus(ctx, action.IntegrationStatusRequest{
		Provider: "composio",
		Platform: platform,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "broker: apps integration probe failed for %s: %v\n", platform, err)
		return "", false
	}
	if action.MapConnectionState(status.Status) != action.StateConnected {
		return "", false
	}
	key := strings.TrimSpace(status.ConnectionKey)
	return key, key != ""
}

// raiseAppActionApproval mints a human approval card for a mutating
// integration action requested from an App, mirroring the agent gate's
// `kind:approval` request (structured integration_action payload, masked
// envelope). It returns the request id the App polls. The App NEVER executes
// the action; only a human approval can. The card is non-blocking so it does
// not freeze the channel — it is a state-changing action the human chooses to
// run from their tool. Best-effort: an empty id is returned only on a
// pathological lock-time failure, which the App surfaces as "could not raise".
func (b *Broker) raiseAppActionApproval(r *http.Request, platform, actionID string, params map[string]any) string {
	actor := appActionApprovalActor(r)
	card := b.buildAppActionCard(r.Context(), platform, actionID, params)

	b.mu.Lock()
	defer b.mu.Unlock()

	dedupeKey := appActionApprovalDedupeKey(platform, actionID)
	// Dedupe workspace-wide so an App that loops a mutating call does not stack
	// duplicate cards and train the human to reflexively approve.
	for i := range b.requests {
		if normalizeRequestKind(b.requests[i].Kind) != "approval" {
			continue
		}
		if strings.TrimSpace(b.requests[i].DedupeKey) != dedupeKey {
			continue
		}
		if requestIsActive(b.requests[i]) {
			return b.requests[i].ID
		}
	}

	display := action.DisplayPlatformName(platform)
	options, recommended := requestOptionDefaults("approval")
	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	req := humanInterview{
		ID:            fmt.Sprintf("request-%d", b.counter),
		Kind:          "approval",
		Status:        "pending",
		From:          actor,
		Channel:       "general",
		Title:         "App action: " + display,
		Question:      fmt.Sprintf("An app wants to run %s on %s. Approve?", actionID, display),
		Context:       "Requested by an internal tool you are using.",
		Options:       options,
		RecommendedID: recommended,
		// Non-blocking: a human chose to run this from their tool; it must not
		// freeze the channel. They approve at leisure; the App polls the id.
		Blocking:  false,
		Required:  false,
		Platform:  platform,
		LogoURL:   curatedToolkitLogo(platform),
		DedupeKey: dedupeKey,
		Action:    card,
		CreatedAt: now,
		UpdatedAt: now,
	}
	b.requests = append(b.requests, req)
	b.appendActionLocked("app_external_action_requested", "office", "general", actor,
		truncateSummary(req.Title, 140), req.ID)
	_ = b.saveLocked()
	return req.ID
}

// buildAppActionCard composes the structured approval payload for a mutating
// App integration action: the masked HTTP envelope (built via a dry-run
// execute, secrets masked by the SAME masker the agent gate uses) so the
// approval card's raw toggle shows exactly what would go over the wire.
func (b *Broker) buildAppActionCard(ctx context.Context, platform, actionID string, params map[string]any) *approvalActionPayload {
	card := &approvalActionPayload{
		Platform: platform,
		ActionID: actionID,
		Name:     action.DisplayPlatformName(platform),
		LogoURL:  curatedToolkitLogo(platform),
	}
	composio := action.NewComposioFromEnv()
	connKey, connected := b.resolveAppIntegrationConnection(ctx, composio, platform)
	if connected {
		if dry, err := composio.ExecuteAction(ctx, action.ExecuteRequest{
			Platform:      platform,
			ActionID:      actionID,
			ConnectionKey: connKey,
			Data:          params,
			DryRun:        true,
		}); err == nil {
			card.RawEnvelope = &approvalActionEnvelope{
				Method:  dry.Request.Method,
				URL:     dry.Request.URL,
				Headers: maskSensitivePayload(dry.Request.Headers),
				Data:    maskSensitivePayload(dry.Request.Data),
			}
		} else {
			fmt.Fprintf(os.Stderr, "broker: apps action dry-run preview failed for %s/%s: %v\n", platform, actionID, err)
		}
	}
	// The broker re-masks on store regardless; sanitize here too so the stored
	// card never carries an unmasked secret.
	return sanitizeApprovalActionPayload(card)
}

// appActionApprovalDedupeKey collapses an App's repeated mutating calls for the
// same (platform, action) onto one approval card.
func appActionApprovalDedupeKey(platform, actionID string) string {
	return fmt.Sprintf("app-action:%s:%s",
		strings.ToLower(strings.TrimSpace(platform)),
		strings.ToLower(strings.TrimSpace(actionID)),
	)
}

// appActionApprovalActor resolves who requested the action for the audit trail:
// the authenticated human session if present, else a generic "app" label. An
// App can never impersonate an agent here.
func appActionApprovalActor(r *http.Request) string {
	if a, ok := requestActorFromContext(r.Context()); ok && a.Kind == requestActorKindHuman {
		if slug := strings.TrimSpace(a.Slug); slug != "" {
			return slug
		}
		return "human"
	}
	return "app"
}

// ─────────────────────────── A. integration catalog ────────────────────────

// appIntegrationCatalogItem is the minimal shape an App sees: a connected
// platform plus its available READ actions, so the App (and the agent building
// it) knows what it can call without execute-time guesswork.
type appIntegrationCatalogItem struct {
	Platform    string   `json:"platform"`
	Name        string   `json:"name"`
	LogoURL     string   `json:"logo_url,omitempty"`
	ReadActions []string `json:"read_actions"`
}

type appIntegrationCatalogResponse struct {
	Connected []appIntegrationCatalogItem `json:"connected"`
}

// handleAppsIntegrationsCatalog serves GET /apps/integrations/catalog: the
// connected platforms + their available READ actions. It reuses the existing
// integrations catalog code (ListIntegrationCatalog, connected filter) so the
// App's view of "what can I call" stays in sync with the Integrations app.
func (b *Broker) handleAppsIntegrationsCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	composio := action.NewComposioFromEnv()
	resp := appIntegrationCatalogResponse{Connected: []appIntegrationCatalogItem{}}
	if !composio.Configured() {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	catalog, err := composio.ListIntegrationCatalog(r.Context(), action.IntegrationCatalogOptions{
		Connected: "true",
		Limit:     100,
	})
	if err != nil {
		// Degrade gracefully: an empty catalog renders a "nothing connected yet"
		// state in the App rather than an error toast.
		fmt.Fprintf(os.Stderr, "broker: apps integration catalog failed: %v\n", err)
		writeJSON(w, http.StatusOK, resp)
		return
	}
	for _, item := range catalog.Items {
		platform := strings.TrimSpace(item.Platform)
		if platform == "" {
			continue
		}
		reads := b.appReadActionsForPlatform(r.Context(), composio, platform)
		resp.Connected = append(resp.Connected, appIntegrationCatalogItem{
			Platform:    platform,
			Name:        firstNonEmpty(item.Name, action.DisplayPlatformName(platform)),
			LogoURL:     strings.TrimSpace(item.LogoURL),
			ReadActions: reads,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// appReadActionsForPlatform returns the READ-only action ids available for a
// platform, filtered through action.ActionIsReadOnly so the catalog advertises
// ONLY what an App can run without raising an approval card. Best-effort: a
// catalog/search failure yields an empty list (the App can still call known
// actions; the listing is a convenience, not a gate).
func (b *Broker) appReadActionsForPlatform(ctx context.Context, composio *action.ComposioREST, platform string) []string {
	search, err := composio.SearchActions(ctx, platform, "", "")
	if err != nil {
		return []string{}
	}
	out := make([]string, 0, len(search.Actions))
	for _, a := range search.Actions {
		id := strings.TrimSpace(a.ActionID)
		if id != "" && action.ActionIsReadOnly(id) {
			out = append(out, id)
		}
	}
	return out
}

// ─────────────────────────────── B. ai() ───────────────────────────────────

// appAIRequest is the App's ai() call: a prompt plus optional input data the
// App already fetched through this bridge, and whether to parse the result as
// JSON.
type appAIRequest struct {
	Prompt string `json:"prompt"`
	Input  any    `json:"input,omitempty"`
	JSON   bool   `json:"json,omitempty"`
	// AppID identifies the calling app so the ai() budget is per-app. Host-supplied
	// (the sealed iframe never sees it), so it is a trustworthy budget key.
	AppID string `json:"app_id,omitempty"`
}

// appAIResponse returns the model's text (and, when json:true, a parsed object).
type appAIResponse struct {
	Text   string          `json:"text,omitempty"`
	Object json.RawMessage `json:"object,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// handleAppsAI serves POST /apps/ai: a bounded ONE-SHOT completion over
// prompt + input using the workspace's own configured LLM provider. It is
// read-only reasoning over data the App already holds — never a tool-call loop
// and never a network escape hatch (the frame has connect-src 'none'). Bounds:
// prompt + input size caps and a time cap.
func (b *Broker) handleAppsAI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req appAIRequest
	if !decodeIntegrationRequest(w, r, &req) {
		return
	}
	// Budget before doing any work: a hostile App loops cheaply, but each
	// completion is expensive. Keyed PER-APP (not per human) and enforced on a
	// per-minute AND per-day window, so one app that re-runs ai() on every tab
	// refocus is bounded and cannot starve the workspace or burn the daily budget.
	if retryAfter, limited := b.consumeAppAIBudget(appBudgetKey(req.AppID, r)); limited {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt is required"})
		return
	}
	systemPrompt, userPrompt, ok := buildAppsAIPrompt(prompt, req.Input, req.JSON)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt or input too large"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), appAITimeout)
	defer cancel()
	out, err := currentAppsLLMCompleter()(ctx, systemPrompt, userPrompt)
	if err != nil {
		// A provider that is not configured / one-shot-capable surfaces a typed
		// "ai_unavailable" so the App can render a graceful fallback rather than
		// crash. HTTP 200: this is an expected product state, not a transport
		// error.
		fmt.Fprintf(os.Stderr, "broker: apps ai completion failed: %v\n", err)
		writeJSON(w, http.StatusOK, appAIResponse{Error: "ai_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, finalizeAppsAIResponse(out, req.JSON))
}

// appBudgetKey is the per-app budget key. It prefers the host-supplied app id
// (the sealed iframe cannot forge it, and only the host can reach these
// endpoints), so each app gets its own bucket; it falls back to the human
// actor/session when an older host omits the id. The "app:" / "actor:" prefixes
// keep the two namespaces from colliding.
func appBudgetKey(appID string, r *http.Request) string {
	// Validate the id SHAPE before trusting it as a map key: a host-supplied id is
	// trustworthy, but validating keeps a malformed/forged value from spraying
	// arbitrary keys into the budget maps. An invalid id falls back to the actor
	// bucket (more restrictive, never less).
	if id := strings.TrimSpace(appID); id != "" && validateCustomAppID(id) == nil {
		return "app:" + id
	}
	actor := strings.TrimSpace(appActionApprovalActor(r))
	if actor == "" {
		actor = "app"
	}
	return "actor:" + actor
}

// consumeAppAIBudget charges one ai() call against the per-app per-minute AND
// per-day buckets, reporting (retryAfter, limited). It peeks both limits first
// and records a hit on neither when either is exceeded, so a rejected call never
// pollutes a bucket. Caller must NOT hold b.mu.
func (b *Broker) consumeAppAIBudget(key string) (time.Duration, bool) {
	now := b.rateLimitNow()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maybeSweepAppBudgetsLocked(now)
	if d, limited := peekRollingLimit(b.appAIRateLimitBuckets, key, appAIRateLimit, appAIRateWindow, now); limited {
		return d, true
	}
	if d, limited := peekRollingLimit(b.appAIDailyBuckets, key, appAIDailyLimit, appAIDailyWindow, now); limited {
		return d, true
	}
	recordRollingHit(b.appAIRateLimitBuckets, key, now)
	recordRollingHit(b.appAIDailyBuckets, key, now)
	return 0, false
}

// consumeAppIntegrationReadBudget charges one integration READ against the
// per-app per-minute AND per-day buckets. Same peek-both-then-record discipline
// as consumeAppAIBudget. Caller must NOT hold b.mu.
func (b *Broker) consumeAppIntegrationReadBudget(key string) (time.Duration, bool) {
	now := b.rateLimitNow()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maybeSweepAppBudgetsLocked(now)
	if d, limited := peekRollingLimit(b.appIntegrationReadBuckets, key, appIntegrationReadLimit, appIntegrationReadWindow, now); limited {
		return d, true
	}
	if d, limited := peekRollingLimit(b.appIntegrationReadDayBuckets, key, appIntegrationReadDailyLimit, appIntegrationReadDailyWindow, now); limited {
		return d, true
	}
	recordRollingHit(b.appIntegrationReadBuckets, key, now)
	recordRollingHit(b.appIntegrationReadDayBuckets, key, now)
	return 0, false
}

// consumeAppDBWriteBudget charges one DB write op against the per-app
// per-minute bucket. Same peek-then-record discipline as consumeAppAIBudget.
// Caller must NOT hold b.mu.
func (b *Broker) consumeAppDBWriteBudget(key string) (time.Duration, bool) {
	now := b.rateLimitNow()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maybeSweepAppBudgetsLocked(now)
	if d, limited := peekRollingLimit(b.appDBWriteBuckets, key, appDBWriteLimit, appDBWriteWindow, now); limited {
		return d, true
	}
	recordRollingHit(b.appDBWriteBuckets, key, now)
	return 0, false
}

// peekRollingLimit prunes the bucket to the window and reports whether it is at
// or over the limit WITHOUT recording a new hit (so a rejected multi-limit call
// pollutes no bucket). It writes the pruned bucket back. Caller holds b.mu and
// the map is non-nil (initialized in NewBroker).
func peekRollingLimit(buckets map[string]ipRateLimitBucket, key string, limit int, window time.Duration, now time.Time) (time.Duration, bool) {
	bucket := buckets[key]
	bucket.timestamps = pruneRateLimitEntries(bucket.timestamps, now.Add(-window))
	// Self-sweep: an empty bucket is dropped rather than resurrected, so a key
	// that goes idle does not linger in the map forever.
	if len(bucket.timestamps) == 0 {
		delete(buckets, key)
		return 0, false
	}
	buckets[key] = bucket
	if len(bucket.timestamps) >= limit {
		retryAfter := bucket.timestamps[0].Add(window).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return retryAfter, true
	}
	return 0, false
}

// recordRollingHit appends one hit at now. Caller holds b.mu; the bucket was just
// pruned by peekRollingLimit.
func recordRollingHit(buckets map[string]ipRateLimitBucket, key string, now time.Time) {
	bucket := buckets[key]
	bucket.timestamps = append(bucket.timestamps, now)
	buckets[key] = bucket
}

// appBudgetSweepInterval is how often maybeSweepAppBudgetsLocked walks the budget
// maps to evict keys that have gone fully idle. The per-peek self-sweep already
// drops a key when ITS owner calls again; this catches keys whose owner never
// returns (e.g. a one-off id), bounding total memory to "apps active recently".
const appBudgetSweepInterval = time.Hour

// maybeSweepAppBudgetsLocked periodically evicts fully-expired keys from all the
// app-budget maps, each pruned by its own window. Throttled to once per
// appBudgetSweepInterval so it adds negligible cost to the hot path. Caller holds
// b.mu.
func (b *Broker) maybeSweepAppBudgetsLocked(now time.Time) {
	if !b.lastAppBudgetPrune.IsZero() && now.Sub(b.lastAppBudgetPrune) < appBudgetSweepInterval {
		return
	}
	b.lastAppBudgetPrune = now
	specs := []struct {
		m      map[string]ipRateLimitBucket
		window time.Duration
	}{
		{b.appAIRateLimitBuckets, appAIRateWindow},
		{b.appAIDailyBuckets, appAIDailyWindow},
		{b.appIntegrationReadBuckets, appIntegrationReadWindow},
		{b.appIntegrationReadDayBuckets, appIntegrationReadDailyWindow},
		{b.appDBWriteBuckets, appDBWriteWindow},
	}
	for _, s := range specs {
		cutoff := now.Add(-s.window)
		for k, bucket := range s.m {
			bucket.timestamps = pruneRateLimitEntries(bucket.timestamps, cutoff)
			if len(bucket.timestamps) == 0 {
				delete(s.m, k)
			} else {
				s.m[k] = bucket
			}
		}
	}
}

// buildAppsAIPrompt composes the system + user prompt for an ai() call and
// enforces the size bounds. Returns ok=false when prompt or serialized input
// exceeds the cap. The system prompt pins the model to pure reasoning over the
// supplied data (no tools, no external lookups) and, when json:true, to a
// single JSON value.
func buildAppsAIPrompt(prompt string, input any, wantJSON bool) (systemPrompt, userPrompt string, ok bool) {
	if len(prompt) > appAIMaxPromptBytes {
		return "", "", false
	}
	var b strings.Builder
	b.WriteString(prompt)
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return "", "", false
		}
		if len(raw) > appAIMaxInputBytes {
			return "", "", false
		}
		b.WriteString("\n\nInput data:\n")
		b.Write(raw)
	}
	system := "You are a reasoning helper embedded in an internal tool. Answer ONLY from the prompt and the supplied input data. Do not invent external facts, do not call tools, and do not request more data."
	if wantJSON {
		system += " Respond with a SINGLE valid JSON value and nothing else — no prose, no markdown fences."
	}
	return system, b.String(), true
}

// finalizeAppsAIResponse caps the output and, when json was requested, extracts
// and validates a single JSON value. A non-JSON answer under json:true is
// returned as raw text with an error marker so the App can decide how to handle
// it rather than receiving a silent empty object.
func finalizeAppsAIResponse(out string, wantJSON bool) appAIResponse {
	text := strings.TrimSpace(out)
	if len(text) > appAIMaxOutputChars {
		text = text[:appAIMaxOutputChars]
	}
	if !wantJSON {
		return appAIResponse{Text: text}
	}
	if obj, ok := extractFirstJSON(text); ok {
		return appAIResponse{Object: obj}
	}
	// Asked for JSON, got prose: surface the text + a marker rather than
	// silently swallowing it.
	return appAIResponse{Text: text, Error: "not_json"}
}

// extractFirstJSON finds and validates the first complete JSON object or array
// in s, tolerating leading/trailing prose or markdown fences a model might add.
// Returns the compacted JSON bytes and ok=true on success.
func extractFirstJSON(s string) (json.RawMessage, bool) {
	trimmed := strings.TrimSpace(s)
	// Fast path: the whole string is valid JSON.
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed), true
	}
	// Otherwise scan for the first balanced {...} or [...] span and validate it.
	for _, open := range []byte{'{', '['} {
		close := byte('}')
		if open == '[' {
			close = ']'
		}
		start := strings.IndexByte(trimmed, open)
		if start < 0 {
			continue
		}
		depth := 0
		inStr := false
		esc := false
		for i := start; i < len(trimmed); i++ {
			c := trimmed[i]
			switch {
			case esc:
				esc = false
			case c == '\\' && inStr:
				esc = true
			case c == '"':
				inStr = !inStr
			case inStr:
				// inside a string literal: ignore structural bytes
			case c == open:
				depth++
			case c == close:
				depth--
				if depth == 0 {
					candidate := trimmed[start : i+1]
					if json.Valid([]byte(candidate)) {
						return json.RawMessage(candidate), true
					}
				}
			}
		}
	}
	return nil, false
}
