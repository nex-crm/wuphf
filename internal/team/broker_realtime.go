package team

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// broker_realtime.go backs the real "Demo workflow to Nex" call: a screen-share
// + realtime-voice session against OpenAI's Realtime API. The browser does the
// WebRTC handshake, but it must NEVER hold the operator's long-lived OpenAI key.
// So the broker mints a short-lived EPHEMERAL token from the configured key and
// hands only that to the browser, along with the model + SDP URL the FE needs.
// See docs/specs/operator-demo-call-real.md.

// realtimeHTTPClient mints ephemeral tokens against OpenAI. A package var so the
// test can inject a stub transport; the real key only ever flows through here.
var realtimeHTTPClient = &http.Client{Timeout: 15 * time.Second}

// realtimeMintURL is the OpenAI endpoint that exchanges the long-lived key for a
// short-lived ephemeral Realtime token. Configurable so the exact GA surface can
// change without a code edit.
func realtimeMintURL() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_REALTIME_MINT_URL")); v != "" {
		return v
	}
	return "https://api.openai.com/v1/realtime/client_secrets"
}

// realtimeSDPURL is where the browser POSTs its WebRTC SDP offer. Returned to the
// FE so the connect surface stays config, not a hardcoded guess that could drift.
func realtimeSDPURL() string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_REALTIME_SDP_URL")); v != "" {
		return v
	}
	return "https://api.openai.com/v1/realtime/calls"
}

// realtimeSessionResponse is the wire shape the FE consumes. It deliberately
// carries the EPHEMERAL token only — never the operator's real key.
type realtimeSessionResponse struct {
	EphemeralKey string `json:"ephemeral_key"`
	Model        string `json:"model"`
	SDPURL       string `json:"sdp_url"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
}

// handleRealtimeSession mints an ephemeral Realtime token for the demo call. With
// no key configured it returns 503 so the FE falls back to the scripted mock.
func (b *Broker) handleRealtimeSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	key := strings.TrimSpace(config.ResolveOpenAIAPIKey())
	if key == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "no OpenAI Realtime key configured",
		})
		return
	}
	model := config.ResolveRealtimeModel()
	ek, expiresAt, err := mintRealtimeEphemeralKey(r.Context(), key, model)
	if err != nil {
		// Never log the response body or key — it can carry sensitive detail.
		log.Printf("realtime: ephemeral mint failed: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "could not start a realtime session",
		})
		return
	}
	writeJSON(w, http.StatusOK, realtimeSessionResponse{
		EphemeralKey: ek,
		Model:        model,
		SDPURL:       realtimeSDPURL(),
		ExpiresAt:    expiresAt,
	})
}

// mintRealtimeEphemeralKey exchanges the long-lived OpenAI key for a short-lived
// ephemeral token usable for one WebRTC handshake. Accepts both the GA response
// shape ({"value":...}) and the preview shape ({"client_secret":{"value":...}}).
func mintRealtimeEphemeralKey(ctx context.Context, apiKey, model string) (string, int64, error) {
	payload, err := json.Marshal(map[string]any{
		"session": map[string]any{"type": "realtime", "model": model},
	})
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, realtimeMintURL(), bytes.NewReader(payload))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := realtimeHTTPClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("realtime mint request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", 0, fmt.Errorf("realtime mint status %d", resp.StatusCode)
	}

	var parsed struct {
		Value        string `json:"value"`
		ExpiresAt    int64  `json:"expires_at"`
		ClientSecret struct {
			Value     string `json:"value"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"client_secret"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", 0, fmt.Errorf("realtime mint decode: %w", err)
	}
	ek, exp := strings.TrimSpace(parsed.Value), parsed.ExpiresAt
	if ek == "" {
		ek, exp = strings.TrimSpace(parsed.ClientSecret.Value), parsed.ClientSecret.ExpiresAt
	}
	if ek == "" {
		return "", 0, errors.New("realtime mint: no ephemeral key in response")
	}
	return ek, exp, nil
}
