package openclaw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// startFakeGateway returns an httptest server that upgrades to WS and answers
// the "connect" request with hello-ok. Additional handlers can be registered
// via onRequest for req/res roundtrip tests in later tasks.
func startFakeGateway(t *testing.T, onRequest func(method string, params json.RawMessage) (payload any, errMsg string)) *httptest.Server {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade: %v", err)
			return
		}
		defer c.Close()
		// Expect connect request.
		_, raw, err := c.ReadMessage()
		if err != nil {
			return
		}
		kind, _, err := DecodeFrame(raw)
		if err != nil || kind != "req" {
			return
		}
		var req RequestFrame
		_ = json.Unmarshal(raw, &req)
		if req.Method != "connect" {
			return
		}
		// Reply hello-ok.
		hello := map[string]any{
			"type":     "hello-ok",
			"protocol": 1,
			"server":   map[string]any{"version": "test", "connId": "c1"},
			"features": map[string]any{"methods": []string{"sessions.list"}, "events": []string{"session.message"}},
			"snapshot": map[string]any{},
			"policy":   map[string]any{"maxPayload": 1024 * 1024, "maxBufferedBytes": 1024 * 1024, "tickIntervalMs": 30000},
		}
		_ = c.WriteJSON(hello)
		// Serve further requests.
		for {
			_, raw, err := c.ReadMessage()
			if err != nil {
				return
			}
			var r RequestFrame
			if err := json.Unmarshal(raw, &r); err != nil {
				continue
			}
			if onRequest == nil {
				continue
			}
			payload, errMsg := onRequest(r.Method, toRawMessage(r.Params))
			res := ResponseFrame{Type: "res", ID: r.ID, OK: errMsg == "", Payload: mustMarshal(payload)}
			if errMsg != "" {
				res.Error = &ErrorShape{Code: "BAD", Message: errMsg}
			}
			_ = c.WriteJSON(res)
		}
	}))
	return srv
}

func toRawMessage(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}
func mustMarshal(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestClientDialHappyPath(t *testing.T) {
	srv := startFakeGateway(t, nil)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{URL: wsURL(srv), Token: "t"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
}

func TestClientRejectsPlaintextNonLoopback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, err := Dial(ctx, Config{URL: "ws://example.com:18789", Token: "t"})
	if err == nil {
		t.Fatal("expected error for ws:// non-loopback")
	}
	if !strings.Contains(err.Error(), "insecure") && !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("expected loopback/insecure error, got %v", err)
	}
}

func TestClientAllowsPlaintextNonLoopbackWhenEnvSet(t *testing.T) {
	t.Setenv("OPENCLAW_ALLOW_INSECURE_PRIVATE_WS", "1")
	// No real server; we only verify Dial accepts the URL past the security check.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := Dial(ctx, Config{URL: "ws://10.0.0.1:18789", Token: "t"})
	if err == nil {
		t.Fatal("expected dial failure (no server); got nil")
	}
	// The error should NOT be the insecure-url message.
	if strings.Contains(err.Error(), "insecure") || strings.Contains(err.Error(), "plaintext") {
		t.Fatalf("env-allowed insecure URL rejected at policy: %v", err)
	}
}
