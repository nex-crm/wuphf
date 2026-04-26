package team

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWebUIProxyHandlerForwardsOnboardingRoutes(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotQuery string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	b := newTestBroker(t)
	req := httptest.NewRequest(http.MethodGet, "/onboarding/state?step=providers", nil)
	rec := httptest.NewRecorder()

	b.webUIProxyHandler(upstream.URL, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/onboarding/state" {
		t.Fatalf("expected proxied onboarding path, got %q", gotPath)
	}
	if gotQuery != "step=providers" {
		t.Fatalf("expected query to be forwarded, got %q", gotQuery)
	}
	if gotAuth != "Bearer "+b.Token() {
		t.Fatalf("expected broker auth header, got %q", gotAuth)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"ok":true}` {
		t.Fatalf("unexpected proxied body %q", body)
	}
}

func TestWorkspaceShredRouteResetsBrokerWithoutShutdown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	logPath := filepath.Join(home, ".wuphf", "logs", "channel-stderr.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("old run"), 0o600); err != nil {
		t.Fatal(err)
	}

	b := newTestBroker(t)
	b.mu.Lock()
	b.messages = []channelMessage{{
		ID:        "stale-message",
		From:      "human",
		Channel:   "general",
		Content:   "old run",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}}
	b.mu.Unlock()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	req, err := http.NewRequest(http.MethodPost, "http://"+b.Addr()+"/workspace/shred", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post shred: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("expected shred to remove logs, stat err=%v", err)
	}

	b.mu.Lock()
	messageCount := len(b.messages)
	b.mu.Unlock()
	if messageCount != 0 {
		t.Fatalf("expected shred route to reset broker messages, got %d", messageCount)
	}

	// Broker stays alive after shred — follow-up HTTP request on the same
	// listener must succeed. Pre-#264 the broker tore itself down here.
	versionReq, err := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/version", nil)
	if err != nil {
		t.Fatalf("new version request: %v", err)
	}
	versionReq.Header.Set("Authorization", "Bearer "+b.Token())
	versionResp, err := http.DefaultClient.Do(versionReq)
	if err != nil {
		t.Fatalf("expected broker to keep serving after shred, got: %v", err)
	}
	defer versionResp.Body.Close()
	if versionResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /version after shred, got %d", versionResp.StatusCode)
	}
}

// TestWorkspaceShredRoutePostShredBrokerAcceptsNewState extends the prior
// test from "broker still serves" to "broker is fully usable" — proves the
// post-shred broker can accept new messages and persist them, mimicking the
// onboarding flow that re-opens after the UI shred. The pre-#264 launcher
// path tore down the broker after shred, so this property never had test
// coverage; the dead-code removal in #307 makes it the steady state.
func TestWorkspaceShredRoutePostShredBrokerAcceptsNewState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	// Pin broker state path explicitly so the assertion doesn't depend on
	// defaultBrokerStatePath()'s home resolution. NewBrokerAt binds the
	// path at construction (Track A.1, #289) — every save from this broker
	// lands at statePath regardless of process-wide env state.
	statePath := filepath.Join(home, "broker-state.json")
	b := NewBrokerAt(statePath)
	b.mu.Lock()
	b.messages = []channelMessage{{
		ID:        "pre-shred",
		From:      "human",
		Channel:   "general",
		Content:   "stale workspace",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}}
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("seed save: %v", err)
	}
	b.mu.Unlock()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected broker-state.json to exist before shred: %v", err)
	}

	// Trigger shred via HTTP — same path the SettingsApp danger-zone button
	// hits via shredWorkspace() in web/src/api/client.ts.
	shredReq, err := http.NewRequest(http.MethodPost, "http://"+b.Addr()+"/workspace/shred", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new shred request: %v", err)
	}
	shredReq.Header.Set("Authorization", "Bearer "+b.Token())
	shredReq.Header.Set("Content-Type", "application/json")
	shredResp, err := http.DefaultClient.Do(shredReq)
	if err != nil {
		t.Fatalf("post shred: %v", err)
	}
	shredResp.Body.Close()
	if shredResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from shred, got %d", shredResp.StatusCode)
	}

	// In-memory state cleared by Reset.
	b.mu.Lock()
	if len(b.messages) != 0 {
		b.mu.Unlock()
		t.Fatalf("expected post-shred broker to have no messages, got %d", len(b.messages))
	}
	b.mu.Unlock()

	// Broker can accept a fresh post-shred message on the same listener,
	// mimicking the user re-onboarding without restarting wuphf. Goes through
	// the full /messages route + auth + persistence pipeline.
	postBody := strings.NewReader(`{"from":"human","channel":"general","content":"post-shred kickoff"}`)
	postReq, err := http.NewRequest(http.MethodPost, "http://"+b.Addr()+"/messages", postBody)
	if err != nil {
		t.Fatalf("new post-message request: %v", err)
	}
	postReq.Header.Set("Authorization", "Bearer "+b.Token())
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("post message after shred: %v", err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from post-shred /messages, got %d", postResp.StatusCode)
	}

	// Persistence flushed the new message back to disk under the broker's
	// snapshotted statePath. Without #289's Track A.1 binding, a torn read
	// here could land the file at the pre-shred path.
	b.mu.Lock()
	gotMessages := len(b.messages)
	b.mu.Unlock()
	if gotMessages == 0 {
		t.Fatalf("expected post-shred broker to retain the new message in memory")
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected broker-state.json to be re-written after post-shred message, got: %v", err)
	}
}
