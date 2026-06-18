package team

import (
	"encoding/json"
	"strings"
	"testing"
)

// sampleGmailEnvelope mirrors the real GMAIL_FETCH_EMAILS response shape: a top
// level data.messages array where each message carries metadata PLUS the full
// body (messageText), MIME headers (payload), and attachments — exactly the
// sensitive fields the sanitizer must drop.
const sampleGmailEnvelope = `{
  "successful": true,
  "data": {
    "messages": [
      {
        "messageId": "abc123",
        "threadId": "thread123",
        "sender": "Sentry <noreply@md.getsentry.com>",
        "subject": "Build failed",
        "messageTimestamp": "2026-06-18T11:49:42Z",
        "labelIds": ["UNREAD", "IMPORTANT", "INBOX"],
        "preview": { "body": "A new issue was detected in your project" },
        "messageText": "SECRET FULL BODY THAT MUST NOT LEAK",
        "attachmentList": [{"filename": "secret.pdf"}],
        "to": "operator@nex.ai",
        "payload": { "headers": [{"name": "Authorization", "value": "Bearer secret-token"}] }
      },
      {
        "messageId": "def456",
        "threadId": "thread456",
        "sender": "plainaddress@example.com",
        "subject": "No display name",
        "messageTimestamp": "2026-06-17T08:00:00Z",
        "labelIds": ["INBOX"],
        "preview": { "body": "read message body preview" }
      }
    ]
  }
}`

func TestSanitizeGmailMessages_DropsBodyHeadersAttachments(t *testing.T) {
	emails, err := sanitizeGmailMessages(json.RawMessage(sampleGmailEnvelope), 25)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if len(emails) != 2 {
		t.Fatalf("want 2 emails, got %d", len(emails))
	}

	// The sanitized struct is JSON-marshaled back to the App; assert the full
	// body, MIME headers, the bearer token, and attachment names never appear in
	// the wire payload. This is the security guarantee of the endpoint.
	wire, err := json.Marshal(emails)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, leak := range []string{
		"SECRET FULL BODY THAT MUST NOT LEAK",
		"secret.pdf",
		"Bearer secret-token",
		"Authorization",
		"messageText",
		"payload",
		"attachmentList",
	} {
		if strings.Contains(string(wire), leak) {
			t.Errorf("sanitized output leaked %q: %s", leak, wire)
		}
	}
}

func TestSanitizeGmailMessages_FieldMapping(t *testing.T) {
	emails, err := sanitizeGmailMessages(json.RawMessage(sampleGmailEnvelope), 25)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}

	first := emails[0]
	if first.ID != "abc123" || first.ThreadID != "thread123" {
		t.Errorf("ids: got id=%q thread=%q", first.ID, first.ThreadID)
	}
	if first.From != "noreply@md.getsentry.com" || first.FromName != "Sentry" {
		t.Errorf("sender split: got from=%q name=%q", first.From, first.FromName)
	}
	if first.Subject != "Build failed" {
		t.Errorf("subject: got %q", first.Subject)
	}
	if first.Snippet != "A new issue was detected in your project" {
		t.Errorf("snippet: got %q", first.Snippet)
	}
	if first.Date != "2026-06-18T11:49:42Z" {
		t.Errorf("date: got %q", first.Date)
	}
	if !first.Unread {
		t.Errorf("expected first message UNREAD")
	}
	if len(first.Labels) != 3 {
		t.Errorf("labels: got %v", first.Labels)
	}

	// Second message: bare address (no display name) and not unread.
	second := emails[1]
	if second.From != "plainaddress@example.com" || second.FromName != "" {
		t.Errorf("bare sender: got from=%q name=%q", second.From, second.FromName)
	}
	if second.Unread {
		t.Errorf("expected second message read (no UNREAD label)")
	}
}

func TestSanitizeGmailMessages_LimitCapAndSnippetCap(t *testing.T) {
	// limit caps the returned count even if upstream returns more.
	emails, err := sanitizeGmailMessages(json.RawMessage(sampleGmailEnvelope), 1)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if len(emails) != 1 {
		t.Fatalf("limit not applied: got %d", len(emails))
	}

	long := strings.Repeat("x", gmailSnippetMax+50)
	envelope := `{"data":{"messages":[{"messageId":"a","preview":{"body":"` + long + `"}}]}}`
	out, err := sanitizeGmailMessages(json.RawMessage(envelope), 25)
	if err != nil {
		t.Fatalf("sanitize long: %v", err)
	}
	if len(out[0].Snippet) != gmailSnippetMax {
		t.Errorf("snippet not capped: got len %d", len(out[0].Snippet))
	}
}

func TestSplitEmailSender(t *testing.T) {
	cases := []struct {
		in       string
		wantAddr string
		wantName string
	}{
		{`Sentry <noreply@getsentry.com>`, "noreply@getsentry.com", "Sentry"},
		{`"Doe, John" <john@x.com>`, "john@x.com", "Doe, John"},
		{`bare@example.com`, "bare@example.com", ""},
		{``, "", ""},
		{`<only@addr.com>`, "only@addr.com", ""},
	}
	for _, c := range cases {
		addr, name := splitEmailSender(c.in)
		if addr != c.wantAddr || name != c.wantName {
			t.Errorf("splitEmailSender(%q) = (%q,%q), want (%q,%q)", c.in, addr, name, c.wantAddr, c.wantName)
		}
	}
}
