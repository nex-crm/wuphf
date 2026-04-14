package action

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
)

type nexAskResponse struct {
	Answer    string `json:"answer"`
	SessionID string `json:"session_id"`
}

type nexInsightItem struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

type nexInsightsResponse struct {
	Insights []nexInsightItem `json:"insights"`
}

func nexClientFromConfig() (*api.Client, error) {
	apiKey := strings.TrimSpace(config.ResolveAPIKey(""))
	if apiKey == "" {
		return nil, fmt.Errorf("nex is not configured")
	}
	client := api.NewClient(apiKey)
	if !client.IsAuthenticated() {
		return nil, fmt.Errorf("nex is not configured")
	}
	return client, nil
}

func nexAsk(query string) (nexAskResponse, error) {
	client, err := nexClientFromConfig()
	if err != nil {
		return nexAskResponse{}, err
	}
	return api.Post[nexAskResponse](client, "/v1/context/ask", map[string]any{"query": strings.TrimSpace(query)}, 0)
}

// FetchEntityBrief asks Nex for context relevant to the given work notification
// and returns a formatted brief string to prepend to the agent's stdin input.
// Returns an empty string when Nex is disabled, not configured, or the call
// fails — callers should always append the original notification regardless.
// The provided context is used for the request timeout.
func FetchEntityBrief(ctx context.Context, notification string) string {
	if config.ResolveNoNex() {
		return ""
	}
	query := strings.TrimSpace(notification)
	if query == "" {
		return ""
	}
	// Summarise the notification into a concise Nex query.
	// We limit the query length to avoid runaway token usage.
	if len(query) > 400 {
		query = query[:400]
	}
	query = "Given this work item, what company context, contacts, or recent activity is most relevant? Work item: " + query

	type result struct {
		answer string
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := nexAsk(query)
		ch <- result{resp.Answer, err}
	}()

	select {
	case <-ctx.Done():
		return ""
	case r := <-ch:
		if r.err != nil || strings.TrimSpace(r.answer) == "" {
			return ""
		}
		return "== NEX CONTEXT ==\n" + strings.TrimSpace(r.answer) + "\n== END NEX CONTEXT =="
	}
}

func nexInsightsSince(since time.Time, limit int) (nexInsightsResponse, error) {
	client, err := nexClientFromConfig()
	if err != nil {
		return nexInsightsResponse{}, err
	}
	if limit <= 0 {
		limit = 5
	}
	q := url.Values{}
	q.Set("from", since.UTC().Format(time.RFC3339))
	q.Set("to", time.Now().UTC().Format(time.RFC3339))
	q.Set("limit", strconv.Itoa(limit))
	return api.Get[nexInsightsResponse](client, "/v1/insights?"+q.Encode(), 0)
}
