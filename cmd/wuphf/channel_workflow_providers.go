package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/workflow"
)

// composioActionProvider executes real Composio actions (Gmail, Slack, etc.).
type composioActionProvider struct {
	registry *action.Registry
}

func newComposioActionProvider(registry *action.Registry) *composioActionProvider {
	return &composioActionProvider{registry: registry}
}

func (p *composioActionProvider) Execute(ctx context.Context, exec workflow.ExecuteSpec, dataStore map[string]any) (map[string]any, error) {
	provider, err := p.registry.ProviderFor(action.CapabilityActionExecute)
	if err != nil {
		return nil, fmt.Errorf("resolve action provider: %w", err)
	}

	req := action.ExecuteRequest{
		ActionID:      exec.Action,
		ConnectionKey: exec.ConnectionKey,
		Data:          resolveDataRefs(exec.Data, dataStore),
	}

	// Infer platform from the action ID (e.g. GMAIL_SEND_EMAIL → gmail).
	if parts := strings.SplitN(exec.Action, "_", 2); len(parts) >= 1 {
		req.Platform = strings.ToLower(parts[0])
	}

	result, err := provider.ExecuteAction(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("execute %s: %w", exec.Action, err)
	}

	var decoded any
	if len(result.Response) > 0 {
		_ = json.Unmarshal(result.Response, &decoded)
	}

	return map[string]any{
		"provider": exec.Provider,
		"action":   exec.Action,
		"dry_run":  result.DryRun,
		"response": decoded,
	}, nil
}

// resolveDataRefs replaces {"ref": "/path"} values with actual data store values.
func resolveDataRefs(data map[string]any, dataStore map[string]any) map[string]any {
	if len(data) == 0 {
		return data
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		if m, ok := v.(map[string]any); ok {
			if ref, ok := m["ref"].(string); ok && strings.HasPrefix(ref, "/") {
				key := strings.TrimPrefix(ref, "/")
				if resolved, ok := dataStore[key]; ok {
					out[k] = resolved
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

// brokerAgentDispatcher sends tasks to agents via the broker HTTP API.
type brokerAgentDispatcher struct {
	brokerAddr  string
	brokerToken string
	channel     string
	timeout     time.Duration
}

func newBrokerAgentDispatcher(addr, token, channel string) *brokerAgentDispatcher {
	return &brokerAgentDispatcher{
		brokerAddr:  addr,
		brokerToken: token,
		channel:     channel,
		timeout:     60 * time.Second,
	}
}

func (d *brokerAgentDispatcher) Dispatch(ctx context.Context, agentSlug string, prompt string) (map[string]any, error) {
	// Get the current message count so we know where to start polling for replies.
	beforeID := d.getLastMessageID(ctx)

	// Route through the CEO agent, which handles all workflow dispatches.
	// The CEO sees tagged messages and responds in the same channel.
	targetAgent := agentSlug
	if targetAgent == "brainstorm" || targetAgent == "deploy-checker" ||
		targetAgent == "deploy-runner" || targetAgent == "email-triage" {
		targetAgent = "ceo"
	}

	payload := map[string]any{
		"from":    "you",
		"channel": d.channel,
		"content": prompt,
		"tagged":  []string{"@" + targetAgent},
		"kind":    "workflow_agent_request",
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.brokerAddr+"/messages", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.brokerToken != "" {
		req.Header.Set("Authorization", "Bearer "+d.brokerToken)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post to broker: %w", err)
	}
	resp.Body.Close()

	// Poll for the CEO's reply. Look for any message from a non-"you" sender
	// that appeared after our request.
	deadline := time.Now().Add(d.timeout)
	for time.Now().Before(deadline) {
		reply, err := d.pollAgentReply(ctx, targetAgent, beforeID)
		if err != nil {
			return nil, err
		}
		if reply != nil {
			return reply, nil
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("@%s did not respond within %s", targetAgent, d.timeout)
}

func (d *brokerAgentDispatcher) getLastMessageID(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		d.brokerAddr+"/messages?channel="+d.channel, nil)
	if err != nil {
		return ""
	}
	if d.brokerToken != "" {
		req.Header.Set("Authorization", "Bearer "+d.brokerToken)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var result struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(raw, &result)
	if len(result.Messages) > 0 {
		return result.Messages[len(result.Messages)-1].ID
	}
	return ""
}

func (d *brokerAgentDispatcher) pollAgentReply(ctx context.Context, agentSlug, afterID string) (map[string]any, error) {
	url := d.brokerAddr + "/messages?channel=" + d.channel
	if afterID != "" {
		url += "&after=" + afterID
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil
	}
	if d.brokerToken != "" {
		req.Header.Set("Authorization", "Bearer "+d.brokerToken)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil // Transient, keep polling.
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var result struct {
		Messages []struct {
			ID      string `json:"id"`
			From    string `json:"from"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, nil
	}

	// Look for any reply from the agent (not from "you" or "system").
	for _, msg := range result.Messages {
		if msg.From == "you" || msg.From == "system" || msg.From == "nex" {
			continue
		}
		// Found an agent reply. Parse the content.
		content := msg.Content

		// Try JSON object.
		var obj map[string]any
		if err := json.Unmarshal([]byte(content), &obj); err == nil {
			return obj, nil
		}

		// Try JSON array.
		var arr []any
		if err := json.Unmarshal([]byte(content), &arr); err == nil {
			return map[string]any{"items": arr, "text": content}, nil
		}

		// Return as text for the parser to handle.
		return map[string]any{"text": content}, nil
	}

	return nil, nil
}

func (d *brokerAgentDispatcher) pollReply(ctx context.Context, agentSlug, afterID string) (map[string]any, string, error) {
	url := d.brokerAddr + "/messages?channel=" + d.channel
	if afterID != "" {
		url += "&after=" + afterID
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	if d.brokerToken != "" {
		req.Header.Set("Authorization", "Bearer "+d.brokerToken)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", nil
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var result struct {
		Messages []struct {
			ID      string `json:"id"`
			From    string `json:"from"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, "", nil
	}

	var latestID string
	for _, msg := range result.Messages {
		latestID = msg.ID
		if msg.From == agentSlug || msg.From == "@"+agentSlug {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(msg.Content), &parsed); err == nil {
				return parsed, latestID, nil
			}
			return map[string]any{"text": msg.Content}, latestID, nil
		}
	}
	return nil, latestID, nil
}

// hydrateDataSources fetches real data from Composio for each DataSource.
func hydrateDataSources(ctx context.Context, spec workflow.WorkflowSpec, registry *action.Registry, rt *workflow.Runtime) {
	if registry == nil || len(spec.DataSources) == 0 {
		return
	}

	for _, ds := range spec.DataSources {
		provider, err := registry.ProviderFor(action.CapabilityActionExecute)
		if err != nil {
			rt.SetData("/"+ds.ID+"_error", fmt.Sprintf("Provider not available: %v", err))
			continue
		}

		platform := ""
		if parts := strings.SplitN(ds.Action, "_", 2); len(parts) >= 1 {
			platform = strings.ToLower(parts[0])
		}

		req := action.ExecuteRequest{
			Platform:      platform,
			ActionID:      ds.Action,
			ConnectionKey: ds.ConnectionKey,
		}

		result, err := provider.ExecuteAction(ctx, req)
		if err != nil {
			rt.SetData("/"+ds.ID+"_error", fmt.Sprintf("Failed to fetch %s: %v", ds.Action, err))
			continue
		}

		var decoded any
		if len(result.Response) > 0 {
			_ = json.Unmarshal(result.Response, &decoded)
		}
		rt.SetData("/"+ds.ID, decoded)
	}
}
