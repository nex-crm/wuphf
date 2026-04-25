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

	b := NewBroker()
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

func TestWorkspaceShredRouteRequestsBrokerShutdown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	logPath := filepath.Join(home, ".wuphf", "logs", "channel-stderr.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("old run"), 0o600); err != nil {
		t.Fatal(err)
	}

	b := NewBroker()
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

	select {
	case <-b.ShutdownRequested():
	case <-time.After(time.Second):
		t.Fatal("expected shred route to request broker shutdown")
	}
}
