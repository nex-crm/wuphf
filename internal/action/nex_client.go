package action

import (
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
