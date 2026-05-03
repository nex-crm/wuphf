package team

import (
	"encoding/json"
	"net/http"
)

// WebShareStatus is the local web UI's view of the team-member invite
// listener. The listener itself is owned by cmd/wuphf because it reuses the
// existing share server and private-network bind policy.
type WebShareStatus struct {
	Running   bool   `json:"running"`
	Bind      string `json:"bind,omitempty"`
	Interface string `json:"interface,omitempty"`
	InviteURL string `json:"invite_url,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

// SetWebShareController registers host-only share controls for ServeWebUI.
// It is intentionally optional so tests and non-web launches do not need to
// construct the share controller.
func (b *Broker) SetWebShareController(start func() (WebShareStatus, error), status func() WebShareStatus, stop func() error) {
	if b == nil {
		return
	}
	b.webShareStart = start
	b.webShareStatus = status
	b.webShareStop = stop
}

func (b *Broker) handleWebShareStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if b.webShareStatus == nil {
		_ = json.NewEncoder(w).Encode(WebShareStatus{})
		return
	}
	_ = json.NewEncoder(w).Encode(b.webShareStatus())
}

func (b *Broker) handleWebShareStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if b.webShareStart == nil {
		http.Error(w, "share controller unavailable", http.StatusServiceUnavailable)
		return
	}
	status, err := b.webShareStart()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if status.Error == "" {
			status.Error = err.Error()
		}
		_ = json.NewEncoder(w).Encode(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (b *Broker) handleWebShareStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if b.webShareStop == nil || b.webShareStatus == nil {
		http.Error(w, "share controller unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := b.webShareStop(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		status := b.webShareStatus()
		if status.Error == "" {
			status.Error = err.Error()
		}
		_ = json.NewEncoder(w).Encode(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(b.webShareStatus())
}
