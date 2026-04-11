package api

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const agentRunTimeout = 30 * time.Second

// AgentRunApproveRequest is the body for approve (empty).
type AgentRunApproveRequest struct{}

// AgentRunDenyRequest is the body for deny.
type AgentRunDenyRequest struct {
	DenyMessage string `json:"deny_message,omitempty"`
}

// AgentRunRespondRequest is the body for respond.
type AgentRunRespondRequest struct {
	Message string `json:"message"`
}

// AgentRunResponse is the common response shape for HITL actions.
type AgentRunResponse struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// ApproveAgentRun calls POST /v1/agent/runs/{runID}/approve.
func (c *Client) ApproveAgentRun(runID string) (AgentRunResponse, error) {
	return Post[AgentRunResponse](c, "/v1/agent/runs/"+runID+"/approve", AgentRunApproveRequest{}, agentRunTimeout)
}

// DenyAgentRun calls POST /v1/agent/runs/{runID}/deny.
func (c *Client) DenyAgentRun(runID, denyMessage string) (AgentRunResponse, error) {
	return Post[AgentRunResponse](c, "/v1/agent/runs/"+runID+"/deny", AgentRunDenyRequest{DenyMessage: denyMessage}, agentRunTimeout)
}

// RespondToAgentRun calls POST /v1/agent/runs/{runID}/respond.
func (c *Client) RespondToAgentRun(runID, message string) (AgentRunResponse, error) {
	return Post[AgentRunResponse](c, "/v1/agent/runs/"+runID+"/respond", AgentRunRespondRequest{Message: message}, agentRunTimeout)
}

// StopAgentRun calls POST /v1/agent/runs/{runID}/stop.
func (c *Client) StopAgentRun(runID string) (AgentRunResponse, error) {
	return Post[AgentRunResponse](c, "/v1/agent/runs/"+runID+"/stop", nil, agentRunTimeout)
}

// StreamAgentRunEvents opens the SSE stream for a run and returns a channel
// that receives raw data-line values (JSON strings) from the stream.
// The channel is closed when the stream ends or ctx is cancelled.
func (c *Client) StreamAgentRunEvents(ctx context.Context, runID string) (<-chan string, error) {
	url := c.BaseURL + "/v1/agent/runs/" + runID + "/events"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	// Use a fresh client without a timeout so the stream can stay open.
	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SSE connect: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		resp.Body.Close()
		return nil, &AuthError{}
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		resp.Body.Close()
		return nil, &ServerError{Status: resp.StatusCode}
	}

	ch := make(chan string, 16)

	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				select {
				case ch <- data:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}
