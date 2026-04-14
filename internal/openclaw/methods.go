package openclaw

import (
	"context"
	"encoding/json"
)

// SessionsListFilter mirrors OpenClaw SessionsListParams.
type SessionsListFilter struct {
	Limit              int      `json:"limit,omitempty"`
	ActiveMinutes      int      `json:"activeMinutes,omitempty"`
	Kinds              []string `json:"kinds,omitempty"`
	IncludeLastMessage bool     `json:"includeLastMessage,omitempty"`
	Search             string   `json:"search,omitempty"`
}

// SessionRow is the subset of OpenClaw SessionListRow WUPHF needs.
type SessionRow struct {
	SessionKey  string `json:"sessionKey"`
	Label       string `json:"label,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Kind        string `json:"kind,omitempty"`
	LastMessage string `json:"lastMessage,omitempty"`
}

type sessionsListResult struct {
	Sessions []SessionRow `json:"sessions"`
	Path     string       `json:"path,omitempty"`
}

func (c *Client) SessionsList(ctx context.Context, f SessionsListFilter) ([]SessionRow, error) {
	raw, err := c.Call(ctx, "sessions.list", f)
	if err != nil {
		return nil, err
	}
	var res sessionsListResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	return res.Sessions, nil
}

// SessionsSend fires a message into an OpenClaw session. Reply arrives as events.
// idempotencyKey MUST be reused across retries of the same logical send.
func (c *Client) SessionsSend(ctx context.Context, key, message, idempotencyKey string) error {
	params := map[string]any{"key": key, "message": message}
	if idempotencyKey != "" {
		params["idempotencyKey"] = idempotencyKey
	}
	_, err := c.Call(ctx, "sessions.send", params)
	return err
}

func (c *Client) SessionsMessagesSubscribe(ctx context.Context, key string) error {
	_, err := c.Call(ctx, "sessions.messages.subscribe", map[string]any{"key": key})
	return err
}

func (c *Client) SessionsMessagesUnsubscribe(ctx context.Context, key string) error {
	_, err := c.Call(ctx, "sessions.messages.unsubscribe", map[string]any{"key": key})
	return err
}

// HistoricMessage is a minimal representation of a past transcript entry.
type HistoricMessage struct {
	SessionKey string          `json:"sessionKey,omitempty"`
	Seq        *int64          `json:"messageSeq,omitempty"`
	Message    json.RawMessage `json:"message,omitempty"`
}

// SessionsHistory fetches transcript entries since sinceSeq for gap catch-up.
// Returns messages in chronological order.
func (c *Client) SessionsHistory(ctx context.Context, key string, sinceSeq int64) ([]HistoricMessage, error) {
	params := map[string]any{"sessionKey": key}
	if sinceSeq > 0 {
		params["sinceSeq"] = sinceSeq
	}
	raw, err := c.Call(ctx, "sessions.history", params)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Messages []HistoricMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		// Fallback: some servers return a bare array.
		var arr []HistoricMessage
		if err2 := json.Unmarshal(raw, &arr); err2 == nil {
			return arr, nil
		}
		return nil, err
	}
	return wrap.Messages, nil
}
