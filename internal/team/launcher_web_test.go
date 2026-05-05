package team

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestStartWebStartAndShutdown exercises the non-blocking StartWeb +
// WebHandle.Shutdown contract. The CLI's LaunchWeb wraps this with
// browser-open + select{}; the future cmd/wuphf-desktop Wails shell will
// embed it without the blocking tail. The test asserts the round-trip:
// listener accepts a connection, then Shutdown closes it.
func TestStartWebStartAndShutdown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	t.Setenv("WUPHF_START_FROM_SCRATCH", "1")
	t.Setenv("WUPHF_NO_NEX", "1")

	l, err := NewLauncher("from-scratch")
	if err != nil {
		t.Fatalf("NewLauncher: %v", err)
	}
	l.noOpen = true

	port := freeTCPPort(t)

	handle, err := l.StartWeb(context.Background(), port)
	if err != nil {
		t.Fatalf("StartWeb: %v", err)
	}
	t.Cleanup(func() {
		_ = handle.Shutdown(context.Background())
	})

	wantPrefix := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !strings.HasPrefix(handle.WebURL, wantPrefix) {
		t.Fatalf("WebURL: got %q, want prefix %q", handle.WebURL, wantPrefix)
	}
	if handle.BrokerURL == "" {
		t.Fatalf("BrokerURL: got empty string")
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if !waitForWebReady(addr, 3*time.Second) {
		t.Fatalf("web UI did not become ready at %s within 3s", addr)
	}

	// /api-token is the same-origin loopback endpoint that proves the web UI
	// proxy is wired through to the broker. A 200 here confirms ServeWebUI's
	// mux is registered and the listener accepts and responds.
	resp, err := http.Get(handle.WebURL + "/api-token")
	if err != nil {
		t.Fatalf("GET /api-token: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api-token: status %d body %q", resp.StatusCode, string(body))
	}

	if err := handle.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// After shutdown the listener should be gone. New dials must fail. Allow
	// up to 1s for the runtime to release the port — graceful close is fast
	// in practice but the kernel can lag on some systems.
	deadline := time.Now().Add(1 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			break
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatalf("expected dial %s to fail after shutdown", addr)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Idempotent: a second Shutdown is a no-op, not a panic.
	if err := handle.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

// freeTCPPort grabs an ephemeral port from the kernel and immediately
// releases it. There is a small race window where another process can
// claim the port before StartWeb listens, but on a CI runner with no
// concurrent network activity this is reliable enough for a smoke test.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on 127.0.0.1:0: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}
	return port
}
