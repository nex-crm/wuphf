package team

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

// masterInboxTransport holds the active Master Inbox transport, if any.
// Set by StartMasterInboxBridge and read by the webhook handler.
// Protected by the broker's mu.
func (b *Broker) masterInboxTransportLocked() *MasterInboxTransport {
	return b.miTransport
}

// StartMasterInboxBridge initializes and starts the Master Inbox transport
// if WUPHF_MASTERINBOX_API_KEY is set and masterinbox surface channels exist.
// Called from the broker's Start path or from a connect flow.
func (b *Broker) StartMasterInboxBridge(ctx context.Context) error {
	apiKey := strings.TrimSpace(os.Getenv("WUPHF_MASTERINBOX_API_KEY"))
	if apiKey == "" {
		return nil
	}

	cfg := masterInboxConfig{
		APIKey:        apiKey,
		APIBase:       strings.TrimSpace(os.Getenv("WUPHF_MASTERINBOX_API_BASE")),
		WebhookSecret: strings.TrimSpace(os.Getenv("WUPHF_MASTERINBOX_WEBHOOK_SECRET")),
	}

	transport := NewMasterInboxTransport(b, cfg)
	if transport.DefaultChannel == "" && len(transport.ChannelMap) == 0 {
		log.Printf("[masterinbox] api key set but no masterinbox surface channels found; skipping bridge")
		return nil
	}

	b.mu.Lock()
	b.miTransport = transport
	b.mu.Unlock()

	log.Printf("[masterinbox] bridge started (channels=%d, default=%s)", len(transport.ChannelMap), transport.DefaultChannel)
	go func() {
		if err := transport.Start(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[masterinbox] bridge stopped: %v", err)
		}
	}()
	return nil
}

// handleMasterInboxWebhook handles inbound webhook events from Master Inbox.
// This endpoint does NOT require broker auth — it is verified via the
// webhook secret (HMAC) when configured, or accepts all requests if no
// secret is set (development mode).
//
//	POST /masterinbox/webhook
//	Body: MasterInboxWebhookEvent JSON
func (b *Broker) handleMasterInboxWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	b.mu.Lock()
	transport := b.masterInboxTransportLocked()
	b.mu.Unlock()

	if transport == nil {
		http.Error(w, "masterinbox bridge not configured", http.StatusServiceUnavailable)
		return
	}

	var event MasterInboxWebhookEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	log.Printf("[masterinbox] webhook: type=%s from=%s channel=%s prospect=%s",
		event.EventType, event.FromName, event.ChannelID, event.ProspectID)

	if err := transport.HandleInbound(event); err != nil {
		log.Printf("[masterinbox] webhook error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleMasterInboxDraft proxies agent draft requests to the Master Inbox API.
// Agents call this via their MCP tool (masterinbox_draft_reply).
//
//	POST /masterinbox/draft
//	Body: { "prospect_id": "...", "message": "..." }
func (b *Broker) handleMasterInboxDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	b.mu.Lock()
	transport := b.masterInboxTransportLocked()
	b.mu.Unlock()

	if transport == nil {
		http.Error(w, "masterinbox bridge not configured", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		ProspectID string `json:"prospect_id"`
		Message    string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.ProspectID == "" || body.Message == "" {
		http.Error(w, "prospect_id and message required", http.StatusBadRequest)
		return
	}

	if err := transport.DraftReply(r.Context(), body.ProspectID, body.Message); err != nil {
		http.Error(w, fmt.Sprintf("draft failed: %v", err), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "drafted"})
}

// handleMasterInboxProspect proxies prospect lookups to the Master Inbox API.
//
//	POST /masterinbox/prospect
//	Body: { "email": "..." }
func (b *Broker) handleMasterInboxProspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	b.mu.Lock()
	transport := b.masterInboxTransportLocked()
	b.mu.Unlock()

	if transport == nil {
		http.Error(w, "masterinbox bridge not configured", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.Email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}

	prospect, err := transport.GetProspectByEmail(r.Context(), body.Email)
	if err != nil {
		http.Error(w, fmt.Sprintf("prospect lookup failed: %v", err), http.StatusBadGateway)
		return
	}
	if prospect == nil {
		http.Error(w, "prospect not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prospect)
}

// handleMasterInboxLabel proxies label operations to the Master Inbox API.
//
//	POST /masterinbox/label
//	Body: { "prospect_id": "...", "label_id": "..." }
func (b *Broker) handleMasterInboxLabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	b.mu.Lock()
	transport := b.masterInboxTransportLocked()
	b.mu.Unlock()

	if transport == nil {
		http.Error(w, "masterinbox bridge not configured", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		ProspectID string `json:"prospect_id"`
		LabelID    string `json:"label_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.ProspectID == "" || body.LabelID == "" {
		http.Error(w, "prospect_id and label_id required", http.StatusBadRequest)
		return
	}

	if err := transport.AddProspectLabel(r.Context(), body.ProspectID, body.LabelID); err != nil {
		http.Error(w, fmt.Sprintf("label add failed: %v", err), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "labeled"})
}
