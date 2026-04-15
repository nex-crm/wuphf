package openclaw

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestSessionsListSerialization(t *testing.T) {
	var captured RequestFrame
	srv := startFakeGateway(t, func(method string, params json.RawMessage) (any, string) {
		if method == "sessions.list" {
			captured.Method = method
			_ = json.Unmarshal(params, &captured.Params)
			return map[string]any{"sessions": []any{map[string]any{"sessionKey": "k", "label": "Agent"}}, "path": "/p"}, ""
		}
		return nil, "unknown"
	})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{URL: wsURL(srv), Token: "t"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	filter := SessionsListFilter{Limit: 10, ActiveMinutes: 60, Kinds: []string{"main"}}
	rows, err := c.SessionsList(ctx, filter)
	if err != nil {
		t.Fatalf("SessionsList: %v", err)
	}
	if len(rows) != 1 || rows[0].SessionKey != "k" {
		t.Fatalf("rows: %+v", rows)
	}
	// Check the params were sent.
	pm, _ := captured.Params.(map[string]any)
	if pm == nil || pm["limit"].(float64) != 10 {
		t.Fatalf("params: %+v", captured.Params)
	}
}

func TestSessionsSendIncludesIdempotencyKey(t *testing.T) {
	var got map[string]any
	srv := startFakeGateway(t, func(method string, params json.RawMessage) (any, string) {
		if method == "sessions.send" {
			_ = json.Unmarshal(params, &got)
			return map[string]any{"ok": true}, ""
		}
		return nil, "unknown"
	})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{URL: wsURL(srv), Token: "t"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	if err := c.SessionsSend(ctx, "agent:main:main", "hello", "idem-1"); err != nil {
		t.Fatalf("SessionsSend: %v", err)
	}
	if got["idempotencyKey"] != "idem-1" {
		t.Fatalf("idempotencyKey missing: %+v", got)
	}
	if got["key"] != "agent:main:main" || got["message"] != "hello" {
		t.Fatalf("params: %+v", got)
	}
}

func TestSessionsMessagesSubscribe(t *testing.T) {
	called := 0
	srv := startFakeGateway(t, func(method string, params json.RawMessage) (any, string) {
		if method == "sessions.messages.subscribe" {
			called++
			return map[string]any{"ok": true}, ""
		}
		return nil, "unknown"
	})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{URL: wsURL(srv), Token: "t"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	if err := c.SessionsMessagesSubscribe(ctx, "k"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected 1 call, got %d", called)
	}
}
