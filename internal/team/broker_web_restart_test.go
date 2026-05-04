package team

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebBrokerRestartRejectsGet(t *testing.T) {
	b := &Broker{}
	req := httptest.NewRequest(http.MethodGet, "/api/broker/restart", nil)
	resp := httptest.NewRecorder()

	b.handleWebBrokerRestart(resp, req)

	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusMethodNotAllowed)
	}
}

func TestWebBrokerRestartRestartsListener(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}
	oldAddr := b.Addr()
	defer b.Stop()

	req := httptest.NewRequest(http.MethodPost, "/api/broker/restart", nil)
	resp := httptest.NewRecorder()

	b.handleWebBrokerRestart(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", resp.Code, http.StatusOK, resp.Body.String())
	}
	var out WebBrokerRestartStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !out.OK || out.URL == "" {
		t.Fatalf("restart response = %+v, want ok with url", out)
	}
	if b.Addr() != oldAddr {
		t.Fatalf("listener addr = %q, want restart on same address %q", b.Addr(), oldAddr)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	healthResp, err := client.Get("http://" + b.Addr() + "/health")
	if err != nil {
		t.Fatalf("GET /health after restart: %v", err)
	}
	defer healthResp.Body.Close()
	body, _ := io.ReadAll(healthResp.Body)
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d: %s", healthResp.StatusCode, http.StatusOK, string(body))
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
