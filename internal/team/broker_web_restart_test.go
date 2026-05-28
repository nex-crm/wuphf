package team

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubReExec swaps in a no-op re-exec hook so the handler can be exercised
// without actually replacing the process image. The previous hook + delay are
// restored on test cleanup, under the same lock the handler uses so the race
// detector sees a happens-before edge.
func stubReExec(t *testing.T, fn func() error) {
	t.Helper()
	reExecHookMu.Lock()
	prevHook := reExecBrokerProcess
	prevDelay := brokerReExecDelay
	reExecBrokerProcess = fn
	brokerReExecDelay = 0
	reExecHookMu.Unlock()
	t.Cleanup(func() {
		reExecHookMu.Lock()
		reExecBrokerProcess = prevHook
		brokerReExecDelay = prevDelay
		reExecHookMu.Unlock()
	})
}

func TestWebBrokerRestartRejectsGet(t *testing.T) {
	stubReExec(t, func() error { return nil })
	b := &Broker{}
	req := httptest.NewRequest(http.MethodGet, "/api/broker/restart", nil)
	resp := httptest.NewRecorder()

	b.handleWebBrokerRestart(resp, req)

	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusMethodNotAllowed)
	}
}

func TestWebBrokerRestartTriggersReExec(t *testing.T) {
	called := make(chan struct{}, 1)
	stubReExec(t, func() error {
		select {
		case called <- struct{}{}:
		default:
		}
		// Returning nil pretends the exec succeeded; the goroutine in the
		// handler treats nil as "process gone" and stops there.
		return nil
	})

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}
	defer b.Stop()

	req := httptest.NewRequest(http.MethodPost, "/api/broker/restart", nil)
	resp := httptest.NewRecorder()

	b.handleWebBrokerRestart(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", resp.Code, http.StatusAccepted, resp.Body.String())
	}
	var out WebBrokerRestartStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !out.OK || out.URL == "" {
		t.Fatalf("restart response = %+v, want ok with url", out)
	}

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("re-exec hook was not called")
	}
}

// When the platform re-exec fails (e.g. Windows, or syscall.Exec returned an
// error), the handler must fall back to the in-process listener restart so
// the SSE client still reconnects.
func TestWebBrokerRestartFallsBackToListenerOnReExecFailure(t *testing.T) {
	var reExecCalled atomic.Bool
	stubReExec(t, func() error {
		reExecCalled.Store(true)
		return errors.New("re-exec not supported in test")
	})

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}
	defer b.Stop()

	oldAddr := b.Addr()

	req := httptest.NewRequest(http.MethodPost, "/api/broker/restart", nil)
	resp := httptest.NewRecorder()

	b.handleWebBrokerRestart(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", resp.Code, http.StatusAccepted, resp.Body.String())
	}

	// Poll until the fallback finishes — the listener restart happens in the
	// same goroutine that called the re-exec hook.
	deadline := time.Now().Add(5 * time.Second)
	client := &http.Client{Timeout: 1 * time.Second}
	for {
		if !reExecCalled.Load() {
			if time.Now().After(deadline) {
				t.Fatal("re-exec hook was not called before deadline")
			}
			time.Sleep(20 * time.Millisecond)
			continue
		}
		// re-exec returned; listener restart should be in flight or done.
		healthResp, err := client.Get("http://" + b.Addr() + "/health")
		if err == nil {
			body, _ := io.ReadAll(healthResp.Body)
			healthResp.Body.Close()
			if healthResp.StatusCode == http.StatusOK {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("GET /health status after fallback = %d: %s", healthResp.StatusCode, string(body))
			}
		} else if time.Now().After(deadline) {
			t.Fatalf("GET /health after fallback never succeeded: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if b.Addr() != oldAddr {
		t.Fatalf("listener addr after fallback = %q, want %q", b.Addr(), oldAddr)
	}
}

func TestWebBrokerRestartRejectsAfterStop(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}
	b.Stop()

	_, err := b.RestartBrokerListener()
	if err == nil {
		t.Fatal("RestartBrokerListener error = nil, want shutdown error")
	}
	if !strings.Contains(err.Error(), "shutting down") {
		t.Fatalf("RestartBrokerListener error = %q, want shutdown error", err)
	}
}

func TestWebBrokerRestartCanRaceWithStop(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = b.RestartBrokerListener()
	}()
	go func() {
		defer wg.Done()
		b.Stop()
	}()
	wg.Wait()
	b.Stop()
}
