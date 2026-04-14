package openclaw

import "encoding/json"

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
type SessionMessageEvent struct {
	SessionKey   string          `json:"sessionKey"`
	MessageID    string          `json:"messageId,omitempty"`
	MessageSeq   *int64          `json:"messageSeq,omitempty"`
	Message      json.RawMessage `json:"message,omitempty"`
	MessageState string          `json:"-"`
	MessageText  string          `json:"-"`
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

func parseSessionMessage(raw json.RawMessage) (*SessionMessageEvent, error) {
	var e SessionMessageEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, err
	}
	var inner struct {
		State   string `json:"state"`
		Content string `json:"content"`
		Text    string `json:"text"`
	}
	if len(e.Message) > 0 {
		_ = json.Unmarshal(e.Message, &inner)
		e.MessageState = inner.State
		if inner.Content != "" {
			e.MessageText = inner.Content
		} else {
			e.MessageText = inner.Text
		}
	}
	return &e, nil
}

func parseSessionsChanged(raw json.RawMessage) (*SessionsChangedEvent, error) {
	var e SessionsChangedEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, err
	}
	return &e, nil
}
