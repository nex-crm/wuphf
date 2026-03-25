package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/api"
)

// GossipInsight is a single insight received from the gossip network.
type GossipInsight struct {
	Content   string  `json:"content"`
	Source    string  `json:"source"`
	Timestamp int64   `json:"timestamp"`
	Relevance float64 `json:"relevance"`
}

// GossipLayer provides cross-agent knowledge sharing via the Nex API.
type GossipLayer struct {
	client *api.Client
}

// NewGossipLayer creates a GossipLayer backed by the given API client.
func NewGossipLayer(client *api.Client) *GossipLayer {
	return &GossipLayer{client: client}
}

// Publish stores an insight in the knowledge base tagged for gossip consumption.
// The content is prefixed with "[agent:<agentSlug>]" so other agents can filter it.
func (g *GossipLayer) Publish(agentSlug, insight string, ctx string) (string, error) {
	content := fmt.Sprintf("[agent:%s] %s", agentSlug, insight)
	result, err := api.Post[map[string]any](g.client, "/remember", map[string]any{
		"content": content,
		"tags":    []string{"gossip", "agent:" + agentSlug},
	}, 0)
	if err != nil {
		return "", fmt.Errorf("gossip publish: %w", err)
	}
	b, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal publish result: %w", err)
	}
	return string(b), nil
}

// Query retrieves gossip insights about a topic, excluding the querying agent's own insights.
func (g *GossipLayer) Query(agentSlug, topic string) ([]GossipInsight, error) {
	type searchResult struct {
		Content   string  `json:"content"`
		Relevance float64 `json:"relevance"`
		Timestamp int64   `json:"timestamp"`
	}
	type searchResponse struct {
		Results []searchResult `json:"results"`
	}

	resp, err := api.Post[searchResponse](g.client, "/search", map[string]any{
		"query": "[gossip] " + topic,
		"limit": 10,
	}, 0)
	if err != nil {
		return nil, fmt.Errorf("gossip query: %w", err)
	}

	selfPrefix := "[agent:" + agentSlug + "]"
	var insights []GossipInsight
	for _, r := range resp.Results {
		if strings.HasPrefix(r.Content, selfPrefix) {
			continue
		}

		source := ""
		content := r.Content
		if strings.HasPrefix(r.Content, "[agent:") {
			end := strings.Index(r.Content, "]")
			if end > 0 {
				source = r.Content[7:end] // strip "[agent:"
				content = strings.TrimSpace(r.Content[end+1:])
			}
		}

		ts := r.Timestamp
		if ts == 0 {
			ts = time.Now().UnixMilli()
		}

		insights = append(insights, GossipInsight{
			Content:   content,
			Source:    source,
			Timestamp: ts,
			Relevance: r.Relevance,
		})
	}
	return insights, nil
}
