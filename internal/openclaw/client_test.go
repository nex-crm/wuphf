package openclaw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// startFakeGateway returns an httptest server that mirrors the real OpenClaw
// handshake closely enough for client tests: it pushes connect.challenge first,
// accepts the device-authed connect request, and replies with a wrapped res
// frame whose payload is `hello-ok`. Additional method handlers can be
// registered via onRequest.
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
		// 1. Push connect.challenge FIRST — the real daemon does this before
		// reading anything from the client.
		_ = c.WriteJSON(map[string]any{
			"type":    "event",
			"event":   "connect.challenge",
			"payload": map[string]any{"nonce": "test-nonce", "ts": time.Now().UnixMilli()},
		})
		// 2. Expect connect request.
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
		// 3. Reply res(hello-ok) — matching the real server at
		// dist/server.impl:22706 which wraps the hello-ok body inside a
		// res frame rather than sending it as a top-level frame.
		_ = c.WriteJSON(map[string]any{
			"type": "res",
			"id":   req.ID,
			"ok":   true,
			"payload": map[string]any{
				"type":     "hello-ok",
				"protocol": 3,
				"server":   map[string]any{"version": "test", "connId": "c1"},
				"features": map[string]any{"methods": []string{"sessions.list"}, "events": []string{"session.message"}},
				"snapshot": map[string]any{},
				"policy":   map[string]any{"maxPayload": 1024 * 1024, "maxBufferedBytes": 1024 * 1024, "tickIntervalMs": 30000},
			},
		})
		// 4. Serve further requests.
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

// testIdentity returns a fresh throwaway DeviceIdentity rooted in t.TempDir().
// Each test gets its own file so the path is writeable under the sandbox.
func testIdentity(t *testing.T) *DeviceIdentity {
	t.Helper()
	id, err := LoadOrCreateDeviceIdentity(filepath.Join(t.TempDir(), "identity.json"))
	if err != nil {
		t.Fatalf("LoadOrCreateDeviceIdentity: %v", err)
	}
	return id
}

func TestClientDialHappyPath(t *testing.T) {
	srv := startFakeGateway(t, nil)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{URL: wsURL(srv), Token: "t", Identity: testIdentity(t)})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
}

func TestClientRejectsPlaintextNonLoopback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, err := Dial(ctx, Config{URL: "ws://example.com:18789", Token: "t", Identity: testIdentity(t)})
	if err == nil {
		t.Fatal("expected error for ws:// non-loopback")
	}
	if !strings.Contains(err.Error(), "insecure") && !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("expected loopback/insecure error, got %v", err)
	}
}

func TestClientAllowsPlaintextNonLoopbackWhenEnvSet(t *testing.T) {
	t.Setenv("OPENCLAW_ALLOW_INSECURE_PRIVATE_WS", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := Dial(ctx, Config{URL: "ws://10.0.0.1:18789", Token: "t", Identity: testIdentity(t)})
	if err == nil {
		t.Fatal("expected dial failure (no server); got nil")
	}
	if strings.Contains(err.Error(), "insecure") || strings.Contains(err.Error(), "plaintext") {
		t.Fatalf("env-allowed insecure URL rejected at policy: %v", err)
	}
}

func TestDialRequiresDeviceIdentity(t *testing.T) {
	// OpenClaw grants zero scopes to token-only clients, so the bridge MUST
	// pass an identity — we'd rather fail fast with a clear error than hit
	// cryptic missing-scope errors on every session method.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := Dial(ctx, Config{URL: "ws://127.0.0.1:1", Token: "t"})
	if err == nil {
		t.Fatal("expected error when Identity is nil")
	}
	if !strings.Contains(err.Error(), "DeviceIdentity") {
		t.Fatalf("expected DeviceIdentity error, got %v", err)
	}
}

func TestClientCallRoundTrip(t *testing.T) {
	srv := startFakeGateway(t, func(method string, params json.RawMessage) (any, string) {
		if method == "sessions.list" {
			return map[string]any{"sessions": []any{}, "path": "/tmp/x"}, ""
		}
		return nil, "unknown method"
	})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{URL: wsURL(srv), Token: "t", Identity: testIdentity(t)})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	payload, err := c.Call(ctx, "sessions.list", map[string]any{"limit": 10})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var got struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Path != "/tmp/x" {
		t.Fatalf("path: %q", got.Path)
	}
}

func TestClientCallServerError(t *testing.T) {
	srv := startFakeGateway(t, func(method string, params json.RawMessage) (any, string) {
		return nil, "no such method"
	})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{URL: wsURL(srv), Token: "t", Identity: testIdentity(t)})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	_, err = c.Call(ctx, "bogus", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var ge *GatewayError
	if !errors.As(err, &ge) {
		t.Fatalf("expected GatewayError, got %T: %v", err, err)
	}
	if ge.Code != "BAD" {
		t.Fatalf("code: %q", ge.Code)
	}
}

func TestClientCallContextCancel(t *testing.T) {
	srv := startFakeGateway(t, func(method string, params json.RawMessage) (any, string) {
		time.Sleep(2 * time.Second)
		return nil, ""
	})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{URL: wsURL(srv), Token: "t", Identity: testIdentity(t)})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()
	callCtx, callCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer callCancel()
	_, err = c.Call(callCtx, "slow.method", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
