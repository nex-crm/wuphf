package openclaw

import (
	"encoding/json"
	"strings"
)

// ClientEventKind discriminates the ClientEvent union.
type ClientEventKind int

const (
	EventKindMessage ClientEventKind = iota + 1
	EventKindChanged
	EventKindGap
	EventKindClose
)

// ClientEvent is the discriminated union emitted on Client.Events().
// Exactly one of the pointer fields is non-nil for Kind != EventKindClose.
type ClientEvent struct {
	Kind            ClientEventKind
	SessionMessage  *SessionMessageEvent
	SessionsChanged *SessionsChangedEvent
	Gap             *GapEvent
	CloseErr        error // set when Kind == EventKindClose
}

// SessionMessageEvent mirrors the OpenClaw "session.message" payload.
//
// Real-daemon shape (observed 2026-04-15 against OpenClaw 2026.4.14):
//
//	{
//	  "sessionKey": "agent:main:main",
//	  "message": {
//	    "role": "user" | "assistant",
//	    "content": "<string>"            // role=user
//	              | [{"type":"text","text":"..."}]  // role=assistant (array of parts)
//	    "timestamp": 1776254522461,
//	    "__openclaw": {"seq": 0}
//	  },
//	  "messageSeq": 0,
//	  "session": {...},
//	  ...
//	}
type SessionMessageEvent struct {
	SessionKey  string          `json:"sessionKey"`
	MessageID   string          `json:"messageId,omitempty"`
	MessageSeq  *int64          `json:"messageSeq,omitempty"`
	Message     json.RawMessage `json:"message,omitempty"`
	Role        string          `json:"-"` // "user" | "assistant" | ""
	MessageText string          `json:"-"` // extracted text for display
}

// SessionsChangedEvent mirrors "sessions.changed".
type SessionsChangedEvent struct {
	SessionKey string `json:"sessionKey"`
	Reason     string `json:"reason,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Label      string `json:"label,omitempty"`
}

// GapEvent is synthesized by the client when event seq numbers skip.
type GapEvent struct {
	SessionKey string
	FromSeq    int64 // last seq we had
	ToSeq      int64 // seq we just received
}

// parseSessionMessage extracts role + text from the polymorphic OpenClaw message
// shape. `content` is either a string (user message) or an array of content parts
// for assistant messages; we concatenate all `type:"text"` parts.
func parseSessionMessage(raw json.RawMessage) (*SessionMessageEvent, error) {
	var e SessionMessageEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, err
	}
	if len(e.Message) == 0 {
		return &e, nil
	}
	var envelope struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Text    string          `json:"text"`
	}
	if err := json.Unmarshal(e.Message, &envelope); err == nil {
		e.Role = envelope.Role
		e.MessageText = extractMessageText(envelope.Content, envelope.Text)
	}
	return &e, nil
}

func extractMessageText(content json.RawMessage, fallback string) string {
	if len(content) == 0 {
		return fallback
	}
	// Fast path: string content (user messages).
	var asString string
	if err := json.Unmarshal(content, &asString); err == nil {
		return asString
	}
	// Array of parts (assistant messages).
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(p.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return fallback
}

func parseSessionsChanged(raw json.RawMessage) (*SessionsChangedEvent, error) {
	var e SessionsChangedEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, err
	}
	return &e, nil
}
