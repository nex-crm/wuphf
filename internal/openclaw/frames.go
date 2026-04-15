// Package openclaw implements the WUPHF side of the OpenClaw gateway bridge:
// frame types, JSON codec, and (in later tasks) the transport client.
package openclaw

import (
	"encoding/json"
	"fmt"
)

// RequestFrame is a WUPHF→OpenClaw gateway request.
type RequestFrame struct {
	Type   string `json:"type"` // always "req"
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

// ResponseFrame is a gateway→WUPHF response to a request.
type ResponseFrame struct {
	Type    string          `json:"type"` // always "res"
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *ErrorShape     `json:"error,omitempty"`
}

// EventFrame is a gateway→WUPHF push event.
type EventFrame struct {
	Type    string          `json:"type"` // always "event"
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Seq     *int64          `json:"seq,omitempty"`
}

// ErrorShape mirrors the OpenClaw ErrorShapeSchema.
type ErrorShape struct {
	Code         string          `json:"code"`
	Message      string          `json:"message"`
	Details      json.RawMessage `json:"details,omitempty"`
	Retryable    *bool           `json:"retryable,omitempty"`
	RetryAfterMs *int64          `json:"retryAfterMs,omitempty"`
}

// DecodeFrame peeks at the "type" discriminator and returns ("req"|"res"|"event", raw, err).
// Callers then re-unmarshal into the concrete frame type they expect.
func DecodeFrame(raw []byte) (string, json.RawMessage, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return "", nil, fmt.Errorf("frame: decode head: %w", err)
	}
	switch head.Type {
	case "req", "res", "event":
		return head.Type, raw, nil
	default:
		return "", nil, fmt.Errorf("frame: unknown type %q", head.Type)
	}
}
