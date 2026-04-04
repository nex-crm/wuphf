package action

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/workflow"
)

const composioWorkflowVersion = "wuphf_workflow_v1"

type composioWorkflowDefinition struct {
	Version     string         `json:"version,omitempty"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Inputs      map[string]any `json:"inputs,omitempty"`
	Steps       []workflowStep `json:"steps"`
}

type workflowStep struct {
	ID              string         `json:"id"`
	Type            string         `json:"type"`
	Description     string         `json:"description,omitempty"`
	Template        string         `json:"template,omitempty"`
	Platform        string         `json:"platform,omitempty"`
	ActionID        string         `json:"action_id,omitempty"`
	ConnectionKey   any            `json:"connection_key,omitempty"`
	Data            map[string]any `json:"data,omitempty"`
	Params          map[string]any `json:"params,omitempty"`
	PathVariables   map[string]any `json:"path_variables,omitempty"`
	QueryParameters map[string]any `json:"query_parameters,omitempty"`
	Headers         map[string]any `json:"headers,omitempty"`
	FormData        bool           `json:"form_data,omitempty"`
	FormURLEncoded  bool           `json:"form_url_encoded,omitempty"`
	DryRun          *bool          `json:"dry_run,omitempty"`
	QueryTemplate   string         `json:"query_template,omitempty"`
	LookbackHours   any            `json:"lookback_hours,omitempty"`
	InsightLimit    any            `json:"insight_limit,omitempty"`
}

type workflowRunRecord struct {
	Provider    string                     `json:"provider"`
	WorkflowKey string                     `json:"workflow_key"`
	RunID       string                     `json:"run_id"`
	Status      string                     `json:"status"`
	StartedAt   string                     `json:"started_at"`
	FinishedAt  string                     `json:"finished_at"`
	Steps       map[string]json.RawMessage `json:"steps,omitempty"`
}

func (c *ComposioREST) CreateWorkflow(ctx context.Context, req WorkflowCreateRequest) (WorkflowCreateResult, error) {
	key := strings.TrimSpace(req.Key)
	if key == "" {
		return WorkflowCreateResult{}, fmt.Errorf("workflow key is required")
	}
	normalized, err := c.normalizeWorkflowDefinition(req.Definition)
	if err != nil {
		return WorkflowCreateResult{}, err
	}
	path, err := saveWorkflowDefinition(c.Name(), key, normalized)
	if err != nil {
		return WorkflowCreateResult{}, err
	}
	return WorkflowCreateResult{Created: true, Key: key, Path: path}, nil
}

func (c *ComposioREST) ExecuteWorkflow(ctx context.Context, req WorkflowExecuteRequest) (WorkflowExecuteResult, error) {
	key, definition, _, err := loadWorkflowDefinition(c.Name(), req.KeyOrPath)
	if err != nil {
		return WorkflowExecuteResult{}, err
	}
	spec, err := c.decodeWorkflowDefinition(definition)
	if err != nil {
		return WorkflowExecuteResult{}, err
	}

	inputs := workflow.MergeInputs(spec.Inputs, req.Inputs)
	stepOutputs := map[string]any{}
	stepLogs := map[string]json.RawMessage{}
	runID := fmt.Sprintf("cmpwf_%d", time.Now().UTC().UnixNano())
	startedAt := time.Now().UTC()
	events := []json.RawMessage{
		mustMarshalJSON(map[string]any{
			"event":        "workflow_started",
			"provider":     c.Name(),
			"workflow_key": key,
			"run_id":       runID,
		}),
	}

	for _, step := range spec.Steps {
		scope := workflow.BuildScope(key, c.Name(), inputs, stepOutputs)
		output, err := c.executeWorkflowStep(ctx, step, scope, req.DryRun)
		if err != nil {
			return WorkflowExecuteResult{}, fmt.Errorf("workflow %s step %s failed: %w", key, step.ID, err)
		}
		stepOutputs[step.ID] = output
		stepLogs[step.ID] = mustMarshalJSON(output)
		events = append(events, mustMarshalJSON(map[string]any{
			"event":   "workflow_step_completed",
			"step_id": step.ID,
			"type":    step.Type,
		}))
	}

	status := "completed"
	if req.DryRun {
		status = "planned"
	}
	events = append(events, mustMarshalJSON(map[string]any{
		"event":  "workflow_finished",
		"run_id": runID,
		"status": status,
	}))

	_ = appendWorkflowRun(c.Name(), key, workflowRunRecord{
		Provider:    c.Name(),
		WorkflowKey: key,
		RunID:       runID,
		Status:      status,
		StartedAt:   startedAt.Format(time.RFC3339),
		FinishedAt:  time.Now().UTC().Format(time.RFC3339),
		Steps:       stepLogs,
	})

	return WorkflowExecuteResult{
		RunID:  runID,
		Status: status,
		Steps:  stepLogs,
		Events: events,
	}, nil
}

func (c *ComposioREST) ListWorkflowRuns(_ context.Context, key string) (WorkflowRunsResult, error) {
	return listWorkflowRuns(c.Name(), key)
}

func (c *ComposioREST) normalizeWorkflowDefinition(definition json.RawMessage) (json.RawMessage, error) {
	spec, err := c.decodeWorkflowDefinition(definition)
	if err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *ComposioREST) decodeWorkflowDefinition(definition json.RawMessage) (composioWorkflowDefinition, error) {
	var spec composioWorkflowDefinition
	if !json.Valid(definition) {
		return spec, fmt.Errorf("workflow definition must be valid JSON")
	}
	if err := json.Unmarshal(definition, &spec); err != nil {
		return spec, fmt.Errorf("parse workflow definition: %w", err)
	}
	spec.Version = fallbackString(spec.Version, composioWorkflowVersion)
	if spec.Version != composioWorkflowVersion {
		return spec, fmt.Errorf("unsupported composio workflow version %q", spec.Version)
	}
	spec.Inputs = normalizeWorkflowInputs(spec.Inputs)
	if len(spec.Steps) == 0 {
		return spec, fmt.Errorf("workflow definition must include at least one step")
	}
	seen := map[string]struct{}{}
	for i := range spec.Steps {
		spec.Steps[i].ID = strings.TrimSpace(spec.Steps[i].ID)
		spec.Steps[i].Type = normalizeWorkflowStepType(spec.Steps[i].Type)
		if len(spec.Steps[i].Data) == 0 && len(spec.Steps[i].Params) > 0 {
			spec.Steps[i].Data = spec.Steps[i].Params
		}
		spec.Steps[i].Template = workflow.NormalizeTemplateString(spec.Steps[i].Template)
		spec.Steps[i].QueryTemplate = workflow.NormalizeTemplateString(spec.Steps[i].QueryTemplate)
		spec.Steps[i].ConnectionKey = workflow.NormalizeValueSyntax(spec.Steps[i].ConnectionKey)
		spec.Steps[i].Data = workflow.NormalizeMapSyntax(spec.Steps[i].Data)
		spec.Steps[i].PathVariables = workflow.NormalizeMapSyntax(spec.Steps[i].PathVariables)
		spec.Steps[i].QueryParameters = workflow.NormalizeMapSyntax(spec.Steps[i].QueryParameters)
		spec.Steps[i].Headers = workflow.NormalizeMapSyntax(spec.Steps[i].Headers)
		spec.Steps[i].LookbackHours = workflow.NormalizeValueSyntax(spec.Steps[i].LookbackHours)
		spec.Steps[i].InsightLimit = workflow.NormalizeValueSyntax(spec.Steps[i].InsightLimit)
		if spec.Steps[i].ID == "" {
			return spec, fmt.Errorf("workflow step %d is missing id", i+1)
		}
		if _, ok := seen[spec.Steps[i].ID]; ok {
			return spec, fmt.Errorf("workflow step id %q is duplicated", spec.Steps[i].ID)
		}
		seen[spec.Steps[i].ID] = struct{}{}
		switch spec.Steps[i].Type {
		case "action":
			if strings.TrimSpace(spec.Steps[i].Platform) == "" {
				return spec, fmt.Errorf("workflow step %q is missing platform", spec.Steps[i].ID)
			}
			if strings.TrimSpace(spec.Steps[i].ActionID) == "" {
				return spec, fmt.Errorf("workflow step %q is missing action_id", spec.Steps[i].ID)
			}
		case "template":
			if strings.TrimSpace(spec.Steps[i].Template) == "" {
				return spec, fmt.Errorf("workflow step %q is missing template", spec.Steps[i].ID)
			}
		case "nex_ask":
			if strings.TrimSpace(spec.Steps[i].QueryTemplate) == "" {
				return spec, fmt.Errorf("workflow step %q is missing query_template", spec.Steps[i].ID)
			}
		case "nex_insights":
		default:
			return spec, fmt.Errorf("unsupported workflow step type %q", spec.Steps[i].Type)
		}
	}
	return spec, nil
}

func normalizeWorkflowStepType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "composio", "one", "external_action", "provider_action":
		return "action"
	default:
		return strings.TrimSpace(raw)
	}
}

func normalizeWorkflowInputs(inputs map[string]any) map[string]any {
	if len(inputs) == 0 {
		return inputs
	}
	out := make(map[string]any, len(inputs))
	for key, value := range inputs {
		if obj, ok := value.(map[string]any); ok {
			if def, ok := obj["default"]; ok {
				out[key] = workflow.NormalizeValueSyntax(def)
				continue
			}
		}
		out[key] = workflow.NormalizeValueSyntax(value)
	}
	return out
}

func (c *ComposioREST) executeWorkflowStep(ctx context.Context, step workflowStep, scope map[string]any, workflowDryRun bool) (map[string]any, error) {
	switch step.Type {
	case "action":
		return c.executeWorkflowActionStep(ctx, step, scope, workflowDryRun)
	case "template":
		return executeWorkflowTemplateStep(step, scope)
	case "nex_ask":
		return executeWorkflowNexAskStep(step, scope)
	case "nex_insights":
		return executeWorkflowNexInsightsStep(step, scope)
	default:
		return nil, fmt.Errorf("unsupported workflow step type %q", step.Type)
	}
}

func (c *ComposioREST) executeWorkflowActionStep(ctx context.Context, step workflowStep, scope map[string]any, workflowDryRun bool) (map[string]any, error) {
	connectionKey, err := workflow.RenderString(step.ConnectionKey, scope)
	if err != nil {
		return nil, fmt.Errorf("render connection_key: %w", err)
	}
	if strings.TrimSpace(connectionKey) == "" {
		connectionKey, err = c.autoResolveWorkflowConnection(ctx, step.Platform)
		if err != nil {
			return nil, err
		}
	}
	data, err := workflow.RenderMap(step.Data, scope)
	if err != nil {
		return nil, fmt.Errorf("render data: %w", err)
	}
	pathVariables, err := workflow.RenderMap(step.PathVariables, scope)
	if err != nil {
		return nil, fmt.Errorf("render path_variables: %w", err)
	}
	queryParameters, err := workflow.RenderMap(step.QueryParameters, scope)
	if err != nil {
		return nil, fmt.Errorf("render query_parameters: %w", err)
	}
	headers, err := workflow.RenderMap(step.Headers, scope)
	if err != nil {
		return nil, fmt.Errorf("render headers: %w", err)
	}
	stepDryRun := actionStepDryRun(step, workflowDryRun)
	result, err := c.ExecuteAction(ctx, ExecuteRequest{
		Platform:        step.Platform,
		ActionID:        step.ActionID,
		ConnectionKey:   connectionKey,
		Data:            data,
		PathVariables:   pathVariables,
		QueryParameters: queryParameters,
		Headers:         headers,
		FormData:        step.FormData,
		FormURLEncoded:  step.FormURLEncoded,
		DryRun:          stepDryRun,
	})
	if err != nil {
		return nil, err
	}
	decoded := decodeJSONObject(result.Response)
	return map[string]any{
		"type":           "action",
		"platform":       step.Platform,
		"action_id":      step.ActionID,
		"connection_key": connectionKey,
		"dry_run":        result.DryRun,
		"request":        result.Request,
		"response":       decoded,
		"result":         decoded,
	}, nil
}

func (c *ComposioREST) autoResolveWorkflowConnection(ctx context.Context, platform string) (string, error) {
	result, err := c.ListConnections(ctx, ListConnectionsOptions{Search: platform, Limit: 20})
	if err != nil {
		return "", fmt.Errorf("resolve connection_key: %w", err)
	}
	platform = normalizeComposioPlatform(platform)
	active := make([]Connection, 0, len(result.Connections))
	for _, conn := range result.Connections {
		if normalizeComposioPlatform(conn.Platform) != platform {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(conn.State)) {
		case "", "active", "operational", "connected":
			active = append(active, conn)
		}
	}
	if len(active) == 1 {
		return strings.TrimSpace(active[0].Key), nil
	}
	if len(active) == 0 {
		return "", fmt.Errorf("connection_key is required and no active %s connection was found", platform)
	}
	return "", fmt.Errorf("connection_key is required because %d active %s connections are available", len(active), platform)
}

func executeWorkflowTemplateStep(step workflowStep, scope map[string]any) (map[string]any, error) {
	text, err := workflow.RenderTemplate(step.Template, scope)
	if err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("template rendered empty")
	}
	return map[string]any{
		"type":   "template",
		"text":   text,
		"result": text,
	}, nil
}

func executeWorkflowNexAskStep(step workflowStep, scope map[string]any) (map[string]any, error) {
	query, err := workflow.RenderTemplate(step.QueryTemplate, scope)
	if err != nil {
		return nil, fmt.Errorf("render query_template: %w", err)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query_template rendered empty")
	}
	answer, err := nexAsk(query)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type":       "nex_ask",
		"query":      query,
		"answer":     strings.TrimSpace(answer.Answer),
		"session_id": strings.TrimSpace(answer.SessionID),
		"result":     strings.TrimSpace(answer.Answer),
	}, nil
}

func executeWorkflowNexInsightsStep(step workflowStep, scope map[string]any) (map[string]any, error) {
	lookbackHours, err := workflow.RenderInt(step.LookbackHours, scope, 24)
	if err != nil {
		return nil, fmt.Errorf("render lookback_hours: %w", err)
	}
	insightLimit, err := workflow.RenderInt(step.InsightLimit, scope, 5)
	if err != nil {
		return nil, fmt.Errorf("render insight_limit: %w", err)
	}
	from := time.Now().UTC().Add(-time.Duration(lookbackHours) * time.Hour)
	insights, err := nexInsightsSince(from, insightLimit)
	if err != nil {
		return nil, err
	}
	normalizedInsights := workflow.NormalizeScopeValue(insights.Insights)
	compactSummary := summarizeWorkflowInsights(insights.Insights)
	return map[string]any{
		"type":           "nex_insights",
		"lookback_hours": lookbackHours,
		"limit":          insightLimit,
		"from":           from.Format(time.RFC3339),
		"insights":       normalizedInsights,
		"result":         compactSummary,
	}, nil
}

func summarizeWorkflowInsights(items []nexInsightItem) string {
	if len(items) == 0 {
		return "No notable Nex insights in the requested window."
	}
	var b strings.Builder
	b.WriteString("Relevant Nex insights:\n")
	for _, item := range items {
		content := truncateWorkflowInsight(strings.TrimSpace(item.Content), 240)
		if content == "" {
			continue
		}
		label := strings.TrimSpace(item.Type)
		if label == "" {
			label = "insight"
		}
		fmt.Fprintf(&b, "- [%s] %s\n", label, content)
	}
	text := strings.TrimSpace(b.String())
	if text == "Relevant Nex insights:" || text == "" {
		return "No notable Nex insights in the requested window."
	}
	return text
}

func truncateWorkflowInsight(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	if max <= 1 {
		return text[:max]
	}
	return strings.TrimSpace(text[:max-1]) + "…"
}

func actionStepDryRun(step workflowStep, workflowDryRun bool) bool {
	if step.DryRun != nil {
		return *step.DryRun
	}
	if !workflowDryRun {
		return false
	}
	return actionLikelyWrites(step.ActionID)
}

func actionLikelyWrites(actionID string) bool {
	actionID = strings.ToUpper(strings.TrimSpace(actionID))
	writeMarkers := []string{
		"SEND",
		"CREATE",
		"UPDATE",
		"DELETE",
		"PATCH",
		"UPSERT",
		"POST",
		"INSERT",
		"UPLOAD",
		"COMPLETE",
	}
	for _, marker := range writeMarkers {
		if strings.Contains(actionID, marker) {
			return true
		}
	}
	return false
}

func decodeJSONObject(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return string(raw)
	}
	return normalizeDecodedJSON(decoded, 0)
}

func normalizeDecodedJSON(value any, depth int) any {
	if depth > 4 {
		return value
	}
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed != "" && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
			var nested any
			if err := json.Unmarshal([]byte(trimmed), &nested); err == nil {
				return normalizeDecodedJSON(nested, depth+1)
			}
		}
		return typed
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeDecodedJSON(item, depth+1))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = normalizeDecodedJSON(item, depth+1)
		}
		return out
	default:
		return value
	}
}

func mustMarshalJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

