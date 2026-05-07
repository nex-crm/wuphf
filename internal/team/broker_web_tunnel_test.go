package team

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebTunnelControllerHandlers(t *testing.T) {
	b := &Broker{}
	status := WebTunnelStatus{
		Running:   true,
		PublicURL: "https://blue-clear-cat-42.trycloudflare.com",
		InviteURL: "https://blue-clear-cat-42.trycloudflare.com/join/tok",
	}
	stopped := false
	b.SetWebTunnelController(
		func() (WebTunnelStatus, error) { return status, nil },
		func() WebTunnelStatus { return status },
		func() error {
			stopped = true
			status = WebTunnelStatus{}
			return nil
		},
	)

	startReq := httptest.NewRequest(http.MethodPost, "/api/share/tunnel/start", nil)
	startResp := httptest.NewRecorder()
	b.handleWebTunnelStart(startResp, startReq)
	if startResp.Code != http.StatusOK {
		t.Fatalf("start status = %d, want %d", startResp.Code, http.StatusOK)
	}
	var startOut WebTunnelStatus
	if err := json.NewDecoder(startResp.Body).Decode(&startOut); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if !startOut.Running || startOut.InviteURL == "" {
		t.Fatalf("start response = %+v, want running invite", startOut)
	}

	stopReq := httptest.NewRequest(http.MethodPost, "/api/share/tunnel/stop", nil)
	stopResp := httptest.NewRecorder()
	b.handleWebTunnelStop(stopResp, stopReq)
	if stopResp.Code != http.StatusOK {
		t.Fatalf("stop status = %d, want %d", stopResp.Code, http.StatusOK)
	}
	if !stopped {
		t.Fatalf("stop callback was not called")
	}
	var stopOut WebTunnelStatus
	if err := json.NewDecoder(stopResp.Body).Decode(&stopOut); err != nil {
		t.Fatalf("decode stop: %v", err)
	}
	if stopOut.Running {
		t.Fatalf("stop response = %+v, want not running", stopOut)
	}
}

func TestWebTunnelStartRejectsGet(t *testing.T) {
	b := &Broker{}
	req := httptest.NewRequest(http.MethodGet, "/api/share/tunnel/start", nil)
	resp := httptest.NewRecorder()
	b.handleWebTunnelStart(resp, req)
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusMethodNotAllowed)
	}
}

// TestWebTunnelStartSurfacesCloudflaredMissing makes sure the
// cloudflared_missing flag round-trips through the JSON encoding so the UI
// can render install instructions instead of a generic error string.
func TestWebTunnelStartSurfacesCloudflaredMissing(t *testing.T) {
	b := &Broker{}
	b.SetWebTunnelController(
		func() (WebTunnelStatus, error) {
			return WebTunnelStatus{
				CloudflaredMissing: true,
				Error:              "cloudflared is not installed.",
			}, errors.New("cloudflared is not installed.")
		},
		func() WebTunnelStatus { return WebTunnelStatus{CloudflaredMissing: true} },
		func() error { return nil },
	)

	req := httptest.NewRequest(http.MethodPost, "/api/share/tunnel/start", nil)
	resp := httptest.NewRecorder()
	b.handleWebTunnelStart(resp, req)
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusInternalServerError)
	}
	var out WebTunnelStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !out.CloudflaredMissing {
		t.Fatalf("cloudflared_missing = false, want true (response: %+v)", out)
	}
}
