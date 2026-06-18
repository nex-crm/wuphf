package team

// broker_apps_gmail.go exposes a READ-ONLY, SANITIZED Gmail feed to sandboxed
// Apps. It is a sensitive-data surface: it reads the operator's real inbox.
//
// Security posture (by design):
//   - READ-ONLY: it only ever runs the read-only GMAIL_FETCH_EMAILS action. It
//     never sends, drafts, deletes, labels, or otherwise mutates mail. The
//     action id is hard-coded here, not taken from the request.
//   - METADATA + SNIPPET ONLY: each returned message carries only id, threadId,
//     from/fromName, subject, a short snippet, date, unread, and label names.
//     The full body (messageText), MIME payload/headers, and attachments from
//     the upstream response are dropped on the floor and never cross the wire.
//   - AUTH-GATED: registered behind requireAuth like every other /apps route.
//   - NO SECRETS LOGGED: the only stderr line is a generic upstream-failure
//     note with the platform/action, never the response body or credentials.
//
// Endpoint:
//
//	GET /apps/gmail/recent?limit=N
//	  -> 200 { connected: true,  emails: [ {id, threadId, from, fromName,
//	            subject, snippet, date, unread, labels[]}, ... ] }
//	  -> 200 { connected: false, error: "<reason>", emails: [] }   (not 500)
//
// When Gmail is not connected (or Composio is unconfigured/unreachable) the
// handler returns HTTP 200 with connected:false so the App can render a
// connect-state instead of an error toast.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
)

const (
	// gmailFetchAction is the single read-only action this endpoint may run. It
	// is hard-coded (never request-derived) so the surface can only ever read.
	gmailFetchAction = "GMAIL_FETCH_EMAILS"
	// gmailRecentDefaultLimit/gmailRecentMaxLimit bound how many messages an App
	// can pull in one call. Kept small: an internal tool shows a recent slice,
	// not the whole mailbox.
	gmailRecentDefaultLimit = 25
	gmailRecentMaxLimit     = 50
	// gmailSnippetMax caps the snippet length so a long preview body cannot bloat
	// the response or leak more of the message than a snippet is meant to.
	gmailSnippetMax = 400
)

// appGmailEmail is the SANITIZED, metadata-and-snippet-only shape an App sees.
// It deliberately omits the full body, MIME headers, and attachments present in
// the upstream Gmail response.
type appGmailEmail struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"threadId"`
	From     string   `json:"from"`
	FromName string   `json:"fromName"`
	Subject  string   `json:"subject"`
	Snippet  string   `json:"snippet"`
	Date     string   `json:"date"`
	Unread   bool     `json:"unread"`
	Labels   []string `json:"labels"`
}

type appGmailRecentResponse struct {
	Connected bool            `json:"connected"`
	Error     string          `json:"error,omitempty"`
	Emails    []appGmailEmail `json:"emails"`
}

// handleAppGmailRecent serves GET /apps/gmail/recent. It resolves the workspace
// Gmail connection the same way the action gate does, runs the read-only fetch,
// and returns a sanitized list. A not-connected / unreachable Gmail is surfaced
// as { connected:false } with HTTP 200 so the App can show a connect-state.
func (b *Broker) handleAppGmailRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := gmailRecentDefaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > gmailRecentMaxLimit {
		limit = gmailRecentMaxLimit
	}

	composio := action.NewComposioFromEnv()
	if !composio.Configured() {
		writeJSON(w, http.StatusOK, appGmailRecentResponse{
			Connected: false,
			Error:     "Gmail is not connected.",
			Emails:    []appGmailEmail{},
		})
		return
	}

	// Resolve the gmail connection key exactly like resolveExternalAction: probe
	// the connection status for the platform, then execute against that key.
	status, err := composio.GetIntegrationConnectionStatus(r.Context(), action.IntegrationStatusRequest{
		Provider: "composio",
		Platform: "gmail",
	})
	if err != nil {
		// Probe call failed (provider unreachable): treat as not-connected for the
		// App's purposes rather than a 500. Do not log the error body — it may
		// echo request context; a generic note is enough to diagnose.
		fmt.Fprintf(os.Stderr, "broker: gmail apps probe failed: %v\n", err)
		writeJSON(w, http.StatusOK, appGmailRecentResponse{
			Connected: false,
			Error:     "Gmail is temporarily unavailable.",
			Emails:    []appGmailEmail{},
		})
		return
	}
	if action.MapConnectionState(status.Status) != action.StateConnected || strings.TrimSpace(status.ConnectionKey) == "" {
		writeJSON(w, http.StatusOK, appGmailRecentResponse{
			Connected: false,
			Error:     "Gmail is not connected.",
			Emails:    []appGmailEmail{},
		})
		return
	}

	res, err := composio.ExecuteAction(r.Context(), action.ExecuteRequest{
		Platform:      "gmail",
		ActionID:      gmailFetchAction,
		ConnectionKey: strings.TrimSpace(status.ConnectionKey),
		Data: map[string]any{
			"max_results": limit,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "broker: gmail apps fetch failed: %v\n", err)
		writeJSON(w, http.StatusOK, appGmailRecentResponse{
			Connected: true,
			Error:     "Could not read Gmail right now.",
			Emails:    []appGmailEmail{},
		})
		return
	}

	emails, err := sanitizeGmailMessages(res.Response, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "broker: gmail apps parse failed: %v\n", err)
		writeJSON(w, http.StatusOK, appGmailRecentResponse{
			Connected: true,
			Error:     "Could not read Gmail right now.",
			Emails:    []appGmailEmail{},
		})
		return
	}
	writeJSON(w, http.StatusOK, appGmailRecentResponse{
		Connected: true,
		Emails:    emails,
	})
}

// gmailRawMessage is the SUBSET of the upstream Gmail message we parse. Fields
// we deliberately do not decode (messageText/full body, payload/MIME headers,
// attachmentList) are simply absent here, so they can never reach the App.
type gmailRawMessage struct {
	MessageID        string   `json:"messageId"`
	ThreadID         string   `json:"threadId"`
	Sender           string   `json:"sender"`
	Subject          string   `json:"subject"`
	MessageTimestamp string   `json:"messageTimestamp"`
	LabelIDs         []string `json:"labelIds"`
	Preview          struct {
		Body string `json:"body"`
	} `json:"preview"`
}

// sanitizeGmailMessages turns the raw GMAIL_FETCH_EMAILS envelope into the
// minimal, metadata-and-snippet-only list Apps may see. It parses only the
// allow-listed fields; the body/headers/attachments are never decoded.
func sanitizeGmailMessages(raw json.RawMessage, limit int) ([]appGmailEmail, error) {
	var envelope struct {
		Data struct {
			Messages []gmailRawMessage `json:"messages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse gmail messages: %w", err)
	}
	msgs := envelope.Data.Messages
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[:limit]
	}
	out := make([]appGmailEmail, 0, len(msgs))
	for _, m := range msgs {
		fromAddr, fromName := splitEmailSender(m.Sender)
		out = append(out, appGmailEmail{
			ID:       strings.TrimSpace(m.MessageID),
			ThreadID: strings.TrimSpace(m.ThreadID),
			From:     fromAddr,
			FromName: fromName,
			Subject:  strings.TrimSpace(m.Subject),
			Snippet:  capGmailSnippet(m.Preview.Body),
			Date:     normalizeGmailDate(m.MessageTimestamp),
			Unread:   gmailLabelsContain(m.LabelIDs, "UNREAD"),
			Labels:   sanitizeGmailLabels(m.LabelIDs),
		})
	}
	return out, nil
}

// splitEmailSender splits a `Name <addr@host>` sender into (address, name).
// When there is no angle-bracket form it treats the whole string as the
// address and leaves the name empty.
func splitEmailSender(sender string) (addr, name string) {
	s := strings.TrimSpace(sender)
	if s == "" {
		return "", ""
	}
	open := strings.LastIndex(s, "<")
	close := strings.LastIndex(s, ">")
	if open >= 0 && close > open {
		addr = strings.TrimSpace(s[open+1 : close])
		name = strings.TrimSpace(strings.Trim(strings.TrimSpace(s[:open]), `"`))
		return addr, name
	}
	return s, ""
}

// capGmailSnippet trims and length-caps the preview body. Snippet only: never
// the full message text.
func capGmailSnippet(body string) string {
	s := strings.TrimSpace(body)
	if len(s) > gmailSnippetMax {
		return s[:gmailSnippetMax]
	}
	return s
}

// normalizeGmailDate normalizes the upstream timestamp to RFC3339. The upstream
// value is already RFC3339-shaped (e.g. "2026-06-18T11:49:42Z"); if it does not
// parse, the raw trimmed value is returned rather than dropping the field.
func normalizeGmailDate(ts string) string {
	s := strings.TrimSpace(ts)
	if s == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, s); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	return s
}

// gmailLabelsContain reports whether want appears in labels (case-insensitive).
func gmailLabelsContain(labels []string, want string) bool {
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l), want) {
			return true
		}
	}
	return false
}

// sanitizeGmailLabels trims and drops empty label ids, returning a fresh slice
// so the caller never shares backing storage with the parsed response.
func sanitizeGmailLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return out
}
