package team

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebShareControllerHandlers(t *testing.T) {
	b := &Broker{}
	status := WebShareStatus{Running: true, Bind: "100.64.0.1", Interface: "tailscale0", InviteURL: "http://100.64.0.1:7891/join/tok"}
	stopped := false
	b.SetWebShareController(
		func() (WebShareStatus, error) { return status, nil },
		func() WebShareStatus { return status },
		func() error {
			stopped = true
			status = WebShareStatus{}
			return nil
		},
	)

	startReq := httptest.NewRequest(http.MethodPost, "/api/share/start", nil)
	startResp := httptest.NewRecorder()
	b.handleWebShareStart(startResp, startReq)
	if startResp.Code != http.StatusOK {
		t.Fatalf("start status = %d, want %d", startResp.Code, http.StatusOK)
	}
	var startOut WebShareStatus
	if err := json.NewDecoder(startResp.Body).Decode(&startOut); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if !startOut.Running || startOut.InviteURL == "" {
		t.Fatalf("start response = %+v, want running invite", startOut)
	}

	stopReq := httptest.NewRequest(http.MethodPost, "/api/share/stop", nil)
	stopResp := httptest.NewRecorder()
	b.handleWebShareStop(stopResp, stopReq)
	if stopResp.Code != http.StatusOK {
		t.Fatalf("stop status = %d, want %d", stopResp.Code, http.StatusOK)
	}
	if !stopped {
		t.Fatalf("stop callback was not called")
	}
	var stopOut WebShareStatus
	if err := json.NewDecoder(stopResp.Body).Decode(&stopOut); err != nil {
		t.Fatalf("decode stop: %v", err)
	}
	if stopOut.Running {
		t.Fatalf("stop response = %+v, want not running", stopOut)
	}
}

func TestWebShareStartRejectsGet(t *testing.T) {
	b := &Broker{}
	req := httptest.NewRequest(http.MethodGet, "/api/share/start", nil)
	resp := httptest.NewRecorder()
	b.handleWebShareStart(resp, req)
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusMethodNotAllowed)
	}
}

func TestWebShareStartFailureReturnsServerError(t *testing.T) {
	b := &Broker{}
	b.SetWebShareController(
		func() (WebShareStatus, error) {
			return WebShareStatus{}, errors.New("no private network interface found")
		},
		func() WebShareStatus { return WebShareStatus{} },
		func() error { return nil },
	)

	req := httptest.NewRequest(http.MethodPost, "/api/share/start", nil)
	resp := httptest.NewRecorder()
	b.handleWebShareStart(resp, req)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusInternalServerError)
	}
	var out WebShareStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Error != "no private network interface found" {
		t.Fatalf("error = %q, want controller error", out.Error)
	}
}
