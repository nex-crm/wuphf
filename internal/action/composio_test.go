package action

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComposioRESTActionHappyPath(t *testing.T) {
	mux := http.NewServeMux()
	var executeUserID string
	mux.HandleFunc("/connected_accounts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id":     "ca_123",
				"status": "ACTIVE",
				"user_id": "cmp_user_123",
				"toolkit": map[string]any{
					"slug": "gmail",
					"name": "Gmail",
				},
				"connection": map[string]any{
					"name": "Founder Gmail",
				},
			}},
		})
	})
	mux.HandleFunc("/connected_accounts/ca_123", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "ca_123",
			"user_id": "cmp_user_123",
			"status":  "ACTIVE",
		})
	})
	mux.HandleFunc("/tools", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"slug":        "GMAIL_SEND_EMAIL",
				"name":        "Send email",
				"description": "Send an email from Gmail",
				"toolkit": map[string]any{
					"slug": "gmail",
				},
			}},
		})
	})
	mux.HandleFunc("/tools/GMAIL_SEND_EMAIL", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"slug":        "GMAIL_SEND_EMAIL",
			"name":        "Send email",
			"description": "Send an email from Gmail",
			"toolkit": map[string]any{
				"slug": "gmail",
			},
			"input_parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to": map[string]any{"type": "string"},
				},
			},
		})
	})
	mux.HandleFunc("/tools/execute/GMAIL_SEND_EMAIL", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if userID, _ := body["user_id"].(string); userID != "" {
			executeUserID = userID
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"successful": true,
			"data": map[string]any{
				"id": "msg-123",
			},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &ComposioREST{
		APIKey:  "cmp_test",
		UserID:  "ceo@example.com",
		BaseURL: server.URL,
		Client:  server.Client(),
	}

	connections, err := client.ListConnections(context.Background(), ListConnectionsOptions{})
	if err != nil {
		t.Fatalf("list connections: %v", err)
	}
	if len(connections.Connections) != 1 || connections.Connections[0].Key != "ca_123" {
		t.Fatalf("unexpected connections %+v", connections)
	}

	search, err := client.SearchActions(context.Background(), "gmail", "send email", "execute")
	if err != nil {
		t.Fatalf("search actions: %v", err)
	}
	if len(search.Actions) != 1 || search.Actions[0].ActionID != "GMAIL_SEND_EMAIL" {
		t.Fatalf("unexpected search %+v", search)
	}

	knowledge, err := client.ActionKnowledge(context.Background(), "gmail", "GMAIL_SEND_EMAIL")
	if err != nil {
		t.Fatalf("knowledge: %v", err)
	}
	if knowledge.Platform != "gmail" || knowledge.ActionID != "GMAIL_SEND_EMAIL" {
		t.Fatalf("unexpected knowledge %+v", knowledge)
	}

	dryRun, err := client.ExecuteAction(context.Background(), ExecuteRequest{
		Platform:      "gmail",
		ActionID:      "GMAIL_SEND_EMAIL",
		ConnectionKey: "ca_123",
		Data:          map[string]any{"to": "ceo@example.com"},
		DryRun:        true,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !dryRun.DryRun || dryRun.Request.URL == "" {
		t.Fatalf("unexpected dry run %+v", dryRun)
	}

	result, err := client.ExecuteAction(context.Background(), ExecuteRequest{
		Platform:      "gmail",
		ActionID:      "GMAIL_SEND_EMAIL",
		ConnectionKey: "ca_123",
		Data:          map[string]any{"to": "ceo@example.com"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.DryRun || len(result.Response) == 0 {
		t.Fatalf("unexpected execute %+v", result)
	}
	if executeUserID != "cmp_user_123" {
		t.Fatalf("expected resolved composio user id cmp_user_123, got %q", executeUserID)
	}
}
