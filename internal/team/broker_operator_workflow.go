package team

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/provider"
)

// broker_operator_workflow.go — the app's DETERMINISTIC workflow: compile once,
// freeze, run the same frozen plan every time.
//
// The operator promise is that building an app makes its automation
// deterministic. Determinism is a COMPILE-TIME property: we derive a semantic
// plan from the app's real, source-derived capabilities (introspectAppSource),
// bind it ONCE into a runnable Composio workflow definition (BindWorkflowPlan),
// and persist that frozen definition (CreateWorkflow -> saveWorkflowDefinition).
// Every run then loads and executes the SAME saved definition (ExecuteWorkflow)
// with no re-binding and no LLM in the loop — identical steps every time. The
// LLM only ever runs at the seams (the one-time bind, and any explicit ai step).
//
// Routes (all under the operator's authenticated session):
//   GET  /operator/apps/{id}/workflow          -> the frozen workflow (or none)
//   POST /operator/apps/{id}/workflow/compile  -> derive + bind + freeze
//   POST /operator/apps/{id}/workflow/run      -> run the frozen plan (dry by default)

// workflowReader is the read side of compile-and-freeze. Kept narrow (not on the
// Provider interface) so only the Composio provider, which persists definitions,
// needs to implement it.
type workflowReader interface {
	GetWorkflow(ctx context.Context, keyOrPath string) (action.WorkflowGetResult, error)
}

// operatorAppWorkflowKey is the stable storage key for an app's frozen workflow.
func operatorAppWorkflowKey(appID string) string {
	base := strings.TrimPrefix(strings.TrimSpace(appID), "app_")
	slug := normalizeChannelSlug(base)
	if slug == "" {
		slug = "app"
	}
	return "operator-app-" + slug
}

// handleOperatorAppWorkflow routes the app-scoped workflow operations.
func (b *Broker) handleOperatorAppWorkflow(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/operator/apps/")
	appID, tail, _ := strings.Cut(rest, "/")
	appID = strings.TrimSpace(appID)
	if appID == "" {
		http.Error(w, "app id is required", http.StatusBadRequest)
		return
	}
	switch tail {
	case "workflow":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		b.getOperatorAppWorkflow(w, r, appID)
	case "workflow/compile":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		b.compileOperatorAppWorkflow(w, r, appID)
	case "workflow/run":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		b.runOperatorAppWorkflow(w, r, appID)
	case "workflow/connections":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		b.getOperatorAppWorkflowConnections(w, r, appID)
	case "workflow/browser/pending":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		b.getOperatorAppBrowserPending(w, r, appID)
	case "workflow/browser/approve":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		b.resolveOperatorAppBrowserApproval(w, r, appID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// getOperatorAppWorkflow returns the app's frozen workflow steps + recent runs,
// or { compiled: false } when nothing has been compiled yet.
func (b *Broker) getOperatorAppWorkflow(w http.ResponseWriter, r *http.Request, appID string) {
	prov, ok := b.operatorWorkflowProvider(w)
	if !ok {
		return
	}
	reader, ok := prov.(workflowReader)
	if !ok {
		http.Error(w, "workflow provider cannot read definitions", http.StatusBadGateway)
		return
	}
	key := operatorAppWorkflowKey(appID)
	got, err := reader.GetWorkflow(r.Context(), key)
	if err != nil {
		http.Error(w, "read workflow: "+err.Error(), http.StatusBadGateway)
		return
	}
	out := map[string]any{
		"compiled":     got.Exists,
		"workflow_key": key,
	}
	if got.Exists {
		out["title"] = got.Title
		out["steps"] = got.Steps
		if runs, err := prov.ListWorkflowRuns(r.Context(), key); err == nil {
			out["runs"] = runs.Runs
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// getOperatorAppWorkflowConnections lists, for each external platform the frozen
// workflow calls, the operator's active connections — so the UI can show a
// chooser when a platform has more than one account. Empty when the workflow has
// no external action steps.
func (b *Broker) getOperatorAppWorkflowConnections(w http.ResponseWriter, r *http.Request, appID string) {
	prov, ok := b.operatorWorkflowProvider(w)
	if !ok {
		return
	}
	reader, ok := prov.(workflowReader)
	if !ok {
		http.Error(w, "workflow provider cannot read definitions", http.StatusBadGateway)
		return
	}
	key := operatorAppWorkflowKey(appID)
	got, err := reader.GetWorkflow(r.Context(), key)
	if err != nil {
		http.Error(w, "read workflow: "+err.Error(), http.StatusBadGateway)
		return
	}

	type connView struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	platforms := []map[string]any{}
	seen := map[string]bool{}
	for _, step := range got.Steps {
		platform := strings.TrimSpace(step.Platform)
		if platform == "" || seen[platform] {
			continue
		}
		seen[platform] = true
		conns := []connView{}
		if res, err := prov.ListConnections(r.Context(), action.ListConnectionsOptions{Search: platform, Limit: 50}); err == nil {
			for _, c := range res.Connections {
				if !strings.EqualFold(strings.TrimSpace(c.Platform), platform) {
					continue
				}
				switch strings.ToLower(strings.TrimSpace(c.State)) {
				case "", "active", "operational", "connected":
					conns = append(conns, connView{Key: strings.TrimSpace(c.Key), Name: strings.TrimSpace(c.Name)})
				}
			}
		}
		platforms = append(platforms, map[string]any{
			"platform":    platform,
			"connections": conns,
			"multiple":    len(conns) > 1,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"platforms": platforms})
}

// compileOperatorAppWorkflow derives the app's semantic plan from its real
// capabilities, binds it once, and freezes it. This is where the determinism is
// minted: after this returns, every run executes the same saved definition.
func (b *Broker) compileOperatorAppWorkflow(w http.ResponseWriter, r *http.Request, appID string) {
	app, _, err := b.appStore().Get(appID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	source, err := b.appStore().Source(appID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	caps := introspectAppSource(source)
	if len(planFromAppCapabilities(app, caps).Steps) <= 1 {
		// Nothing the app reads or writes, so there is no automation to compile.
		http.Error(w, "this app does not read or write anything yet, so it has no workflow to compile", http.StatusBadRequest)
		return
	}
	// Author the plan from the app's actual PURPOSE (its description), tailored to
	// this app — not a generic capability list — so different apps get different
	// workflows. Falls back to the deterministic capability-derived plan when the
	// model is unavailable or its output is unusable.
	plan := b.authoredAppWorkflowPlan(r.Context(), app, caps)

	prov, ok := b.operatorWorkflowProvider(w)
	if !ok {
		return
	}
	// Bind against the REAL engine (Composio catalog search + LLM), not the stub:
	// the Workflow tab's frozen plan is genuine runnable actions. The resolver
	// fails safe per step, so an offline catalog/model degrades a single step to
	// narration rather than breaking the compile.
	def, err := action.BindWorkflowPlan(r.Context(), plan, operatorComposioResolver(prov))
	if err != nil {
		http.Error(w, "compile workflow: "+err.Error(), http.StatusBadRequest)
		return
	}
	raw, err := json.Marshal(def)
	if err != nil {
		http.Error(w, "marshal workflow definition", http.StatusInternalServerError)
		return
	}
	key := operatorAppWorkflowKey(appID)
	if !prov.Supports(action.CapabilityWorkflowCreate) {
		http.Error(w, "workflow provider cannot persist definitions", http.StatusBadGateway)
		return
	}
	if _, err := prov.CreateWorkflow(r.Context(), action.WorkflowCreateRequest{Key: key, Definition: raw}); err != nil {
		http.Error(w, "freeze workflow: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Read the frozen steps back so the response is exactly what later runs see.
	reader, ok := prov.(workflowReader)
	if !ok {
		http.Error(w, "workflow provider cannot read definitions", http.StatusBadGateway)
		return
	}
	got, err := reader.GetWorkflow(r.Context(), key)
	if err != nil {
		http.Error(w, "read frozen workflow: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"compiled":     true,
		"workflow_key": key,
		"title":        got.Title,
		"steps":        got.Steps,
	})
}

// runOperatorAppWorkflow executes the app's FROZEN workflow — it loads the saved
// definition and runs it, with no re-binding, so the run is deterministic. Dry
// run by default: the operator previews exactly what the frozen plan does before
// anything mutates an external system.
func (b *Broker) runOperatorAppWorkflow(w http.ResponseWriter, r *http.Request, appID string) {
	r.Body = http.MaxBytesReader(w, r.Body, studioRequestMaxBodyBytes)
	defer r.Body.Close()
	var body struct {
		Inputs map[string]any `json:"inputs"`
		DryRun *bool          `json:"dry_run"`
		// Connections is the operator's per-platform connection choice
		// (platform -> connection_key), surfaced by the UI's connection chooser
		// to disambiguate a platform that has multiple active accounts.
		Connections map[string]string `json:"connections"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
	}
	inputs := body.Inputs
	if len(body.Connections) > 0 {
		if inputs == nil {
			inputs = map[string]any{}
		}
		chosen := make(map[string]any, len(body.Connections))
		for platform, key := range body.Connections {
			chosen[platform] = key
		}
		inputs["connections"] = chosen
	}

	prov, ok := b.operatorWorkflowProvider(w)
	if !ok {
		return
	}
	key := operatorAppWorkflowKey(appID)
	// Confirm the workflow has been frozen before trying to run it, so a missing
	// definition is a clear 409 ("compile it first"), not an opaque load error.
	if reader, ok := prov.(workflowReader); ok {
		got, err := reader.GetWorkflow(r.Context(), key)
		if err != nil {
			http.Error(w, "read workflow: "+err.Error(), http.StatusBadGateway)
			return
		}
		if !got.Exists {
			http.Error(w, "this app's workflow has not been compiled yet", http.StatusConflict)
			return
		}
	}

	dryRun := true
	if body.DryRun != nil {
		dryRun = *body.DryRun
	}
	// Carry the app id so a `browser` step knows which app's chat to ask for
	// browser-control + send approval (slice 3b). A live (non-dry) run may PAUSE
	// on that ask; the request stays open until the operator replies in chat.
	ctx := context.WithValue(r.Context(), browserStepAppIDKey, appID)
	execution, err := prov.ExecuteWorkflow(ctx, action.WorkflowExecuteRequest{
		KeyOrPath: key,
		Inputs:    inputs,
		DryRun:    dryRun,
	})
	if err != nil {
		http.Error(w, "run workflow: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"workflow_key": key,
		"dry_run":      dryRun,
		"run_id":       execution.RunID,
		"status":       execution.Status,
		"steps":        execution.Steps,
	})
}

// operatorWorkflowProvider resolves the Composio workflow provider, writing the
// error response itself when unavailable. Returns ok=false when the caller
// should stop (the response is already written).
func (b *Broker) operatorWorkflowProvider(w http.ResponseWriter) (action.Provider, bool) {
	registry := action.NewRegistryFromEnv()
	prov, err := registry.ProviderNamed("composio", action.CapabilityWorkflowExecute)
	if err != nil {
		http.Error(w, "composio workflow provider unavailable: "+err.Error(), http.StatusBadGateway)
		return nil, false
	}
	return prov, true
}

// operatorWorkflowProviderOrNil is the context-only twin of
// operatorWorkflowProvider: it returns the Composio workflow provider, or nil
// when unavailable, so headless callers (the publish-time precompile) fail safe
// instead of writing an HTTP error.
func (b *Broker) operatorWorkflowProviderOrNil() action.Provider {
	prov, err := action.NewRegistryFromEnv().ProviderNamed("composio", action.CapabilityWorkflowExecute)
	if err != nil {
		return nil
	}
	return prov
}

// compileAndFreezeAppWorkflow is the headless twin of compileOperatorAppWorkflow
// (the on-demand HTTP path): it derives the app's deterministic workflow from
// its real capabilities, authors the plan from the app's purpose, binds it
// against the real Composio catalog, and freezes it under operatorAppWorkflowKey.
// The broker calls it the moment an app publishes so the workflow is figured out
// AS PART OF the build — the Workflow tab opens already-compiled rather than
// compiling on demand while the operator watches. Fail-safe: returns an error
// (logged and dropped by the async wrapper) on no-provider / nothing-to-automate
// / offline model, and the on-demand compile path stays as the fallback.
func (b *Broker) compileAndFreezeAppWorkflow(ctx context.Context, appID string) error {
	app, _, err := b.appStore().Get(appID)
	if err != nil {
		return err
	}
	source, err := b.appStore().Source(appID)
	if err != nil {
		return err
	}
	caps := introspectAppSource(source)
	if len(planFromAppCapabilities(app, caps).Steps) <= 1 {
		return nil // nothing the app reads or writes — no automation to compile
	}
	plan := b.authoredAppWorkflowPlan(ctx, app, caps)
	prov := b.operatorWorkflowProviderOrNil()
	if prov == nil {
		return fmt.Errorf("composio workflow provider unavailable")
	}
	def, err := action.BindWorkflowPlan(ctx, plan, operatorComposioResolver(prov))
	if err != nil {
		return err
	}
	raw, err := json.Marshal(def)
	if err != nil {
		return err
	}
	if !prov.Supports(action.CapabilityWorkflowCreate) {
		return fmt.Errorf("workflow provider cannot persist definitions")
	}
	if _, err := prov.CreateWorkflow(ctx, action.WorkflowCreateRequest{Key: operatorAppWorkflowKey(appID), Definition: raw}); err != nil {
		return err
	}
	return nil
}

// precompileAppWorkflowAsync runs compileAndFreezeAppWorkflow off the publish
// request path so a freshly-published app already has its frozen workflow by the
// time the operator opens the Workflow tab.
func (b *Broker) precompileAppWorkflowAsync(appID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := b.compileAndFreezeAppWorkflow(ctx, appID); err != nil {
		log.Printf("operator workflow: precompile for %s skipped: %v", appID, err)
	}
}

const workflowAuthorSystemPrompt = `You design a DETERMINISTIC automation that runs an internal app on a schedule.
Return ONLY a JSON object, no prose: {"steps":[{"id","kind","title","detail","integration","gated"}]}.
Rules:
- kind is one of: trigger, enrich, ai, decision, action, branch, browser.
- The FIRST step is exactly one "trigger" (the schedule).
- "enrich" reads data; "ai" summarizes/analyzes; "action" sends or writes to an external system; "decision"/"branch" gate on a condition.
- integration is a lowercase platform slug (e.g. "gmail", "slack") ONLY for a step that calls that external system, else "".
- gated is true for any step that SENDS or WRITES to an external system.
- Use ONLY the data sources and integrations the app actually has (listed below). Do NOT invent capabilities.
- kind "browser" — for a step that must use an external system the app has NO integration for: set integration to "" and put the exact goal in "detail". Nex drives the browser to do it. Set gated true if it sends/writes.
- Tailor the steps to THIS app's specific purpose. Keep it tight: 3 to 7 steps.`

// authoredAppWorkflowPlan asks the model to design a workflow for THIS app from
// its purpose + real capabilities, so two apps with overlapping capabilities
// still get distinct, intent-specific workflows. It fails safe to the
// deterministic capability-derived plan when the model is unavailable or returns
// something unusable, and clamps any integration the model names to the app's
// real capability set so a hallucinated integration can never reach a run.
func (b *Broker) authoredAppWorkflowPlan(ctx context.Context, app CustomApp, caps AppCapabilities) action.Plan {
	fallback := planFromAppCapabilities(app, caps)
	purpose := strings.TrimSpace(app.Description)
	if purpose == "" {
		purpose = strings.TrimSpace(app.Summary)
	}
	if purpose == "" {
		// No stated purpose to tailor to — the capability-derived plan is as good
		// as it gets, and it avoids a needless model call.
		return fallback
	}
	user := fmt.Sprintf("App: %s\nPurpose: %s\n\n%s", app.Name, purpose, renderAppCapabilities(caps))
	out, err := provider.RunConfiguredOneShotCtx(ctx, workflowAuthorSystemPrompt, user, "")
	if err != nil {
		return fallback
	}
	plan, ok := parseAuthoredWorkflowPlan(out, app, caps)
	if !ok {
		return fallback
	}
	return plan
}

// parseAuthoredWorkflowPlan validates the model's JSON plan into a runnable
// semantic plan. Returns ok=false on any problem so the caller uses its safe
// fallback. Integrations are clamped to the app's real platforms.
func parseAuthoredWorkflowPlan(raw string, app CustomApp, caps AppCapabilities) (action.Plan, bool) {
	obj := extractFirstJSONObject(raw)
	if obj == "" {
		return action.Plan{}, false
	}
	var parsed struct {
		Steps []struct {
			ID          string `json:"id"`
			Kind        string `json:"kind"`
			Title       string `json:"title"`
			Detail      string `json:"detail"`
			Integration string `json:"integration"`
			Gated       bool   `json:"gated"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(obj), &parsed); err != nil {
		return action.Plan{}, false
	}
	if len(parsed.Steps) == 0 {
		return action.Plan{}, false
	}

	known := map[string]bool{}
	for _, it := range caps.Integrations {
		known[strings.ToLower(strings.TrimSpace(it.Platform))] = true
	}
	for _, api := range caps.BridgeAPIs {
		if api == "getEmails" {
			known["gmail"] = true
		}
	}

	steps := make([]action.PlanStep, 0, len(parsed.Steps)+1)
	for i, s := range parsed.Steps {
		id := strings.TrimSpace(s.ID)
		if id == "" {
			id = fmt.Sprintf("s%d", i)
		}
		integ := strings.ToLower(strings.TrimSpace(s.Integration))
		kind := normalizeAuthoredKind(s.Kind)
		// No integration for what this step needs → drive the browser instead of
		// pretending the API exists ("no integration available → browser step").
		// Only convert steps that actually touch an external system (action/send);
		// a stray integration on a read becomes a plain narration as before.
		if integ != "" && !known[integ] {
			if kind == "action" || s.Gated {
				kind = "browser"
			}
			integ = ""
		}
		steps = append(steps, action.PlanStep{
			ID:          id,
			Kind:        kind,
			Title:       strings.TrimSpace(s.Title),
			Detail:      strings.TrimSpace(s.Detail),
			Integration: integ,
			Gated:       s.Gated,
		})
	}
	// Guarantee a leading trigger so binding always has a schedule entry to drop.
	if steps[0].Kind != "trigger" {
		steps = append([]action.PlanStep{{
			ID: "trigger", Kind: "trigger", Title: "On a schedule",
			Detail: "Runs on the cadence you set; nothing else triggers it.",
		}}, steps...)
	}
	if len(steps) <= 1 {
		return action.Plan{}, false
	}
	return action.Plan{Name: app.Name, ToolID: app.ID, Steps: steps}, true
}

func normalizeAuthoredKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "trigger", "enrich", "ai", "decision", "action", "branch", "browser":
		return strings.ToLower(strings.TrimSpace(raw))
	case "read", "fetch", "load":
		return "enrich"
	case "send", "write", "post", "create":
		return "action"
	default:
		return "enrich"
	}
}

// extractFirstJSONObject returns the outermost {...} block, tolerating code
// fences or prose the model may wrap around it.
func extractFirstJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return s[start : end+1]
}

// planFromAppCapabilities derives a semantic workflow plan from an app's
// deterministic, source-derived capabilities: trigger, then the reads (email,
// tasks, integration read actions), then an AI step if the app summarizes, then
// the gated writes (integration sends, task creation). The same capabilities
// always produce the same plan, so the compile is itself reproducible.
func planFromAppCapabilities(app CustomApp, caps AppCapabilities) action.Plan {
	steps := []action.PlanStep{{
		ID:     "trigger",
		Kind:   "trigger",
		Title:  "On a schedule",
		Detail: "Runs on the cadence you set; nothing else triggers it.",
	}}
	n := 0
	add := func(kind, title, detail, integration string, gated bool) {
		n++
		steps = append(steps, action.PlanStep{
			ID:          fmt.Sprintf("s%d", n),
			Kind:        kind,
			Title:       title,
			Detail:      detail,
			Integration: integration,
			Gated:       gated,
		})
	}

	hasBridge := func(name string) bool {
		for _, b := range caps.BridgeAPIs {
			if b == name {
				return true
			}
		}
		return false
	}

	// Reads first — bridge reads, then integration read actions.
	if hasBridge("getEmails") {
		add("enrich", "Read recent email", "Fetch recent messages, read-only.", "gmail", false)
	}
	if hasBridge("getTasks") {
		add("enrich", "Read the team's tasks", "Load the workspace's current tasks.", "", false)
	}
	if hasBridge("getOfficeMembers") {
		add("enrich", "Read the team roster", "Load the workspace members, read-only.", "", false)
	}

	// Collect integration writes to place AFTER the AI step; emit reads inline.
	type writeUse struct{ platform, action string }
	var writes []writeUse
	integrations := append([]AppIntegrationUsage(nil), caps.Integrations...)
	sort.Slice(integrations, func(i, j int) bool { return integrations[i].Platform < integrations[j].Platform })
	emailRead := hasBridge("getEmails")
	for _, it := range integrations {
		for _, act := range it.Actions {
			if actionIDLikelyWrites(act) {
				writes = append(writes, writeUse{it.Platform, act})
				continue
			}
			// getEmails already surfaced the gmail fetch as "Read recent email";
			// don't list the same read twice.
			if emailRead && it.Platform == "gmail" && act == "GMAIL_FETCH_EMAILS" {
				continue
			}
			add("enrich", humanizeAction(it.Platform, act), "Read from "+it.Platform+", read-only.", it.Platform, false)
		}
	}

	// The AI step, if the app summarizes with ai().
	if hasBridge("ai") {
		add("ai", "Summarize with AI", "Run the workspace AI over what was read.", "", false)
	}

	// Gated writes last — each is held for human approval before it sends.
	for _, wu := range writes {
		add("action", humanizeAction(wu.platform, wu.action), "Send via "+wu.platform+" — held for your approval before it goes out.", wu.platform, true)
	}
	for _, ow := range caps.OfficeWrites {
		if ow == "createTask" {
			add("action", "Create a task", "Create a workspace task — you confirm before it is created.", "", true)
		}
	}

	return action.Plan{Name: app.Name, ToolID: app.ID, Steps: steps}
}

// actionIDLikelyWrites mirrors the action package's write-marker heuristic so
// the plan derivation can place an integration action as a gated write vs a
// read. Kept local because the action-package helper is unexported.
func actionIDLikelyWrites(actionID string) bool {
	upper := strings.ToUpper(strings.TrimSpace(actionID))
	for _, marker := range []string{
		"SEND", "CREATE", "UPDATE", "DELETE", "PATCH",
		"UPSERT", "POST", "INSERT", "UPLOAD", "COMPLETE",
	} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

// humanizeAction turns a Composio action id into a short, readable step title,
// e.g. ("slack", "SLACK_SENDS_A_MESSAGE_TO_A_SLACK_CHANNEL") -> "Slack: sends a
// message to a slack channel".
func humanizeAction(platform, actionID string) string {
	words := strings.ToLower(strings.TrimSpace(actionID))
	words = strings.ReplaceAll(words, "_", " ")
	// Drop a leading platform prefix so it does not read "Slack: slack ...".
	plat := strings.ToLower(strings.TrimSpace(platform))
	words = strings.TrimSpace(strings.TrimPrefix(words, plat+" "))
	label := strings.TrimSpace(platform)
	if label == "" {
		if words == "" {
			return "Integration action"
		}
		return strings.ToUpper(words[:1]) + words[1:]
	}
	label = strings.ToUpper(label[:1]) + label[1:]
	if words == "" {
		return label
	}
	return label + ": " + words
}
