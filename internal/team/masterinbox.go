package team

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	masterInboxDefaultAPIBase = "https://api.masterinbox.com/api/api-webhook/v1/api"
	masterInboxDrainInterval  = 2 * time.Second
)

// masterInboxConfig holds credentials and settings for the Master Inbox API.
type masterInboxConfig struct {
	APIKey        string
	APIBase       string
	WebhookSecret string
}

func (c masterInboxConfig) baseURL() string {
	if c.APIBase != "" {
		return strings.TrimRight(c.APIBase, "/")
	}
	return masterInboxDefaultAPIBase
}

// --- Webhook event types ---

// MasterInboxWebhookEvent represents an inbound webhook event from Master Inbox.
// Field names match the real Master Inbox webhook payload.
type MasterInboxWebhookEvent struct {
	EventType         string            `json:"event_type"`
	EventOption       string            `json:"event_option,omitempty"`
	Date              string            `json:"date,omitempty"`
	WorkspaceID       string            `json:"workspace_id,omitempty"`
	WorkspaceName     string            `json:"workspace_name,omitempty"`
	ProspectID        string            `json:"prospect_id,omitempty"`
	ChannelID         string            `json:"channel_id,omitempty"`
	Channel           string            `json:"channel,omitempty"`
	Subject           string            `json:"subject,omitempty"`
	Text              string            `json:"text,omitempty"`
	HTML              string            `json:"html,omitempty"`
	Name              string            `json:"name,omitempty"`
	FirstName         string            `json:"first_name,omitempty"`
	LastName          string            `json:"last_name,omitempty"`
	Email             string            `json:"email,omitempty"`
	FromName          string            `json:"from_name,omitempty"`
	FromAddress       string            `json:"from_address,omitempty"`
	ToName            string            `json:"to_name,omitempty"`
	ToAddress         string            `json:"to_address,omitempty"`
	LinkedIn          string            `json:"linkedin,omitempty"`
	ListName          string            `json:"list_name,omitempty"`
	JobTitle          string            `json:"job_title,omitempty"`
	CompanyName       string            `json:"company_name,omitempty"`
	CompanyLinkedIn   string            `json:"company_linkedin_url,omitempty"`
	ConversationStage string            `json:"conversation_stage,omitempty"`
	ThreadURL         string            `json:"thread_url,omitempty"`
	Source            string            `json:"source,omitempty"`
	ReplyFrom         string            `json:"reply_from,omitempty"`
	DealValue         float64           `json:"deal_value,omitempty"`
	MessageQuantity   int               `json:"message_quantity,omitempty"`
	Seen              int               `json:"seen,omitempty"`
	IsSpam            int               `json:"is_spam,omitempty"`
	LabelNames        []string          `json:"label_names,omitempty"`
	CustomFields      map[string]any    `json:"custom_fields,omitempty"`
}

// --- API response/request types ---

type masterInboxAPIResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Message string          `json:"message,omitempty"`
}

// MasterInboxProspect represents a prospect record from Master Inbox.
type MasterInboxProspect struct {
	ID        string `json:"id,omitempty"`
	Email     string `json:"email,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Company   string `json:"company,omitempty"`
	Title     string `json:"title,omitempty"`
	Phone     string `json:"phone,omitempty"`
	LinkedIn  string `json:"linkedin_url,omitempty"`
}

func (p MasterInboxProspect) displayName() string {
	name := strings.TrimSpace(p.FirstName + " " + p.LastName)
	if name == "" {
		return p.Email
	}
	return name
}

// MasterInboxLabel represents a label in Master Inbox.
type MasterInboxLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// --- Transport ---

// MasterInboxTransport bridges Master Inbox with the office broker.
// Inbound webhook events are posted to the broker; outbound agent replies
// are pushed to Master Inbox as drafts via the draft-message API.
type MasterInboxTransport struct {
	Config masterInboxConfig
	Broker *Broker
	// ChannelMap maps Master Inbox channel ID -> office channel slug
	ChannelMap map[string]string
	// DefaultChannel is the fallback office channel for unmapped events
	DefaultChannel string
	client         *http.Client
}

// NewMasterInboxTransport creates a transport from the broker's surface channels.
func NewMasterInboxTransport(broker *Broker, cfg masterInboxConfig) *MasterInboxTransport {
	channelMap := make(map[string]string)
	defaultChannel := ""
	for _, ch := range broker.SurfaceChannels("masterinbox") {
		if ch.Surface == nil {
			continue
		}
		if ch.Surface.RemoteID != "" {
			channelMap[ch.Surface.RemoteID] = ch.Slug
		}
		if defaultChannel == "" {
			defaultChannel = ch.Slug
		}
	}
	return &MasterInboxTransport{
		Config:         cfg,
		Broker:         broker,
		ChannelMap:     channelMap,
		DefaultChannel: defaultChannel,
		client:         &http.Client{Timeout: 30 * time.Second},
	}
}

// Start begins the bidirectional bridge: listening for inbound webhooks
// (handled via broker HTTP handler) and draining the broker's external queue
// for outbound draft delivery.
func (t *MasterInboxTransport) Start(ctx context.Context) error {
	if t.Config.APIKey == "" {
		return fmt.Errorf("masterinbox api key is empty")
	}
	if len(t.ChannelMap) == 0 && t.DefaultChannel == "" {
		return fmt.Errorf("no masterinbox channels configured")
	}
	return t.drainOutbound(ctx)
}

// HandleInbound processes an incoming Master Inbox webhook event and posts
// it to the broker as a surface message.
func (t *MasterInboxTransport) HandleInbound(event MasterInboxWebhookEvent) error {
	channel := t.resolveChannel(event.ChannelID)
	if channel == "" {
		return fmt.Errorf("no mapped channel for masterinbox channel_id: %s", event.ChannelID)
	}

	from := event.FromName
	if from == "" {
		from = event.Name
	}
	if from == "" {
		from = event.Email
	}
	if from == "" {
		from = "prospect"
	}

	content := formatInboundMessage(event)
	if content == "" {
		return nil
	}

	_, err := t.Broker.PostInboundSurfaceMessage(from, channel, content, "masterinbox")
	return err
}

// resolveChannel maps a Master Inbox channel ID to an office channel slug.
func (t *MasterInboxTransport) resolveChannel(channelID string) string {
	if channelID != "" {
		if slug, ok := t.ChannelMap[channelID]; ok {
			return slug
		}
	}
	return t.DefaultChannel
}

// formatInboundMessage formats a webhook event into a readable message
// including prospect metadata so agents have full context for drafting.
func formatInboundMessage(event MasterInboxWebhookEvent) string {
	var sb strings.Builder
	if event.Subject != "" {
		sb.WriteString("**Subject:** ")
		sb.WriteString(event.Subject)
		sb.WriteString("\n")
	}

	// Prospect context block — agents need this for drafting and enrichment
	hasContext := event.Email != "" || event.CompanyName != "" || event.ProspectID != ""
	if hasContext {
		sb.WriteString("\n**Prospect Context:**\n")
		if event.ProspectID != "" {
			sb.WriteString("- Prospect ID: `")
			sb.WriteString(event.ProspectID)
			sb.WriteString("`\n")
		}
		if event.Email != "" {
			sb.WriteString("- Email: ")
			sb.WriteString(event.Email)
			sb.WriteString("\n")
		}
		if event.CompanyName != "" {
			sb.WriteString("- Company: ")
			sb.WriteString(event.CompanyName)
			sb.WriteString("\n")
		}
		if event.JobTitle != "" {
			sb.WriteString("- Title: ")
			sb.WriteString(event.JobTitle)
			sb.WriteString("\n")
		}
		if event.ConversationStage != "" {
			sb.WriteString("- Stage: ")
			sb.WriteString(event.ConversationStage)
			sb.WriteString("\n")
		}
		if event.DealValue > 0 {
			sb.WriteString(fmt.Sprintf("- Deal Value: $%.0f\n", event.DealValue))
		}
		if len(event.LabelNames) > 0 {
			sb.WriteString("- Labels: ")
			sb.WriteString(strings.Join(event.LabelNames, ", "))
			sb.WriteString("\n")
		}
		if event.LinkedIn != "" {
			sb.WriteString("- LinkedIn: ")
			sb.WriteString(event.LinkedIn)
			sb.WriteString("\n")
		}
	}

	if event.Text != "" {
		sb.WriteString("\n**Message:**\n")
		sb.WriteString(event.Text)
	}
	return sb.String()
}

// drainOutbound periodically checks the broker's external queue and pushes
// agent replies to Master Inbox as drafts.
func (t *MasterInboxTransport) drainOutbound(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(masterInboxDrainInterval):
		}

		msgs := t.Broker.ExternalQueue("masterinbox")
		for _, msg := range msgs {
			if msg.Kind == "surface" && msg.Source == "masterinbox" {
				continue
			}
			// Outbound agent messages are queued for draft delivery.
			// The prospect_id must be provided in the message title or
			// extracted from the thread context. For now, skip messages
			// without a prospect_id in the title field.
			prospectID := msg.Title
			if prospectID == "" {
				fmt.Printf("[masterinbox] outbound skip: no prospect_id in msg %s\n", msg.ID)
				continue
			}
			if err := t.DraftReply(ctx, prospectID, msg.Content); err != nil {
				fmt.Printf("[masterinbox] draft error: %v\n", err)
			}
		}
	}
}

// --- Master Inbox API methods ---

// DraftReply creates a draft reply in Master Inbox for the given broker message.
// The draft-message API requires prospect_id and message fields.
func (t *MasterInboxTransport) DraftReply(ctx context.Context, prospectID string, message string) error {
	body := map[string]any{
		"prospect_id": prospectID,
		"message":     message,
	}
	_, err := t.apiPost(ctx, "/draft-message", body)
	if err != nil {
		return fmt.Errorf("masterinbox draft-message: %w", err)
	}
	fmt.Printf("[masterinbox] drafted reply for prospect %s\n", prospectID)
	return nil
}

// GetProspectByEmail looks up a prospect by email address.
func (t *MasterInboxTransport) GetProspectByEmail(ctx context.Context, email string) (*MasterInboxProspect, error) {
	data, err := t.apiPost(ctx, "/get-prospects-by-email", map[string]any{
		"email": email,
	})
	if err != nil {
		return nil, fmt.Errorf("masterinbox get-prospects-by-email: %w", err)
	}
	var prospects []MasterInboxProspect
	if err := json.Unmarshal(data, &prospects); err != nil {
		return nil, fmt.Errorf("masterinbox prospect decode: %w", err)
	}
	if len(prospects) == 0 {
		return nil, nil
	}
	return &prospects[0], nil
}

// AddProspectLabel adds a label to a prospect.
func (t *MasterInboxTransport) AddProspectLabel(ctx context.Context, prospectID, labelID string) error {
	_, err := t.apiPost(ctx, "/add-prospect-label", map[string]any{
		"prospect_id": prospectID,
		"label_id":    labelID,
	})
	if err != nil {
		return fmt.Errorf("masterinbox add-prospect-label: %w", err)
	}
	return nil
}

// GetLabels fetches all labels from Master Inbox.
func (t *MasterInboxTransport) GetLabels(ctx context.Context) ([]MasterInboxLabel, error) {
	data, err := t.apiGet(ctx, "/get-labels")
	if err != nil {
		return nil, fmt.Errorf("masterinbox get-labels: %w", err)
	}
	var labels []MasterInboxLabel
	if err := json.Unmarshal(data, &labels); err != nil {
		return nil, fmt.Errorf("masterinbox labels decode: %w", err)
	}
	return labels, nil
}

// GetChannels fetches all channels from Master Inbox.
func (t *MasterInboxTransport) GetChannels(ctx context.Context) (json.RawMessage, error) {
	data, err := t.apiPost(ctx, "/get-channels", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("masterinbox get-channels: %w", err)
	}
	return data, nil
}

// --- HTTP helpers ---

func (t *MasterInboxTransport) apiPost(ctx context.Context, path string, body any) (json.RawMessage, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := t.Config.baseURL() + path
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.Config.APIKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("api error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp masterInboxAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return respBody, nil
	}
	if !apiResp.Success && apiResp.Message != "" {
		return nil, fmt.Errorf("api error: %s", apiResp.Message)
	}
	return apiResp.Data, nil
}

func (t *MasterInboxTransport) apiGet(ctx context.Context, path string) (json.RawMessage, error) {
	url := t.Config.baseURL() + path
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+t.Config.APIKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("api error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp masterInboxAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return respBody, nil
	}
	if !apiResp.Success && apiResp.Message != "" {
		return nil, fmt.Errorf("api error: %s", apiResp.Message)
	}
	return apiResp.Data, nil
}
