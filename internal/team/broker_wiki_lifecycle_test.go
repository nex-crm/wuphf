package team

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureWikiWorkerRetriesAfterInitFailure verifies that a transient
// repo.Init failure (parent path is a regular file rather than a dir) no
// longer permanently consumes the init slot. After the obstruction is
// removed, the next ensureWikiWorker call must successfully bring the
// worker up — this is the regression that left /notebook/* and /review/*
// stuck on 503 until the operator restarted the broker.
func TestEnsureWikiWorkerRetriesAfterInitFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")

	// Block init by planting a regular file where ~/.wuphf must be a dir.
	// MkdirAll(<home>/.wuphf/wiki) returns ENOTDIR because .wuphf is a file.
	blocker := filepath.Join(home, ".wuphf")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}

	b := newTestBroker(t)

	b.ensureWikiWorker()
	if b.WikiWorker() != nil {
		t.Fatalf("wiki worker should be nil after init failure, got %p", b.WikiWorker())
	}
	if err := b.WikiInitErr(); err == nil {
		t.Fatalf("WikiInitErr should be set after init failure")
	}

	// Clear the obstruction and retry. Pre-fix this would stay nil because
	// sync.Once was already consumed.
	if err := os.Remove(blocker); err != nil {
		t.Fatalf("remove blocker: %v", err)
	}

	b.ensureWikiWorker()
	if b.WikiWorker() == nil {
		t.Fatalf("wiki worker should be set after retry; init err: %v", b.WikiInitErr())
	}
	if err := b.WikiInitErr(); err != nil {
		t.Fatalf("WikiInitErr should be cleared on success, got %v", err)
	}

	// Idempotent on subsequent calls — must not double-init or panic.
	prev := b.WikiWorker()
	b.ensureWikiWorker()
	if b.WikiWorker() != prev {
		t.Fatalf("ensureWikiWorker should be idempotent; got new worker pointer")
	}
}

// TestNotebookHandler503SelfHealsAfterInitFailure exercises the user-
// reported flow: the first request hits 503 because init failed at boot,
// and after the underlying obstruction clears, the next request succeeds
// without a broker restart.
func TestNotebookHandler503SelfHealsAfterInitFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")

	blocker := filepath.Join(home, ".wuphf")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}

	b := newTestBroker(t)

	srv := httptest.NewServer(b.requireAuth(b.handleNotebookList))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"?slug=ceo", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("first request: want 503, got %d, body %s", resp.StatusCode, string(body))
	}
	// 503 body should now surface the underlying error so operators can
	// diagnose without grepping broker stdout.
	if !strings.Contains(string(body), "notebook backend is not active") {
		t.Fatalf("503 body should mention the backend; got %s", string(body))
	}
	if !strings.Contains(string(body), "repo init") {
		t.Fatalf("503 body should surface the underlying init error; got %s", string(body))
	}

	if err := os.Remove(blocker); err != nil {
		t.Fatalf("remove blocker: %v", err)
	}

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"?slug=ceo", nil)
	req2.Header.Set("Authorization", "Bearer "+b.Token())
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second request after retry: want 200, got %d, body %s", resp2.StatusCode, string(body2))
	}
}

// TestHealthMemoryBackendReadyReflectsWikiWorker locks in the /health
// fix: when memory_backend_active=markdown but wiki worker init failed,
// memory_backend_ready must be false. The pre-fix value of true masked
// the failure from operators and the web UI status bar.
func TestHealthMemoryBackendReadyReflectsWikiWorker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")

	blocker := filepath.Join(home, ".wuphf")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}

	b := newTestBroker(t)
	b.ensureWikiWorker() // fails — wikiWorker stays nil

	srv := httptest.NewServer(http.HandlerFunc(b.handleHealth))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()
	var got HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if got.MemoryBackendActive != "markdown" {
		t.Fatalf("expected memory_backend_active=markdown, got %q", got.MemoryBackendActive)
	}
	if got.MemoryBackendReady {
		t.Fatalf("expected memory_backend_ready=false when wiki worker is nil; got true")
	}
}
