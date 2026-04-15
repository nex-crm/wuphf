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
	AgentID            string   `json:"agentId,omitempty"`
}

// SessionRow is the subset of OpenClaw session-list-row WUPHF needs.
//
// The real daemon uses "key" as the session identifier (NOT "sessionKey").
// Verified 2026-04-15 against OpenClaw 2026.4.14 sessions.list output.
type SessionRow struct {
	Key         string `json:"key"`
	Label       string `json:"label,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Kind        string `json:"kind,omitempty"`
	ChatType    string `json:"chatType,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	LastMessage string `json:"lastMessage,omitempty"`
	UpdatedAt   int64  `json:"updatedAt,omitempty"`
}

type sessionsListResult struct {
	Sessions []SessionRow `json:"sessions"`
	Path     string       `json:"path,omitempty"`
	Count    int          `json:"count,omitempty"`
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

// SessionsSend fires a message into an OpenClaw session. The agent's reply
// arrives asynchronously as session.message events.
//
// idempotencyKey MUST be reused across retries of the same logical send so the
// gateway deduplicates. The returned runId identifies the turn on the daemon
// side and can be correlated with chat/session events.
type SessionsSendResult struct {
	RunID      string `json:"runId,omitempty"`
	Status     string `json:"status,omitempty"` // "started" | ...
	MessageSeq int64  `json:"messageSeq,omitempty"`
}

func (c *Client) SessionsSend(ctx context.Context, key, message, idempotencyKey string) (*SessionsSendResult, error) {
	params := map[string]any{"key": key, "message": message}
	if idempotencyKey != "" {
		params["idempotencyKey"] = idempotencyKey
	}
	raw, err := c.Call(ctx, "sessions.send", params)
	if err != nil {
		return nil, err
	}
	var res SessionsSendResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) SessionsMessagesSubscribe(ctx context.Context, key string) error {
	_, err := c.Call(ctx, "sessions.messages.subscribe", map[string]any{"key": key})
	return err
}

func (c *Client) SessionsMessagesUnsubscribe(ctx context.Context, key string) error {
	_, err := c.Call(ctx, "sessions.messages.unsubscribe", map[string]any{"key": key})
	return err
}
