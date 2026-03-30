package action

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

const defaultOneBin = "one"

type OneCLI struct {
	Bin        string
	ArgsPrefix []string
	WorkDir    string
	Env        []string
}

func NewOneCLIFromEnv() *OneCLI {
	bin := strings.TrimSpace(os.Getenv("WUPHF_ONE_BIN"))
	workDir := strings.TrimSpace(os.Getenv("WUPHF_ONE_WORKDIR"))
	if workDir == "" {
		cfgDir := filepath.Dir(config.ConfigPath())
		workDir = filepath.Join(cfgDir, "one")
	}
	var argsPrefix []string
	if bin == "" {
		switch {
		case lookPathExists(defaultOneBin):
			bin = defaultOneBin
		case lookPathExists("npx"):
			bin = "npx"
			argsPrefix = []string{"-y", "@withone/cli"}
		default:
			bin = defaultOneBin
		}
	}
	var env []string
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		env = append(env, "ONE_SECRET="+secret)
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		env = append(env, "ONE_IDENTITY="+identity)
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			env = append(env, "ONE_IDENTITY_TYPE="+identityType)
		}
	}
	return &OneCLI{
		Bin:        bin,
		ArgsPrefix: argsPrefix,
		WorkDir:    workDir,
		Env:        env,
	}
}

func (o *OneCLI) Name() string { return "one" }

func (o *OneCLI) Configured() bool {
	if config.ResolveNoNex() {
		return false
	}
	if lookPathExists(o.Bin) {
		return true
	}
	return o.Bin == "npx" && lookPathExists("npx")
}

func (o *OneCLI) Supports(cap Capability) bool {
	switch cap {
	case CapabilityGuide,
		CapabilityConnections,
		CapabilityActionSearch,
		CapabilityActionKnowledge,
		CapabilityActionExecute,
		CapabilityWorkflowCreate,
		CapabilityWorkflowExecute,
		CapabilityWorkflowRuns,
		CapabilityRelayList,
		CapabilityRelayEventTypes,
		CapabilityRelayCreate,
		CapabilityRelayActivate,
		CapabilityRelayEvents,
		CapabilityRelayEvent:
		return true
	default:
		return false
	}
}

func (o *OneCLI) Guide(ctx context.Context, topic string) (GuideResult, error) {
	if strings.TrimSpace(topic) == "" {
		topic = "all"
	}
	var raw json.RawMessage
	if err := o.runJSON(ctx, []string{"guide", topic}, &raw); err != nil {
		return GuideResult{}, err
	}
	return GuideResult{Topic: topic, Raw: raw}, nil
}

func (o *OneCLI) ListConnections(ctx context.Context, opts ListConnectionsOptions) (ConnectionsResult, error) {
	args := []string{"connection", "list"}
	if opts.Search != "" {
		args = append(args, "--search", opts.Search)
	}
	if opts.Limit > 0 {
		args = append(args, "--limit", strconv.Itoa(opts.Limit))
	}
	var result ConnectionsResult
	if err := o.runJSON(ctx, args, &result); err != nil {
		return ConnectionsResult{}, err
	}
	return result, nil
}

func (o *OneCLI) SearchActions(ctx context.Context, platform, query, mode string) (ActionSearchResult, error) {
	args := []string{"actions", "search", strings.TrimSpace(platform), strings.TrimSpace(query)}
	if mode != "" {
		args = append(args, "--type", mode)
	}
	var result struct {
		Actions []struct {
			ActionID string `json:"actionId"`
			Title    string `json:"title"`
			Method   string `json:"method"`
			Path     string `json:"path"`
		} `json:"actions"`
	}
	if err := o.runJSON(ctx, args, &result); err != nil {
		return ActionSearchResult{}, err
	}
	out := ActionSearchResult{Platform: platform, Query: query, Mode: mode}
	for _, action := range result.Actions {
		out.Actions = append(out.Actions, Action{
			ActionID: action.ActionID,
			Title:    action.Title,
			Method:   action.Method,
			Path:     action.Path,
		})
	}
	return out, nil
}

func (o *OneCLI) ActionKnowledge(ctx context.Context, platform, actionID string) (KnowledgeResult, error) {
	var result struct {
		Knowledge string `json:"knowledge"`
		Method    string `json:"method"`
	}
	if err := o.runJSON(ctx, []string{"actions", "knowledge", strings.TrimSpace(platform), strings.TrimSpace(actionID)}, &result); err != nil {
		return KnowledgeResult{}, err
	}
	return KnowledgeResult{
		Platform:  platform,
		ActionID:  actionID,
		Method:    result.Method,
		Knowledge: result.Knowledge,
	}, nil
}

func (o *OneCLI) ExecuteAction(ctx context.Context, req ExecuteRequest) (ExecuteResult, error) {
	args := []string{
		"actions", "execute",
		strings.TrimSpace(req.Platform),
		strings.TrimSpace(req.ActionID),
		strings.TrimSpace(req.ConnectionKey),
	}
	if len(req.Data) > 0 {
		args = append(args, "--data", marshalCompact(req.Data))
	}
	if len(req.PathVariables) > 0 {
		args = append(args, "--path-vars", marshalCompact(req.PathVariables))
	}
	if len(req.QueryParameters) > 0 {
		args = append(args, "--query-params", marshalCompact(req.QueryParameters))
	}
	if len(req.Headers) > 0 {
		args = append(args, "--headers", marshalCompact(req.Headers))
	}
	if req.FormData {
		args = append(args, "--form-data")
	}
	if req.FormURLEncoded {
		args = append(args, "--form-url-encoded")
	}
	if req.DryRun {
		args = append(args, "--dry-run")
	}
	var result struct {
		DryRun  bool `json:"dryRun"`
		Request struct {
			Method  string         `json:"method"`
			URL     string         `json:"url"`
			Headers map[string]any `json:"headers"`
			Data    map[string]any `json:"data"`
		} `json:"request"`
		Response json.RawMessage `json:"response"`
	}
	if err := o.runJSON(ctx, args, &result); err != nil {
		return ExecuteResult{}, err
	}
	return ExecuteResult{
		DryRun: result.DryRun,
		Request: ExecuteEnvelope{
			Method:  result.Request.Method,
			URL:     result.Request.URL,
			Headers: result.Request.Headers,
			Data:    result.Request.Data,
		},
		Response: result.Response,
	}, nil
}

func (o *OneCLI) CreateWorkflow(ctx context.Context, req WorkflowCreateRequest) (WorkflowCreateResult, error) {
	args := []string{"flow", "create", strings.TrimSpace(req.Key), "--definition", string(req.Definition)}
	var result WorkflowCreateResult
	if err := o.runJSON(ctx, args, &result); err != nil {
		return WorkflowCreateResult{}, err
	}
	return result, nil
}

func (o *OneCLI) ExecuteWorkflow(ctx context.Context, req WorkflowExecuteRequest) (WorkflowExecuteResult, error) {
	args := []string{"flow", "execute", strings.TrimSpace(req.KeyOrPath)}
	for key, value := range req.Inputs {
		args = append(args, "-i", fmt.Sprintf("%s=%s", key, marshalScalar(value)))
	}
	if req.DryRun {
		args = append(args, "--dry-run")
	}
	if req.Verbose {
		args = append(args, "-v")
	}
	if req.Mock {
		args = append(args, "--mock")
	}
	if req.AllowBash {
		args = append(args, "--allow-bash")
	}

	lines, err := o.runJSONLines(ctx, args)
	if err != nil {
		return WorkflowExecuteResult{}, err
	}
	out := WorkflowExecuteResult{Events: append([]json.RawMessage(nil), lines...)}
	for _, line := range lines {
		var probe struct {
			Event   string                     `json:"event"`
			RunID   string                     `json:"runId"`
			LogFile string                     `json:"logFile"`
			Status  string                     `json:"status"`
			Steps   map[string]json.RawMessage `json:"steps"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		if probe.Event == "workflow:result" || probe.Status != "" {
			out.RunID = probe.RunID
			out.LogFile = probe.LogFile
			out.Status = probe.Status
			if len(probe.Steps) > 0 {
				out.Steps = probe.Steps
			}
		}
	}
	return out, nil
}

func (o *OneCLI) ListWorkflowRuns(ctx context.Context, key string) (WorkflowRunsResult, error) {
	args := []string{"flow", "runs"}
	if strings.TrimSpace(key) != "" {
		args = append(args, key)
	}
	var raw json.RawMessage
	if err := o.runJSON(ctx, args, &raw); err != nil {
		return WorkflowRunsResult{}, err
	}
	return WorkflowRunsResult{Raw: raw}, nil
}

func (o *OneCLI) ListRelays(ctx context.Context, opts ListRelaysOptions) (RelayListResult, error) {
	args := []string{"relay", "list"}
	if opts.Limit > 0 {
		args = append(args, "--limit", strconv.Itoa(opts.Limit))
	}
	if opts.Page > 0 {
		args = append(args, "--page", strconv.Itoa(opts.Page))
	}
	var result struct {
		Total     int `json:"total"`
		Showing   int `json:"showing"`
		Endpoints []struct {
			ID           string   `json:"id"`
			Active       bool     `json:"active"`
			Description  string   `json:"description"`
			EventFilters []string `json:"eventFilters"`
			ActionsCount int      `json:"actionsCount"`
			URL          string   `json:"url"`
			CreatedAt    string   `json:"createdAt"`
		} `json:"endpoints"`
	}
	if err := o.runJSON(ctx, args, &result); err != nil {
		return RelayListResult{}, err
	}
	out := RelayListResult{Total: result.Total, Showing: result.Showing}
	for _, endpoint := range result.Endpoints {
		out.Endpoints = append(out.Endpoints, Relay{
			ID:           endpoint.ID,
			URL:          endpoint.URL,
			Active:       endpoint.Active,
			Description:  endpoint.Description,
			EventFilters: endpoint.EventFilters,
			ActionsCount: endpoint.ActionsCount,
			CreatedAt:    endpoint.CreatedAt,
		})
	}
	return out, nil
}

func (o *OneCLI) RelayEventTypes(ctx context.Context, platform string) (RelayEventTypesResult, error) {
	var result struct {
		Platform   string   `json:"platform"`
		EventTypes []string `json:"eventTypes"`
	}
	if err := o.runJSON(ctx, []string{"relay", "event-types", strings.TrimSpace(platform)}, &result); err != nil {
		return RelayEventTypesResult{}, err
	}
	return RelayEventTypesResult{Platform: result.Platform, EventTypes: result.EventTypes}, nil
}

func (o *OneCLI) CreateRelay(ctx context.Context, req RelayCreateRequest) (RelayResult, error) {
	args := []string{"relay", "create", "--connection-key", strings.TrimSpace(req.ConnectionKey)}
	if req.Description != "" {
		args = append(args, "--description", strings.TrimSpace(req.Description))
	}
	if len(req.EventFilters) > 0 {
		args = append(args, "--event-filters", marshalCompact(req.EventFilters))
	}
	if req.CreateWebhook {
		args = append(args, "--create-webhook")
	}
	var result struct {
		ID           string          `json:"id"`
		URL          string          `json:"url"`
		Active       bool            `json:"active"`
		Description  string          `json:"description"`
		EventFilters []string        `json:"eventFilters"`
		Actions      json.RawMessage `json:"actions"`
	}
	if err := o.runJSON(ctx, args, &result); err != nil {
		return RelayResult{}, err
	}
	return RelayResult{
		ID:           result.ID,
		URL:          result.URL,
		Active:       result.Active,
		Description:  result.Description,
		EventFilters: result.EventFilters,
		Actions:      result.Actions,
	}, nil
}

func (o *OneCLI) ActivateRelay(ctx context.Context, req RelayActivateRequest) (RelayResult, error) {
	args := []string{"relay", "activate", strings.TrimSpace(req.ID), "--actions", string(req.Actions)}
	if req.WebhookSecret != "" {
		args = append(args, "--webhook-secret", req.WebhookSecret)
	}
	var result struct {
		ID           string          `json:"id"`
		Active       bool            `json:"active"`
		Description  string          `json:"description"`
		EventFilters []string        `json:"eventFilters"`
		Actions      json.RawMessage `json:"actions"`
		URL          string          `json:"url"`
	}
	if err := o.runJSON(ctx, args, &result); err != nil {
		return RelayResult{}, err
	}
	return RelayResult{
		ID:           result.ID,
		URL:          result.URL,
		Active:       result.Active,
		Description:  result.Description,
		EventFilters: result.EventFilters,
		Actions:      result.Actions,
	}, nil
}

func (o *OneCLI) ListRelayEvents(ctx context.Context, opts RelayEventsOptions) (RelayEventsResult, error) {
	args := []string{"relay", "events"}
	if opts.Limit > 0 {
		args = append(args, "--limit", strconv.Itoa(opts.Limit))
	}
	if opts.Page > 0 {
		args = append(args, "--page", strconv.Itoa(opts.Page))
	}
	if opts.Platform != "" {
		args = append(args, "--platform", opts.Platform)
	}
	if opts.EventType != "" {
		args = append(args, "--event-type", opts.EventType)
	}
	if opts.After != "" {
		args = append(args, "--after", opts.After)
	}
	if opts.Before != "" {
		args = append(args, "--before", opts.Before)
	}
	var result struct {
		Total   int `json:"total"`
		Showing int `json:"showing"`
		Events  []struct {
			ID        string `json:"id"`
			Platform  string `json:"platform"`
			EventType string `json:"eventType"`
			Timestamp string `json:"timestamp"`
		} `json:"events"`
	}
	if err := o.runJSON(ctx, args, &result); err != nil {
		return RelayEventsResult{}, err
	}
	out := RelayEventsResult{Total: result.Total, Showing: result.Showing}
	for _, event := range result.Events {
		out.Events = append(out.Events, RelayEvent{
			ID:        event.ID,
			Platform:  event.Platform,
			EventType: event.EventType,
			Timestamp: event.Timestamp,
		})
	}
	return out, nil
}

func (o *OneCLI) GetRelayEvent(ctx context.Context, id string) (RelayEventDetail, error) {
	var raw json.RawMessage
	if err := o.runJSON(ctx, []string{"relay", "event", strings.TrimSpace(id)}, &raw); err != nil {
		return RelayEventDetail{}, err
	}
	var result struct {
		ID        string          `json:"id"`
		Platform  string          `json:"platform"`
		EventType string          `json:"eventType"`
		Timestamp string          `json:"timestamp"`
		CreatedAt string          `json:"createdAt"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return RelayEventDetail{}, err
	}
	ts := result.Timestamp
	if ts == "" {
		ts = result.CreatedAt
	}
	return RelayEventDetail{
		ID:        result.ID,
		Platform:  result.Platform,
		EventType: result.EventType,
		Timestamp: ts,
		Payload:   result.Payload,
		Raw:       raw,
	}, nil
}

func (o *OneCLI) runJSON(ctx context.Context, args []string, out any) error {
	stdout, err := o.run(ctx, args)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(stdout, out); err != nil {
		return fmt.Errorf("parse one JSON output: %w", err)
	}
	return nil
}

func (o *OneCLI) runJSONLines(ctx context.Context, args []string) ([]json.RawMessage, error) {
	stdout, err := o.run(ctx, args)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var lines []json.RawMessage
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			return nil, fmt.Errorf("parse one JSONL output: invalid JSON line %q", string(line))
		}
		lines = append(lines, append(json.RawMessage(nil), line...))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func (o *OneCLI) run(ctx context.Context, args []string) ([]byte, error) {
	if err := o.ensureConfigured(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(o.WorkDir, 0o700); err != nil {
		return nil, err
	}
	cmdArgs := append(append([]string{}, o.ArgsPrefix...), "--agent")
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.CommandContext(ctx, o.Bin, cmdArgs...)
	cmd.Dir = o.WorkDir
	cmd.Env = append(os.Environ(), o.Env...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.Error); ok && ee.Err == exec.ErrNotFound {
			return nil, fmt.Errorf("one CLI not found. Install One, make npx available, or set WUPHF_ONE_BIN")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("one CLI failed: %s", msg)
	}
	return bytes.TrimSpace(stdout.Bytes()), nil
}

func (o *OneCLI) ensureConfigured() error {
	if config.ResolveNoNex() {
		return errors.New("Nex is disabled for this session (--no-nex), so WUPHF-managed integrations are unavailable")
	}
	return nil
}

func (o *OneCLI) hasEnvPrefix(prefix string) bool {
	for _, item := range o.Env {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}

func marshalCompact(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func lookPathExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func marshalScalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return marshalCompact(v)
	}
}
