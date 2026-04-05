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
	body := fmt.Sprintf(`{"from":"you","channel":%q,"content":%q,"tagged":[%q]}`,
		d.channel, prompt, "@"+agentSlug)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.brokerAddr+"/messages", strings.NewReader(body))
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

	// Poll for the agent's reply.
	deadline := time.Now().Add(d.timeout)
	lastID := ""
	for time.Now().Before(deadline) {
		reply, id, err := d.pollReply(ctx, agentSlug, lastID)
		if err != nil {
			return nil, err
		}
		if reply != nil {
			return reply, nil
		}
		if id != "" {
			lastID = id
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("agent %q did not respond within %s", agentSlug, d.timeout)
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
