package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/openclaw"
)

// TestOpenclawBridgeFullPipeline_E2E exercises the OpenClaw bridge end-to-end
// against a fake gateway running over a real WebSocket. It proves:
//
//  1. Real openclaw.Dial → connect.challenge + device-authed connect + wrapped hello-ok
//  2. NewOpenclawBridgeWithDialer + Start → bridge subscribes bound sessions
//  3. Bridge.OnOfficeMessage → real sessions.send frame delivered to gateway
//  4. Gateway pushes session.message event with assistant role → broker gets it
//  5. Gateway pushes session.message with user role → broker is NOT echoed
//
// No mocks of the protocol layer. Real bytes flow over real sockets through
// the real openclaw.Client.
func TestOpenclawBridgeFullPipeline_E2E(t *testing.T) {
	gw := startFakeOpenclawGatewayE2E(t)
	defer gw.Close()

	identity, err := openclaw.LoadOrCreateDeviceIdentity(filepath.Join(t.TempDir(), "identity.json"))
	if err != nil {
		t.Fatalf("identity: %v", err)
	}

	broker := NewBroker()
	bindings := []config.OpenclawBridgeBinding{
		{SessionKey: "agent:e2e:demo", Slug: "openclaw-demo-e2e", DisplayName: "Demo"},
	}
	dialer := func(ctx context.Context) (openclawClient, error) {
		return openclaw.Dial(ctx, openclaw.Config{URL: gw.URL(), Token: "test-token", Identity: identity})
	}
	bridge := NewOpenclawBridgeWithDialer(broker, nil, dialer, bindings)
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("Start bridge: %v", err)
	}
	defer bridge.Stop()

	// Wait for supervisor → dial → subscribe.
	waitForE2E(t, 3*time.Second, func() bool {
		return gw.subscriptionCount("agent:e2e:demo") >= 1
	}, "supervisor never subscribed agent:e2e:demo")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Outbound: bridge → gateway sessions.send
	if err := bridge.OnOfficeMessage(ctx, "openclaw-demo-e2e", "hello agent"); err != nil {
		t.Fatalf("OnOfficeMessage: %v", err)
	}
	waitForE2E(t, 2*time.Second, func() bool {
		return gw.lastSendForKey("agent:e2e:demo") == "hello agent"
	}, "gateway never received hello agent")

	// Inbound user echo: gateway pushes our own message back. Bridge MUST NOT
	// re-post it (otherwise every outbound turn double-fires in #general).
	beforeEchoes := countMessagesFrom(broker, "openclaw-demo-e2e", "hello agent")
	gw.pushUserMessage("agent:e2e:demo", "hello agent")
	time.Sleep(150 * time.Millisecond)
	if got := countMessagesFrom(broker, "openclaw-demo-e2e", "hello agent"); got != beforeEchoes {
		t.Fatalf("bridge re-posted a user-role echo; before=%d after=%d", beforeEchoes, got)
	}

	// Inbound assistant reply: gateway → bridge → broker message.
	wantContent := "hi from openclaw, tell Michael Scott we're back"
	beforeCount := countMessagesFrom(broker, "openclaw-demo-e2e", wantContent)
	gw.pushAssistantMessage("agent:e2e:demo", wantContent)
	waitForE2E(t, 2*time.Second, func() bool {
		return countMessagesFrom(broker, "openclaw-demo-e2e", wantContent) > beforeCount
	}, "assistant event never appeared as broker message")
}

func countMessagesFrom(b *Broker, slug, contains string) int {
	n := 0
	for _, m := range b.AllMessages() {
		if m.From == slug && strings.Contains(m.Content, contains) {
			n++
		}
	}
	return n
}

// fakeOpenclawGatewayE2E implements the subset of OpenClaw Gateway protocol
// the WUPHF bridge actually hits, matching the observed real-daemon shape:
//
//   - connect.challenge event pushed BEFORE reading client
//   - wrapped res(hello-ok) as the connect response (protocol 3)
//   - sessions.list uses "key" as the session identifier
//   - session.message events use role + content parts (not state/content)
type fakeOpenclawGatewayE2E struct {
	srv          *httptest.Server
	mu           sync.Mutex
	subscribed   map[string][]*fakeOCGwConn
	subsCount    map[string]int
	sentMessages map[string]string
	conns        []*fakeOCGwConn
	seq          int64
}

type fakeOCGwConn struct {
	c       *websocket.Conn
	writeMu sync.Mutex
}

func startFakeOpenclawGatewayE2E(t *testing.T) *fakeOpenclawGatewayE2E {
	g := &fakeOpenclawGatewayE2E{
		subscribed:   make(map[string][]*fakeOCGwConn),
		subsCount:    make(map[string]int),
		sentMessages: make(map[string]string),
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade: %v", err)
			return
		}
		fc := &fakeOCGwConn{c: c}
		g.mu.Lock()
		g.conns = append(g.conns, fc)
		g.mu.Unlock()
		g.serve(fc)
	}))
	return g
}

func (g *fakeOpenclawGatewayE2E) URL() string {
	return "ws" + strings.TrimPrefix(g.srv.URL, "http")
}

func (g *fakeOpenclawGatewayE2E) Close() { g.srv.Close() }

func (g *fakeOpenclawGatewayE2E) serve(fc *fakeOCGwConn) {
	defer fc.c.Close()

	// 1. Push connect.challenge first.
	fc.write(map[string]any{
		"type":    "event",
		"event":   "connect.challenge",
		"payload": map[string]any{"nonce": "e2e-nonce", "ts": time.Now().UnixMilli()},
	})

	// 2. Read connect.
	_, raw, err := fc.c.ReadMessage()
	if err != nil {
		return
	}
	var req struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Method string `json:"method"`
	}
	_ = json.Unmarshal(raw, &req)
	if req.Method != "connect" {
		return
	}

	// 3. Reply res(hello-ok) — wrapped.
	fc.write(map[string]any{
		"type": "res",
		"id":   req.ID,
		"ok":   true,
		"payload": map[string]any{
			"type":     "hello-ok",
			"protocol": 3,
			"server":   map[string]any{"version": "fake", "connId": "fc-1"},
			"features": map[string]any{
				"methods": []string{"sessions.list", "sessions.send", "sessions.messages.subscribe", "sessions.messages.unsubscribe"},
				"events":  []string{"session.message", "sessions.changed"},
			},
			"snapshot": map[string]any{},
			"policy":   map[string]any{"maxPayload": 1 << 20, "maxBufferedBytes": 1 << 20, "tickIntervalMs": 30000},
		},
	})

	for {
		_, raw, err := fc.c.ReadMessage()
		if err != nil {
			return
		}
		var r struct {
			Type   string          `json:"type"`
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			continue
		}
		switch r.Method {
		case "sessions.list":
			g.respond(fc, r.ID, true, map[string]any{"sessions": []any{
				map[string]any{"key": "agent:e2e:demo", "label": "Demo", "displayName": "Demo Agent", "kind": "direct"},
			}, "path": "/tmp/fake", "count": 1}, nil)
		case "sessions.send":
			var p struct {
				Key     string `json:"key"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal(r.Params, &p)
			g.mu.Lock()
			g.sentMessages[p.Key] = p.Message
			g.mu.Unlock()
			g.respond(fc, r.ID, true, map[string]any{"runId": "run-" + r.ID, "status": "started", "messageSeq": 1}, nil)
		case "sessions.messages.subscribe":
			var p struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal(r.Params, &p)
			g.mu.Lock()
			g.subscribed[p.Key] = append(g.subscribed[p.Key], fc)
			g.subsCount[p.Key]++
			g.mu.Unlock()
			g.respond(fc, r.ID, true, map[string]any{"ok": true}, nil)
		default:
			g.respond(fc, r.ID, false, nil, map[string]any{"code": "UNKNOWN", "message": "method not implemented in fake"})
		}
	}
}

func (g *fakeOpenclawGatewayE2E) respond(fc *fakeOCGwConn, id string, ok bool, payload any, errShape any) {
	res := map[string]any{"type": "res", "id": id, "ok": ok}
	if payload != nil {
		res["payload"] = payload
	}
	if errShape != nil {
		res["error"] = errShape
	}
	fc.write(res)
}

func (fc *fakeOCGwConn) write(v any) {
	fc.writeMu.Lock()
	defer fc.writeMu.Unlock()
	_ = fc.c.WriteJSON(v)
}

func (g *fakeOpenclawGatewayE2E) subscriptionCount(key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.subsCount[key]
}

func (g *fakeOpenclawGatewayE2E) lastSendForKey(key string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sentMessages[key]
}

// pushAssistantMessage emits the real-daemon shape for an agent reply: role
// "assistant" with content as an array of {type,text} parts.
func (g *fakeOpenclawGatewayE2E) pushAssistantMessage(sessionKey, text string) {
	g.pushEvent(sessionKey, map[string]any{
		"role":      "assistant",
		"content":   []any{map[string]any{"type": "text", "text": text}},
		"timestamp": time.Now().UnixMilli(),
	})
}

// pushUserMessage emits the shape the server uses to echo our own outbound
// messages — a user role with a plain string content.
func (g *fakeOpenclawGatewayE2E) pushUserMessage(sessionKey, text string) {
	g.pushEvent(sessionKey, map[string]any{
		"role":      "user",
		"content":   text,
		"timestamp": time.Now().UnixMilli(),
	})
}

func (g *fakeOpenclawGatewayE2E) pushEvent(sessionKey string, message map[string]any) {
	g.mu.Lock()
	g.seq++
	subs := g.subscribed[sessionKey]
	seq := g.seq
	g.mu.Unlock()
	evt := map[string]any{
		"type":  "event",
		"event": "session.message",
		"seq":   seq,
		"payload": map[string]any{
			"sessionKey": sessionKey,
			"messageSeq": seq,
			"message":    message,
		},
	}
	for _, fc := range subs {
		fc.write(evt)
	}
}

func waitForE2E(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waitForE2E timed out: %s", msg)
}
