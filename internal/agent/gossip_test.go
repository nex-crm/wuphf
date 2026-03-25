package agent_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
)

func newGossipTestClient(serverURL string) *api.Client {
	c := api.NewClient("test-key")
	c.BaseURL = serverURL
	return c
}

func TestGossipLayer_Publish(t *testing.T) {
	var gotBody map[string]any
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	gl := agent.NewGossipLayer(newGossipTestClient(srv.URL))
	result, err := gl.Publish("planner", "deployment is stable", "production check")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	if gotPath != "/remember" {
		t.Errorf("expected POST /remember, got %s", gotPath)
	}

	content, _ := gotBody["content"].(string)
	if !strings.Contains(content, "[agent:planner]") {
		t.Errorf("expected content to contain agent prefix, got: %q", content)
	}
	if !strings.Contains(content, "deployment is stable") {
		t.Errorf("expected content to contain insight, got: %q", content)
	}
}

func TestGossipLayer_Query_FiltersOwnInsights(t *testing.T) {
	now := time.Now().UnixMilli()
	mockResults := map[string]any{
		"results": []any{
			map[string]any{
				"content":   "[agent:planner] own insight — should be filtered",
				"relevance": 0.9,
				"timestamp": now,
			},
			map[string]any{
				"content":   "[agent:researcher] external insight",
				"relevance": 0.8,
				"timestamp": now,
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResults)
	}))
	defer srv.Close()

	gl := agent.NewGossipLayer(newGossipTestClient(srv.URL))
	insights, err := gl.Query("planner", "deployment")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(insights) != 1 {
		t.Fatalf("expected 1 insight (own filtered), got %d", len(insights))
	}
	if insights[0].Source != "researcher" {
		t.Errorf("expected source 'researcher', got %q", insights[0].Source)
	}
	if insights[0].Content != "external insight" {
		t.Errorf("expected content 'external insight', got %q", insights[0].Content)
	}
}

func TestGossipLayer_Query_SendsCorrectPayload(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer srv.Close()

	gl := agent.NewGossipLayer(newGossipTestClient(srv.URL))
	_, err := gl.Query("planner", "architecture")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	query, _ := gotBody["query"].(string)
	if !strings.Contains(query, "[gossip]") {
		t.Errorf("expected query to contain [gossip] prefix, got: %q", query)
	}
	if !strings.Contains(query, "architecture") {
		t.Errorf("expected query to contain topic, got: %q", query)
	}

	limit, _ := gotBody["limit"].(float64)
	if limit != 10 {
		t.Errorf("expected limit 10, got %v", limit)
	}
}

func TestGossipLayer_Query_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer srv.Close()

	gl := agent.NewGossipLayer(newGossipTestClient(srv.URL))
	insights, err := gl.Query("planner", "nothing")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(insights) != 0 {
		t.Errorf("expected 0 insights, got %d", len(insights))
	}
}
