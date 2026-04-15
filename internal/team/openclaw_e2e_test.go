package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/openclaw"
)

// TestOpenclawBridgeFullPipeline_E2E exercises the OpenClaw bridge end-to-end
// against a fake gateway running over a real WebSocket. It proves the full data
// flow:
//
//   1. Real openclaw.Dial → real WS handshake against fake gateway
//   2. NewOpenclawBridgeWithDialer + Start → bridge subscribes bound sessions
//   3. Bridge.OnOfficeMessage → real sessions.send frame delivered to gateway
//   4. Gateway pushes session.message event → bridge routes to broker
//   5. Outbound + inbound + delta + final all observable from the broker side
//
// No mocks of the protocol layer. Real bytes flow over real sockets through
// the real openclaw.Client.
func TestOpenclawBridgeFullPipeline_E2E(t *testing.T) {
	gw := startFakeOpenclawGatewayE2E(t)
	defer gw.Close()

	broker := NewBroker()
	bindings := []config.OpenclawBridgeBinding{
		{SessionKey: "agent:e2e:demo", Slug: "openclaw-demo-e2e", DisplayName: "Demo"},
	}
	dialer := func(ctx context.Context) (openclawClient, error) {
		return openclaw.Dial(ctx, openclaw.Config{URL: gw.URL(), Token: "test-token"})
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

	// Inbound delta: gateway → bridge → AgentStream
	gw.pushDelta("agent:e2e:demo", "thinking…")
	waitForE2E(t, 2*time.Second, func() bool {
		buf := broker.AgentStream("openclaw-demo-e2e")
		return buf != nil && len(buf.recent()) > 0
	}, "delta event never reached AgentStream")

	// Inbound final: gateway → bridge → broker message
	wantContent := "hi from openclaw, tell Michael Scott we're back"
	beforeCount := countMessagesFrom(broker, "openclaw-demo-e2e", wantContent)
	gw.pushFinal("agent:e2e:demo", wantContent)
	waitForE2E(t, 2*time.Second, func() bool {
		return countMessagesFrom(broker, "openclaw-demo-e2e", wantContent) > beforeCount
	}, "final event never appeared as broker message")
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

// fakeOpenclawGatewayE2E implements the minimum OpenClaw Gateway protocol the
// WUPHF bridge needs: hello-ok handshake, sessions.list, sessions.send,
// sessions.messages.subscribe, sessions.history. Tests can push session.message
// events into subscribed connections via pushFinal/pushDelta.
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
	// Expect connect.
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
	hello := map[string]any{
		"type":     "hello-ok",
		"protocol": 1,
		"server":   map[string]any{"version": "fake", "connId": "fc-1"},
		"features": map[string]any{"methods": []string{"sessions.list", "sessions.send", "sessions.messages.subscribe", "sessions.history"}, "events": []string{"session.message"}},
		"snapshot": map[string]any{},
		"policy":   map[string]any{"maxPayload": 1 << 20, "maxBufferedBytes": 1 << 20, "tickIntervalMs": 30000},
	}
	fc.write(hello)

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
				map[string]any{"sessionKey": "agent:e2e:demo", "label": "Demo", "displayName": "Demo Agent"},
			}, "path": "/tmp/fake"}, nil)
		case "sessions.send":
			var p struct {
				Key     string `json:"key"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal(r.Params, &p)
			g.mu.Lock()
			g.sentMessages[p.Key] = p.Message
			g.mu.Unlock()
			g.respond(fc, r.ID, true, map[string]any{"ok": true}, nil)
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
		case "sessions.history":
			g.respond(fc, r.ID, true, map[string]any{"messages": []any{}}, nil)
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

func (g *fakeOpenclawGatewayE2E) pushFinal(sessionKey, content string) {
	g.pushEvent(sessionKey, "final", content)
}

func (g *fakeOpenclawGatewayE2E) pushDelta(sessionKey, content string) {
	g.pushEvent(sessionKey, "delta", content)
}

func (g *fakeOpenclawGatewayE2E) pushEvent(sessionKey, state, content string) {
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
			"message":    map[string]any{"state": state, "content": content},
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
