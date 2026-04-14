package openclaw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Config configures a Client.
type Config struct {
	URL         string // e.g. ws://127.0.0.1:18789 or wss://host:18789
	Token       string // shared secret (from wuphf config or env)
	ClientID    string // defaults to "wuphf"
	UserAgent   string // optional
	DialTimeout time.Duration
}

// Client is a long-lived connection to the OpenClaw Gateway.
type Client struct {
	cfg      Config
	conn     *websocket.Conn
	events   chan ClientEvent
	pending  map[string]chan ResponseFrame
	mu       sync.Mutex
	closed   bool
	closeErr error
	nextID   uint64
	lastSeq  map[string]int64 // per-session last seen event seq
}

// Dial establishes a connection and completes the hello handshake.
func Dial(ctx context.Context, cfg Config) (*Client, error) {
	if err := enforceTransportSecurity(cfg.URL); err != nil {
		return nil, err
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	if cfg.ClientID == "" {
		cfg.ClientID = "wuphf"
	}
	dialer := websocket.Dialer{HandshakeTimeout: cfg.DialTimeout}
	dctx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
	defer cancel()
	conn, _, err := dialer.DialContext(dctx, cfg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("openclaw dial: %w", err)
	}
	c := &Client{
		cfg:     cfg,
		conn:    conn,
		events:  make(chan ClientEvent, 64),
		pending: make(map[string]chan ResponseFrame),
		lastSeq: make(map[string]int64),
	}
	if err := c.doHandshake(dctx); err != nil {
		conn.Close()
		return nil, err
	}
	go c.readLoop()
	return c, nil
}

// enforceTransportSecurity blocks ws:// to non-loopback hosts unless env-allowed.
func enforceTransportSecurity(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("openclaw: invalid url %q: %w", rawURL, err)
	}
	if u.Scheme == "wss" {
		return nil
	}
	if u.Scheme != "ws" {
		return fmt.Errorf("openclaw: unsupported scheme %q", u.Scheme)
	}
	host := u.Hostname()
	if isLoopbackHost(host) {
		return nil
	}
	if os.Getenv("OPENCLAW_ALLOW_INSECURE_PRIVATE_WS") == "1" {
		return nil
	}
	return fmt.Errorf("openclaw: plaintext ws:// to non-loopback host %q is insecure; use wss:// or set OPENCLAW_ALLOW_INSECURE_PRIVATE_WS=1 on a trusted LAN", host)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) doHandshake(ctx context.Context) error {
	connectReq := RequestFrame{
		Type:   "req",
		ID:     c.newID(),
		Method: "connect",
		Params: map[string]any{
			"minProtocol": 1,
			"maxProtocol": 2,
			"client": map[string]any{
				"id":       c.cfg.ClientID,
				"version":  "0.1",
				"platform": runtimeOS(),
				"mode":     "operator",
			},
			"auth": map[string]any{"token": c.cfg.Token},
		},
	}
	if err := c.conn.WriteJSON(connectReq); err != nil {
		return fmt.Errorf("openclaw: write connect: %w", err)
	}
	// Expect hello-ok.
	_, raw, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("openclaw: read hello-ok: %w", err)
	}
	var hello struct {
		Type     string      `json:"type"`
		Protocol int         `json:"protocol"`
		Error    *ErrorShape `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &hello); err != nil {
		return fmt.Errorf("openclaw: decode hello-ok: %w", err)
	}
	if hello.Type != "hello-ok" {
		if hello.Error != nil {
			return fmt.Errorf("openclaw: handshake refused: %s: %s", hello.Error.Code, hello.Error.Message)
		}
		return fmt.Errorf("openclaw: unexpected handshake frame type %q", hello.Type)
	}
	if hello.Protocol < 1 {
		return fmt.Errorf("openclaw: unsupported protocol %d", hello.Protocol)
	}
	return nil
}

func (c *Client) readLoop() {
	defer func() {
		c.mu.Lock()
		if !c.closed {
			c.closed = true
			for _, ch := range c.pending {
				close(ch)
			}
			c.pending = nil
		}
		c.mu.Unlock()
		c.events <- ClientEvent{Kind: EventKindClose, CloseErr: c.closeErr}
		close(c.events)
	}()
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) && !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				c.closeErr = err
			}
			return
		}
		kind, payload, err := DecodeFrame(raw)
		if err != nil {
			continue
		}
		switch kind {
		case "res":
			var res ResponseFrame
			if err := json.Unmarshal(payload, &res); err != nil {
				continue
			}
			c.mu.Lock()
			ch := c.pending[res.ID]
			delete(c.pending, res.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- res
				close(ch)
			}
		case "event":
			var evt EventFrame
			if err := json.Unmarshal(payload, &evt); err != nil {
				continue
			}
			c.dispatchEvent(evt)
		}
	}
}

func (c *Client) dispatchEvent(e EventFrame) {
	switch e.Event {
	case "session.message":
		parsed, err := parseSessionMessage(e.Payload)
		if err != nil {
			return
		}
		// Seq-gap detection: capture gap under mu, emit after unlock so a slow
		// consumer can't deadlock producers that need the mutex.
		var gap *GapEvent
		if parsed.MessageSeq != nil {
			c.mu.Lock()
			last, ok := c.lastSeq[parsed.SessionKey]
			if ok && *parsed.MessageSeq > last+1 {
				gap = &GapEvent{SessionKey: parsed.SessionKey, FromSeq: last, ToSeq: *parsed.MessageSeq}
			}
			c.lastSeq[parsed.SessionKey] = *parsed.MessageSeq
			c.mu.Unlock()
		}
		if gap != nil {
			c.events <- ClientEvent{Kind: EventKindGap, Gap: gap}
		}
		c.events <- ClientEvent{Kind: EventKindMessage, SessionMessage: parsed}
	case "sessions.changed":
		parsed, err := parseSessionsChanged(e.Payload)
		if err != nil {
			return
		}
		c.events <- ClientEvent{Kind: EventKindChanged, SessionsChanged: parsed}
	}
}

// Events returns a receive-only channel of ClientEvents.
func (c *Client) Events() <-chan ClientEvent { return c.events }

// Close closes the WebSocket and ends the event stream.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	return c.conn.Close()
}

func (c *Client) newID() string {
	c.mu.Lock()
	c.nextID++
	n := c.nextID
	c.mu.Unlock()
	return fmt.Sprintf("req-%d-%d", time.Now().UnixNano(), n)
}

// runtimeOS reports the current OS. Split out as a var so tests can override.
var runtimeOS = func() string {
	return runtime.GOOS
}

// GatewayError is returned by Call when the gateway responds with ok=false.
type GatewayError struct {
	Code    string
	Message string
	Details json.RawMessage
}

func (e *GatewayError) Error() string {
	return fmt.Sprintf("openclaw gateway error: %s: %s", e.Code, e.Message)
}

// Call sends a request and returns the response payload. The returned raw payload
// should be unmarshaled by typed callers in methods.go.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.newID()
	resCh := make(chan ResponseFrame, 1)
	c.mu.Lock()
	if c.closed || c.pending == nil {
		c.mu.Unlock()
		return nil, errors.New("openclaw: client closed")
	}
	c.pending[id] = resCh
	c.mu.Unlock()

	req := RequestFrame{Type: "req", ID: id, Method: method, Params: params}
	if err := c.conn.WriteJSON(req); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("openclaw: write %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case res, ok := <-resCh:
		if !ok {
			return nil, errors.New("openclaw: connection closed while awaiting response")
		}
		if !res.OK {
			if res.Error == nil {
				return nil, fmt.Errorf("openclaw: %s failed (no error detail)", method)
			}
			return nil, &GatewayError{Code: res.Error.Code, Message: res.Error.Message, Details: res.Error.Details}
		}
		return res.Payload, nil
	}
}
