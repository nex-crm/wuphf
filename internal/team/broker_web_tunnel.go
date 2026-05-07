package team

import (
	"encoding/json"
	"net/http"
)

// WebTunnelStatus is the local web UI's view of the public-tunnel listener
// for team-member invites. The listener wraps a `cloudflared` subprocess that
// publishes a one-off https://*.trycloudflare.com URL pointing at the
// loopback share HTTP server, so non-technical hosts can hand a link to a
// teammate without standing up Tailscale or an SSH tunnel themselves.
//
// Owned by cmd/wuphf the same way WebShareStatus is — keeping the broker
// transport-agnostic.
type WebTunnelStatus struct {
	Running bool `json:"running"`
	// PublicURL is the trycloudflare.com origin (no path). Empty until
	// cloudflared has reported a URL.
	PublicURL string `json:"public_url,omitempty"`
	// InviteURL is PublicURL + "/join/<token>". Empty until both the
	// tunnel and a fresh invite token are ready.
	InviteURL string `json:"invite_url,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Error     string `json:"error,omitempty"`
	// CloudflaredMissing flips on when the tunnel binary is not on PATH so
	// the UI can render install instructions instead of a generic error.
	CloudflaredMissing bool `json:"cloudflared_missing,omitempty"`
}

// SetWebTunnelController registers host-only public-tunnel controls. Like
// SetWebShareController, it is optional so non-web launches can skip it.
func (b *Broker) SetWebTunnelController(start func() (WebTunnelStatus, error), status func() WebTunnelStatus, stop func() error) {
	if b == nil {
		return
	}
	b.webTunnelStart = start
	b.webTunnelStatus = status
	b.webTunnelStop = stop
}

func (b *Broker) handleWebTunnelStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if b.webTunnelStatus == nil {
		_ = json.NewEncoder(w).Encode(WebTunnelStatus{})
		return
	}
	_ = json.NewEncoder(w).Encode(b.webTunnelStatus())
}

func (b *Broker) handleWebTunnelStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if b.webTunnelStart == nil {
		http.Error(w, "tunnel controller unavailable", http.StatusServiceUnavailable)
		return
	}
	status, err := b.webTunnelStart()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		if status.Error == "" {
			status.Error = err.Error()
		}
		_ = json.NewEncoder(w).Encode(status)
		return
	}
	_ = json.NewEncoder(w).Encode(status)
}

func (b *Broker) handleWebTunnelStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if b.webTunnelStop == nil || b.webTunnelStatus == nil {
		http.Error(w, "tunnel controller unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := b.webTunnelStop(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		status := b.webTunnelStatus()
		if status.Error == "" {
			status.Error = err.Error()
		}
		_ = json.NewEncoder(w).Encode(status)
		return
	}
	_ = json.NewEncoder(w).Encode(b.webTunnelStatus())
}
