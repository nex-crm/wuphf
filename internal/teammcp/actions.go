package teammcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/calendar"
)

var externalActionProvider action.Provider

type TeamActionGuideArgs struct {
	Topic string `json:"topic,omitempty" jsonschema:"One of: overview, actions, flows, relay, all. Defaults to all."`
}

type TeamActionConnectionsArgs struct {
	Search string `json:"search,omitempty" jsonschema:"Optional platform search query like gmail or hub-spot"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum connections to return"`
}

type TeamActionSearchArgs struct {
	Platform string `json:"platform" jsonschema:"Kebab-case platform name like gmail, slack, hub-spot, google-calendar"`
	Query    string `json:"query" jsonschema:"Natural-language action search like send email or create contact"`
	Mode     string `json:"mode,omitempty" jsonschema:"One of: execute or knowledge. Defaults to execute when the intent is to actually do something."`
}

type TeamActionKnowledgeArgs struct {
	Platform string `json:"platform" jsonschema:"Kebab-case platform name"`
	ActionID string `json:"action_id" jsonschema:"Action ID returned by team_action_search"`
}

type TeamActionExecuteArgs struct {
	Platform        string         `json:"platform" jsonschema:"Kebab-case platform name"`
	ActionID        string         `json:"action_id" jsonschema:"Action ID returned by team_action_search"`
	ConnectionKey   string         `json:"connection_key" jsonschema:"Connection key from team_action_connections"`
	Data            map[string]any `json:"data,omitempty" jsonschema:"Request body as a JSON object"`
	PathVariables   map[string]any `json:"path_variables,omitempty" jsonschema:"Path variables as a JSON object"`
	QueryParameters map[string]any `json:"query_parameters,omitempty" jsonschema:"Query parameters as a JSON object"`
	Headers         map[string]any `json:"headers,omitempty" jsonschema:"Extra headers as a JSON object"`
	FormData        bool           `json:"form_data,omitempty" jsonschema:"Send as multipart/form-data"`
	FormURLEncoded  bool           `json:"form_url_encoded,omitempty" jsonschema:"Send as application/x-www-form-urlencoded"`
	DryRun          bool           `json:"dry_run,omitempty" jsonschema:"Build the request without sending it"`
	Channel         string         `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug          string         `json:"my_slug,omitempty" jsonschema:"Agent slug performing the action. Defaults to WUPHF_AGENT_SLUG."`
	Summary         string         `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
}

type TeamActionWorkflowCreateArgs struct {
	Key              string   `json:"key" jsonschema:"Stable workflow key like daily-digest or escalate-renewal-risk"`
	DefinitionJSON   string   `json:"definition_json" jsonschema:"Full One workflow JSON definition as a string"`
	Channel          string   `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug           string   `json:"my_slug,omitempty" jsonschema:"Agent slug creating the workflow. Defaults to WUPHF_AGENT_SLUG."`
	Summary          string   `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
	SkillName        string   `json:"skill_name,omitempty" jsonschema:"Optional WUPHF skill name. Defaults to the workflow key."`
	SkillTitle       string   `json:"skill_title,omitempty" jsonschema:"Optional skill title shown in the Skills app."`
	SkillDescription string   `json:"skill_description,omitempty" jsonschema:"Optional skill description shown in the Skills app."`
	SkillTags        []string `json:"skill_tags,omitempty" jsonschema:"Optional skill tags"`
	SkillTrigger     string   `json:"skill_trigger,omitempty" jsonschema:"Optional trigger text that explains when the workflow should run"`
}

type TeamActionWorkflowExecuteArgs struct {
	KeyOrPath string         `json:"key_or_path" jsonschema:"Workflow key or path"`
	Inputs    map[string]any `json:"inputs,omitempty" jsonschema:"Workflow inputs as a JSON object"`
	DryRun    bool           `json:"dry_run,omitempty" jsonschema:"Run in dry-run mode"`
	Verbose   bool           `json:"verbose,omitempty" jsonschema:"Emit verbose workflow events"`
	Mock      bool           `json:"mock,omitempty" jsonschema:"Mock external steps where supported"`
	AllowBash bool           `json:"allow_bash,omitempty" jsonschema:"Allow bash/code steps in the workflow"`
	Channel   string         `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug    string         `json:"my_slug,omitempty" jsonschema:"Agent slug executing the workflow. Defaults to WUPHF_AGENT_SLUG."`
	Summary   string         `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
}

type TeamActionWorkflowScheduleArgs struct {
	Key        string         `json:"key" jsonschema:"Saved workflow key to run on a schedule"`
	Schedule   string         `json:"schedule" jsonschema:"Cron expression or shorthand like daily, hourly, 4h, or 0 9 * * 1-5"`
	Inputs     map[string]any `json:"inputs,omitempty" jsonschema:"Optional workflow inputs"`
	Channel    string         `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug     string         `json:"my_slug,omitempty" jsonschema:"Agent slug scheduling the workflow. Defaults to WUPHF_AGENT_SLUG."`
	Summary    string         `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
	SkillName  string         `json:"skill_name,omitempty" jsonschema:"Optional existing or new WUPHF skill name to mirror this workflow"`
	SkillTitle string         `json:"skill_title,omitempty" jsonschema:"Optional skill title when creating or updating the mirrored skill"`
}

type TeamActionRelaysArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"Maximum relays to return"`
	Page  int `json:"page,omitempty" jsonschema:"Page number"`
}

type TeamActionRelayEventTypesArgs struct {
	Platform string `json:"platform" jsonschema:"Kebab-case platform name like gmail, stripe, google-calendar"`
}

type TeamActionRelayCreateArgs struct {
	ConnectionKey string   `json:"connection_key" jsonschema:"Connection key from team_action_connections"`
	Description   string   `json:"description,omitempty" jsonschema:"Short description of what the relay is for"`
	EventFilters  []string `json:"event_filters,omitempty" jsonschema:"Optional list of event types to include"`
	CreateWebhook bool     `json:"create_webhook,omitempty" jsonschema:"Whether One should create the webhook endpoint on the source platform where supported"`
	Channel       string   `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug        string   `json:"my_slug,omitempty" jsonschema:"Agent slug creating the relay. Defaults to WUPHF_AGENT_SLUG."`
	Summary       string   `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
}

type TeamActionRelayActivateArgs struct {
	ID            string `json:"id" jsonschema:"Relay endpoint ID"`
	ActionsJSON   string `json:"actions_json" jsonschema:"JSON array of relay forwarding actions"`
	WebhookSecret string `json:"webhook_secret,omitempty" jsonschema:"Optional webhook secret"`
	Channel       string `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug        string `json:"my_slug,omitempty" jsonschema:"Agent slug activating the relay. Defaults to WUPHF_AGENT_SLUG."`
	Summary       string `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
}

type TeamActionRelayEventsArgs struct {
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum events to return"`
	Page      int    `json:"page,omitempty" jsonschema:"Page number"`
	Platform  string `json:"platform,omitempty" jsonschema:"Optional platform filter"`
	EventType string `json:"event_type,omitempty" jsonschema:"Optional event type filter"`
	After     string `json:"after,omitempty" jsonschema:"Optional cursor/time filter supported by One"`
	Before    string `json:"before,omitempty" jsonschema:"Optional cursor/time filter supported by One"`
}

type TeamActionRelayEventArgs struct {
	ID string `json:"id" jsonschema:"Relay event ID"`
}

func registerActionTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_guide",
		Description: "Read the current external action provider guide in machine-readable form before building or wiring external actions.",
	}, handleTeamActionGuide)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_connections",
		Description: "List connected external accounts and connection keys available through the current action provider.",
	}, handleTeamActionConnections)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_search",
		Description: "Search for external actions on a platform using natural language. Use this before knowledge or execute.",
	}, handleTeamActionSearch)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_knowledge",
		Description: "Load the schema and usage guidance for an external action. Always do this before executing or wiring the action.",
	}, handleTeamActionKnowledge)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_execute",
		Description: "Execute an external action through the selected provider. Use dry_run first for risky writes.",
	}, handleTeamActionExecute)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_workflow_create",
		Description: "Save a reusable external workflow from a full JSON definition.",
	}, handleTeamActionWorkflowCreate)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_workflow_execute",
		Description: "Execute a saved external workflow by key or path.",
	}, handleTeamActionWorkflowExecute)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_workflow_schedule",
		Description: "Schedule a saved external workflow on a WUPHF-native cadence so it shows up in Calendar and runs through the office scheduler.",
	}, handleTeamActionWorkflowSchedule)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_relays",
		Description: "List registered external triggers or relay endpoints for the selected provider.",
	}, handleTeamActionRelays)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_relay_event_types",
		Description: "List supported event types for a platform before creating a trigger or relay.",
	}, handleTeamActionRelayEventTypes)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_relay_create",
		Description: "Create an external trigger or relay for receiving events from a connected platform.",
	}, handleTeamActionRelayCreate)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_relay_activate",
		Description: "Enable or activate a previously registered external trigger or relay.",
	}, handleTeamActionRelayActivate)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_relay_events",
		Description: "List recent One relay events so the office can inspect or poll them.",
	}, handleTeamActionRelayEvents)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_action_relay_event",
		Description: "Fetch the full payload for one specific relay event.",
	}, handleTeamActionRelayEvent)
}

func handleTeamActionGuide(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionGuideArgs) (*mcp.CallToolResult, any, error) {
	provider, err := selectedActionProvider(action.CapabilityGuide)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.Guide(ctx, args.Topic)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyJSON(result.Raw)), nil, nil
}

func handleTeamActionConnections(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionConnectionsArgs) (*mcp.CallToolResult, any, error) {
	provider, err := selectedActionProvider(action.CapabilityConnections)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ListConnections(ctx, action.ListConnectionsOptions{Search: args.Search, Limit: args.Limit})
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionSearch(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionSearchArgs) (*mcp.CallToolResult, any, error) {
	mode := strings.TrimSpace(args.Mode)
	if mode == "" {
		mode = "execute"
	}
	provider, err := selectedActionProvider(action.CapabilityActionSearch)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.SearchActions(ctx, args.Platform, args.Query, mode)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionKnowledge(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionKnowledgeArgs) (*mcp.CallToolResult, any, error) {
	provider, err := selectedActionProvider(action.CapabilityActionKnowledge)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ActionKnowledge(ctx, args.Platform, args.ActionID)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionExecute(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionExecuteArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
	provider, err := selectedActionProvider(action.CapabilityActionExecute)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ExecuteAction(ctx, action.ExecuteRequest{
		Platform:        args.Platform,
		ActionID:        args.ActionID,
		ConnectionKey:   args.ConnectionKey,
		Data:            args.Data,
		PathVariables:   args.PathVariables,
		QueryParameters: args.QueryParameters,
		Headers:         args.Headers,
		FormData:        args.FormData,
		FormURLEncoded:  args.FormURLEncoded,
		DryRun:          args.DryRun,
	})
	if err != nil {
		_ = brokerRecordAction(ctx, "external_action_failed", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("%s action %s on %s failed", strings.Title(provider.Name()), args.ActionID, args.Platform)), args.ActionID)
		return toolError(err), nil, nil
	}
	kind := "external_action_executed"
	summary := fallbackSummary(args.Summary, fmt.Sprintf("Executed %s on %s via %s", args.ActionID, args.Platform, strings.Title(provider.Name())))
	if args.DryRun {
		kind = "external_action_planned"
		summary = fallbackSummary(args.Summary, fmt.Sprintf("Planned %s on %s via %s", args.ActionID, args.Platform, strings.Title(provider.Name())))
	}
	_ = brokerRecordAction(ctx, kind, provider.Name(), channel, slug, summary, args.ActionID)
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionWorkflowCreate(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionWorkflowCreateArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
	definition := json.RawMessage(strings.TrimSpace(args.DefinitionJSON))
	if !json.Valid(definition) {
		return toolError(fmt.Errorf("definition_json must be valid JSON")), nil, nil
	}
	provider, err := selectedActionProvider(action.CapabilityWorkflowCreate)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.CreateWorkflow(ctx, action.WorkflowCreateRequest{
		Key:        args.Key,
		Definition: definition,
	})
	if err != nil {
		_ = brokerRecordAction(ctx, "external_workflow_failed", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Creating workflow %s via %s failed", args.Key, strings.Title(provider.Name()))), args.Key)
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(result.Key) == "" {
		result.Key = strings.TrimSpace(args.Key)
	}
	if err := upsertWorkflowSkill(ctx, workflowSkillSpec{
		Name:             fallbackString(args.SkillName, result.Key),
		Title:            fallbackString(args.SkillTitle, humanizeWorkflowKey(result.Key)),
		Description:      fallbackString(args.SkillDescription, fmt.Sprintf("Reusable %s workflow for %s.", strings.Title(provider.Name()), humanizeWorkflowKey(result.Key))),
		Tags:             append([]string{provider.Name(), "workflow"}, args.SkillTags...),
		Trigger:          strings.TrimSpace(args.SkillTrigger),
		WorkflowProvider: provider.Name(),
		WorkflowKey:      result.Key,
		WorkflowDef:      strings.TrimSpace(args.DefinitionJSON),
		Channel:          channel,
		CreatedBy:        slug,
	}); err != nil {
		_ = brokerRecordAction(ctx, "external_workflow_failed", provider.Name(), channel, slug, fmt.Sprintf("Created workflow %s via %s, but failed to mirror it into Skills", result.Key, strings.Title(provider.Name())), result.Key)
		return toolError(err), nil, nil
	}
	_ = brokerRecordAction(ctx, "external_workflow_created", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Created workflow %s via %s", result.Key, strings.Title(provider.Name()))), result.Key)
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionWorkflowExecute(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionWorkflowExecuteArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
	provider, err := selectedActionProvider(action.CapabilityWorkflowExecute)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ExecuteWorkflow(ctx, action.WorkflowExecuteRequest{
		KeyOrPath: args.KeyOrPath,
		Inputs:    args.Inputs,
		DryRun:    args.DryRun,
		Verbose:   args.Verbose,
		Mock:      args.Mock,
		AllowBash: args.AllowBash,
	})
	if err != nil {
		_ = brokerRecordAction(ctx, "external_workflow_failed", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Workflow %s via %s failed", args.KeyOrPath, strings.Title(provider.Name()))), args.KeyOrPath)
		return toolError(err), nil, nil
	}
	kind := "external_workflow_executed"
	summary := fallbackSummary(args.Summary, fmt.Sprintf("Executed workflow %s via %s", args.KeyOrPath, strings.Title(provider.Name())))
	if args.DryRun {
		kind = "external_workflow_planned"
		summary = fallbackSummary(args.Summary, fmt.Sprintf("Planned workflow %s via %s", args.KeyOrPath, strings.Title(provider.Name())))
	}
	_ = brokerRecordAction(ctx, kind, provider.Name(), channel, slug, summary, args.KeyOrPath)
	_ = touchWorkflowSkill(ctx, args.KeyOrPath, result.Status, time.Now().UTC())
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionWorkflowSchedule(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionWorkflowScheduleArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	provider, err := selectedActionProvider(action.CapabilityWorkflowExecute)
	if err != nil {
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(args.Key) == "" {
		return toolError(fmt.Errorf("key is required")), nil, nil
	}
	channel := resolveChannel(args.Channel)
	sched, err := calendar.ParseCron(args.Schedule)
	if err != nil {
		return toolError(fmt.Errorf("invalid schedule %q: %w", args.Schedule, err)), nil, nil
	}
	nextRun := sched.Next(time.Now().UTC())
	if nextRun.IsZero() {
		return toolError(fmt.Errorf("could not compute next run for %q", args.Schedule)), nil, nil
	}
	payload, err := json.Marshal(map[string]any{
		"provider":      provider.Name(),
		"workflow_key":  strings.TrimSpace(args.Key),
		"inputs":        args.Inputs,
		"schedule_expr": strings.TrimSpace(args.Schedule),
		"created_by":    slug,
		"channel":       channel,
		"skill_name":    strings.TrimSpace(args.SkillName),
	})
	if err != nil {
		return toolError(err), nil, nil
	}
	job := map[string]any{
		"slug":          schedulerSlug(channel, args.Key),
		"kind":          provider.Name() + "_workflow",
		"label":         "Run " + humanizeWorkflowKey(args.Key),
		"target_type":   "workflow",
		"target_id":     strings.TrimSpace(args.Key),
		"channel":       channel,
		"provider":      provider.Name(),
		"workflow_key":  strings.TrimSpace(args.Key),
		"skill_name":    strings.TrimSpace(args.SkillName),
		"schedule_expr": strings.TrimSpace(args.Schedule),
		"due_at":        nextRun.UTC().Format(time.RFC3339),
		"next_run":      nextRun.UTC().Format(time.RFC3339),
		"status":        "scheduled",
		"payload":       string(payload),
	}
	if err := brokerPostJSON(ctx, "/scheduler", job, nil); err != nil {
		_ = brokerRecordAction(ctx, "external_workflow_failed", provider.Name(), channel, slug, fmt.Sprintf("Failed to schedule workflow %s via %s", args.Key, strings.Title(provider.Name())), args.Key)
		return toolError(err), nil, nil
	}
	skillName := strings.TrimSpace(args.SkillName)
	if skillName == "" {
		skillName = strings.TrimSpace(args.Key)
	}
	_ = upsertWorkflowSkill(ctx, workflowSkillSpec{
		Name:             skillName,
		Title:            fallbackString(args.SkillTitle, humanizeWorkflowKey(args.Key)),
		Description:      fmt.Sprintf("Reusable %s workflow for %s.", strings.Title(provider.Name()), humanizeWorkflowKey(args.Key)),
		Tags:             []string{provider.Name(), "workflow", "scheduled"},
		WorkflowProvider: provider.Name(),
		WorkflowKey:      strings.TrimSpace(args.Key),
		WorkflowSchedule: strings.TrimSpace(args.Schedule),
		Channel:          channel,
		CreatedBy:        slug,
	})
	_ = brokerRecordAction(ctx, "external_workflow_scheduled", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Scheduled workflow %s via %s (%s)", args.Key, strings.Title(provider.Name()), args.Schedule)), args.Key)
	return textResult(prettyObject(map[string]any{
		"ok":           true,
		"workflow_key": strings.TrimSpace(args.Key),
		"schedule":     strings.TrimSpace(args.Schedule),
		"next_run":     nextRun.UTC().Format(time.RFC3339),
		"skill_name":   skillName,
	})), nil, nil
}

func handleTeamActionRelays(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelaysArgs) (*mcp.CallToolResult, any, error) {
	provider, err := selectedActionProvider(action.CapabilityRelayList)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ListRelays(ctx, action.ListRelaysOptions{Limit: args.Limit, Page: args.Page})
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionRelayEventTypes(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelayEventTypesArgs) (*mcp.CallToolResult, any, error) {
	provider, err := selectedActionProvider(action.CapabilityRelayEventTypes)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.RelayEventTypes(ctx, args.Platform)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionRelayCreate(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelayCreateArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
	provider, err := selectedActionProvider(action.CapabilityRelayCreate)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.CreateRelay(ctx, action.RelayCreateRequest{
		ConnectionKey: args.ConnectionKey,
		Description:   args.Description,
		EventFilters:  args.EventFilters,
		CreateWebhook: args.CreateWebhook,
	})
	if err != nil {
		_ = brokerRecordAction(ctx, "external_trigger_failed", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Creating trigger for %s via %s failed", args.ConnectionKey, strings.Title(provider.Name()))), args.ConnectionKey)
		return toolError(err), nil, nil
	}
	_ = brokerRecordAction(ctx, "external_trigger_registered", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Created trigger %s via %s", result.ID, strings.Title(provider.Name()))), result.ID)
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionRelayActivate(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelayActivateArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
	actions := json.RawMessage(strings.TrimSpace(args.ActionsJSON))
	if !json.Valid(actions) {
		return toolError(fmt.Errorf("actions_json must be valid JSON")), nil, nil
	}
	provider, err := selectedActionProvider(action.CapabilityRelayActivate)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ActivateRelay(ctx, action.RelayActivateRequest{
		ID:            args.ID,
		Actions:       actions,
		WebhookSecret: args.WebhookSecret,
	})
	if err != nil {
		_ = brokerRecordAction(ctx, "external_trigger_failed", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Activating trigger %s via %s failed", args.ID, strings.Title(provider.Name()))), args.ID)
		return toolError(err), nil, nil
	}
	_ = brokerRecordAction(ctx, "external_trigger_registered", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Activated trigger %s via %s", result.ID, strings.Title(provider.Name()))), result.ID)
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionRelayEvents(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelayEventsArgs) (*mcp.CallToolResult, any, error) {
	result, err := externalActionProvider.ListRelayEvents(ctx, action.RelayEventsOptions{
		Limit:     args.Limit,
		Page:      args.Page,
		Platform:  args.Platform,
		EventType: args.EventType,
		After:     args.After,
		Before:    args.Before,
	})
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionRelayEvent(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelayEventArgs) (*mcp.CallToolResult, any, error) {
	result, err := externalActionProvider.GetRelayEvent(ctx, args.ID)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func brokerRecordAction(ctx context.Context, kind, source, channel, actor, summary, relatedID string) error {
	return brokerPostJSON(ctx, "/actions", map[string]any{
		"kind":       strings.TrimSpace(kind),
		"source":     strings.TrimSpace(source),
		"channel":    resolveChannel(channel),
		"actor":      strings.TrimSpace(actor),
		"summary":    strings.TrimSpace(summary),
		"related_id": strings.TrimSpace(relatedID),
	}, nil)
}

type workflowSkillSpec struct {
	Name             string
	Title            string
	Description      string
	Tags             []string
	Trigger          string
	WorkflowProvider string
	WorkflowKey      string
	WorkflowDef      string
	WorkflowSchedule string
	RelayID          string
	RelayPlatform    string
	RelayEventTypes  []string
	Channel          string
	CreatedBy        string
}

func upsertWorkflowSkill(ctx context.Context, spec workflowSkillSpec) error {
	if strings.TrimSpace(spec.Name) == "" || strings.TrimSpace(spec.CreatedBy) == "" {
		return nil
	}
	payload := map[string]any{
		"action":               "create",
		"name":                 strings.TrimSpace(spec.Name),
		"title":                strings.TrimSpace(spec.Title),
		"description":          strings.TrimSpace(spec.Description),
		"content":              workflowSkillContent(spec),
		"created_by":           strings.TrimSpace(spec.CreatedBy),
		"channel":              resolveChannel(spec.Channel),
		"tags":                 compactStrings(spec.Tags),
		"trigger":              strings.TrimSpace(spec.Trigger),
		"workflow_provider":    strings.TrimSpace(spec.WorkflowProvider),
		"workflow_key":         strings.TrimSpace(spec.WorkflowKey),
		"workflow_definition":  strings.TrimSpace(spec.WorkflowDef),
		"workflow_schedule":    strings.TrimSpace(spec.WorkflowSchedule),
		"relay_id":             strings.TrimSpace(spec.RelayID),
		"relay_platform":       strings.TrimSpace(spec.RelayPlatform),
		"relay_event_types":    compactStrings(spec.RelayEventTypes),
		"last_execution_status": "",
	}
	if err := brokerPostJSON(ctx, "/skills", payload, nil); err == nil {
		return nil
	} else if !strings.Contains(err.Error(), "409") {
		return err
	}
	return brokerPutJSON(ctx, "/skills", map[string]any{
		"name":                  strings.TrimSpace(spec.Name),
		"title":                 strings.TrimSpace(spec.Title),
		"description":           strings.TrimSpace(spec.Description),
		"content":               workflowSkillContent(spec),
		"channel":               resolveChannel(spec.Channel),
		"tags":                  compactStrings(spec.Tags),
		"trigger":               strings.TrimSpace(spec.Trigger),
		"workflow_provider":     strings.TrimSpace(spec.WorkflowProvider),
		"workflow_key":          strings.TrimSpace(spec.WorkflowKey),
		"workflow_definition":   strings.TrimSpace(spec.WorkflowDef),
		"workflow_schedule":     strings.TrimSpace(spec.WorkflowSchedule),
		"relay_id":              strings.TrimSpace(spec.RelayID),
		"relay_platform":        strings.TrimSpace(spec.RelayPlatform),
		"relay_event_types":     compactStrings(spec.RelayEventTypes),
	}, nil)
}

func touchWorkflowSkill(ctx context.Context, workflowKey, status string, when time.Time) error {
	key := strings.TrimSpace(workflowKey)
	if key == "" {
		return nil
	}
	return brokerPutJSON(ctx, "/skills", map[string]any{
		"name":                  key,
		"workflow_key":          key,
		"last_execution_at":     when.UTC().Format(time.RFC3339),
		"last_execution_status": strings.TrimSpace(status),
	}, nil)
}

func workflowSkillContent(spec workflowSkillSpec) string {
	lines := []string{
		fmt.Sprintf("One workflow skill: %s", humanizeWorkflowKey(fallbackString(spec.WorkflowKey, spec.Name))),
		"Use team_action_workflow_execute to run it through WUPHF.",
	}
	if strings.TrimSpace(spec.WorkflowSchedule) != "" {
		lines = append(lines, "Schedule: "+strings.TrimSpace(spec.WorkflowSchedule))
	}
	if strings.TrimSpace(spec.Trigger) != "" {
		lines = append(lines, "Trigger: "+strings.TrimSpace(spec.Trigger))
	}
	if strings.TrimSpace(spec.RelayID) != "" {
		lines = append(lines, "Relay: "+strings.TrimSpace(spec.RelayID))
	}
	return strings.Join(lines, "\n")
}

func compactStrings(items []string) []string {
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func humanizeWorkflowKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "Workflow"
	}
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '-' || r == '_' || r == ':'
	})
	for i := range parts {
		parts[i] = strings.Title(parts[i])
	}
	return strings.Join(parts, " ")
}

func schedulerSlug(channel, workflowKey string) string {
	channel = resolveChannel(channel)
	workflowKey = strings.ToLower(strings.TrimSpace(workflowKey))
	workflowKey = strings.ReplaceAll(workflowKey, " ", "-")
	return fmt.Sprintf("one-workflow:%s:%s", channel, workflowKey)
}

func fallbackSummary(explicit, fallback string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	return fallback
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func prettyObject(v any) string {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", v)
	}
	return string(raw)
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err == nil {
		return out.String()
	}
	return string(raw)
}

func selectedActionProvider(cap action.Capability) (action.Provider, error) {
	if externalActionProvider != nil {
		return externalActionProvider, nil
	}
	return action.NewRegistryFromEnv().ProviderFor(cap)
}
