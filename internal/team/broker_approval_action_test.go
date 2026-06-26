package team

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// An approval request carrying the structured action payload stores it with the
// raw envelope re-masked and the internal connection key stripped, and records
// the connection-unverified signal — the broker side of slice 4b / review LOW #5.
func TestApprovalRequestStoresMaskedStructuredAction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))

	body, _ := json.Marshal(map[string]any{
		"action":                "create",
		"kind":                  "approval",
		"from":                  "ceo",
		"channel":               "general",
		"title":                 "Send Email via Gmail",
		"question":              "Approve?",
		"blocking":              true,
		"required":              true,
		"connection_unverified": true,
		"integration_action": map[string]any{
			"platform":  "gmail",
			"action_id": "GMAIL_SEND_EMAIL",
			"verb":      "Send Email",
			"name":      "Gmail",
			"account":   map[string]any{"name": "Founder Gmail", "key": "ca_secret_123"},
			"raw_envelope": map[string]any{
				"method": "POST",
				"url":    "https://backend.composio.dev/api/v3/tools/execute",
				"data": map[string]any{
					"to":    "lead@acme.com",
					"token": "super-secret-value",
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/requests", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	b.handlePostRequest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	if len(b.requests) != 1 {
		t.Fatalf("expected 1 stored request, got %d", len(b.requests))
	}
	got := b.requests[0]
	if !got.ConnectionUnverified {
		t.Fatalf("connection_unverified not stored")
	}
	if got.Action == nil {
		t.Fatalf("structured action payload not stored")
	}
	if got.Action.Platform != "gmail" || got.Action.ActionID != "GMAIL_SEND_EMAIL" || got.Action.Verb != "Send Email" {
		t.Fatalf("action identity wrong: %+v", got.Action)
	}
	// The internal connection key is stripped; the friendly name survives.
	if got.Action.Account == nil || got.Action.Account.Key != "" || got.Action.Account.Name != "Founder Gmail" {
		t.Fatalf("account not sanitized: %+v", got.Action.Account)
	}
	// The secret in the envelope body is masked; the non-secret is untouched.
	if got.Action.RawEnvelope == nil {
		t.Fatalf("raw envelope missing")
	}
	if v := got.Action.RawEnvelope.Data["token"]; v != "***" {
		t.Fatalf("token not masked in stored envelope: %v", v)
	}
	if v := got.Action.RawEnvelope.Data["to"]; v != "lead@acme.com" {
		t.Fatalf("non-secret altered in stored envelope: %v", v)
	}
	if got.Action.RawEnvelope.Method != "POST" {
		t.Fatalf("envelope method dropped: %+v", got.Action.RawEnvelope)
	}
}
