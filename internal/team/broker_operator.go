package team

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/provider"
)

// handleOperatorRunPlan closes the operator build->run loop: it takes the
// semantic plan the builder produced, binds it to a runnable Composio workflow,
// and runs it (dry run by default). Binding uses the stub resolver for now —
// deterministic and network-free — so the FE loop is clickable before the real
// SearchActions+LLM resolver lands; swapping the resolver does not change this
// wiring. Execution reuses the existing Composio workflow executor.
func (b *Broker) handleOperatorRunPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, studioRequestMaxBodyBytes)
	defer r.Body.Close()

	var body struct {
		SchemaVersion int            `json:"schema_version"`
		Plan          action.Plan    `json:"plan"`
		Inputs        map[string]any `json:"inputs"`
		DryRun        *bool          `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(body.Plan.Steps) == 0 {
		http.Error(w, "plan has no steps", http.StatusBadRequest)
		return
	}

	registry := action.NewRegistryFromEnv()
	prov, err := registry.ProviderNamed("composio", action.CapabilityWorkflowExecute)
	if err != nil {
		http.Error(w, "composio workflow provider unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}

	def, err := action.BindWorkflowPlan(r.Context(), body.Plan, operatorResolver(prov))
	if err != nil {
		http.Error(w, "bind plan: "+err.Error(), http.StatusBadRequest)
		return
	}
	raw, err := json.Marshal(def)
	if err != nil {
		http.Error(w, "marshal workflow definition", http.StatusInternalServerError)
		return
	}

	key := operatorWorkflowKey(body.Plan)
	if prov.Supports(action.CapabilityWorkflowCreate) {
		if _, err := prov.CreateWorkflow(r.Context(), action.WorkflowCreateRequest{Key: key, Definition: raw}); err != nil {
			http.Error(w, "create workflow: "+err.Error(), http.StatusBadGateway)
			return
		}
	}

	// Default to a dry run: the operator previews the workflow before anything
	// mutates an external system. A real run is an explicit opt-in.
	dryRun := true
	if body.DryRun != nil {
		dryRun = *body.DryRun
	}
	execution, err := prov.ExecuteWorkflow(r.Context(), action.WorkflowExecuteRequest{
		KeyOrPath: key,
		Inputs:    body.Inputs,
		DryRun:    dryRun,
	})
	if err != nil {
		http.Error(w, "run workflow: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":           true,
		"workflow_key": key,
		"dry_run":      dryRun,
		"run_id":       execution.RunID,
		"status":       execution.Status,
		"steps":        execution.Steps,
	})
}

// operatorResolver picks how a plan is bound to Composio mechanics. The default
// is the safe, offline stub (narrates steps as templates). Setting
// WUPHF_OPERATOR_REAL_BINDING=1 switches on the real resolver — it searches the
// Composio catalog and asks the configured model to pick the action_id, map
// params, and author run_if. The real resolver itself fails safe per step, so
// flipping the flag never breaks a build; the flag just gates live binding until
// it has been validated against real integrations.
func operatorResolver(prov action.Provider) action.WorkflowActionResolver {
	if os.Getenv("WUPHF_OPERATOR_REAL_BINDING") != "1" {
		return action.NewStubWorkflowResolver()
	}
	composio, ok := prov.(*action.ComposioREST)
	if !ok {
		return action.NewStubWorkflowResolver()
	}
	search := func(ctx context.Context, platform, query string) ([]action.Action, error) {
		res, err := composio.SearchActions(ctx, platform, query, "")
		if err != nil {
			return nil, err
		}
		return res.Actions, nil
	}
	llm := func(ctx context.Context, system, user string) (string, error) {
		return provider.RunConfiguredOneShotCtx(ctx, system, user, "")
	}
	return action.NewComposioActionResolver(search, llm)
}

// operatorWorkflowKey derives a stable storage key for a built plan.
func operatorWorkflowKey(plan action.Plan) string {
	base := strings.TrimSpace(plan.ToolID)
	if base == "" {
		base = strings.TrimSpace(plan.Name)
	}
	slug := normalizeChannelSlug(base)
	if slug == "" {
		slug = "plan"
	}
	return "operator-" + slug
}
