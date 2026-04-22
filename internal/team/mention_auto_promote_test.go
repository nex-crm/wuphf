package team

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// Real-world failure (v0.0.6.1): user typed "@pm do you know of Lenny's PM fit
// frameworks?" in #general. The web composer did not commit `@pm` into an
// explicit tag chip, so the POST body had empty `tagged`. The broker refused
// to auto-promote because the sender was `you`/`human`, so the message was
// posted with `Tagged: []`. Routing hit `addImmediate(lead)` and CEO absorbed
// the message instead of PM.
//
// Fix: auto-promote body @-mentions for human senders too. extractMentionedSlugs
// already restricts to registered agent slugs, so conversational @-text that
// doesn't match an agent is untouched.

func newBrokerWithPM(t *testing.T) *Broker {
	t.Helper()
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	t.Cleanup(func() { brokerStatePath = oldPathFn })

	b := NewBroker()
	b.mu.Lock()
	b.members = append(b.members, officeMember{Slug: "pm", Name: "Product Manager"})
	b.mu.Unlock()
	return b
}

func postMessage(t *testing.T, b *Broker, from, channel, content string, tagged []string) channelMessage {
	t.Helper()
	body := map[string]any{
		"from":    from,
		"channel": channel,
		"content": content,
	}
	if tagged != nil {
		body["tagged"] = tagged
	}
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/messages", bytes.NewReader(buf))
	req.Header.Set("Authorization", "Bearer "+b.token)
	rec := httptest.NewRecorder()
	b.handlePostMessage(rec, req)
	if rec.Code != http.StatusOK {
		resBody, _ := io.ReadAll(rec.Result().Body)
		t.Fatalf("post message status=%d body=%s", rec.Code, string(resBody))
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, m := range b.messages {
		if m.ID == resp.ID {
			return m
		}
	}
	t.Fatalf("message %s not found", resp.ID)
	return channelMessage{}
}

func TestAutoPromote_HumanTypedAtPM_PromotesToTagged(t *testing.T) {
	b := newBrokerWithPM(t)

	// User types `@pm do you know of Lenny's PM fit frameworks?` with NO
	// explicit tag chip (web composer didn't commit it).
	msg := postMessage(t, b, "you", "general",
		"@pm do you know of Lenny's PM fit frameworks?", nil)

	if !containsString(msg.Tagged, "pm") {
		t.Fatalf("bug reproduced: human's `@pm` text was not auto-promoted to tagged; got %+v", msg.Tagged)
	}
}

func TestAutoPromote_HumanTypedAtNonAgent_LeavesUntagged(t *testing.T) {
	b := newBrokerWithPM(t)

	// Conversational @-reference that is NOT a registered agent slug must
	// stay untagged — the original defensive behaviour for non-agent @-text.
	msg := postMessage(t, b, "you", "general",
		"email @joedoe for the spec", nil)

	if len(msg.Tagged) != 0 {
		t.Fatalf("non-agent @-reference was promoted to tagged; got %+v", msg.Tagged)
	}
}

func TestAutoPromote_AgentTypedAtPM_StillWorks(t *testing.T) {
	// Regression guard: agent-sender auto-promote (the pre-existing behaviour)
	// must keep working after the human-sender path was widened.
	b := newBrokerWithPM(t)

	msg := postMessage(t, b, "ceo", "general",
		"@pm — quick one for you", nil)

	if !containsString(msg.Tagged, "pm") {
		t.Fatalf("agent's `@pm` text was not auto-promoted to tagged; got %+v", msg.Tagged)
	}
}

func TestAutoPromote_ExplicitTagRespected(t *testing.T) {
	// When the web composer DID commit an explicit tag chip, the tagged
	// array arrives populated. Must not duplicate.
	b := newBrokerWithPM(t)

	msg := postMessage(t, b, "you", "general",
		"@pm please scope this", []string{"pm"})

	pmCount := 0
	for _, slug := range msg.Tagged {
		if slug == "pm" {
			pmCount++
		}
	}
	if pmCount != 1 {
		t.Fatalf("expected pm exactly once in tagged; got %+v", msg.Tagged)
	}
}

// Real-world failure (v0.0.6.1): after onboarding reseeded the roster, the
// launcher logged `office_reseeded: respawn panes failed: ... tmux: no server
// running on /private/tmp/tmux-501/wuphf` on every reseed. In web/headless
// mode there is no tmux server by design — the headless dispatch path handles
// delivery. Logging the expected attach-failure as an error made the console
// look like it was failing repeatedly.
//
// Fix: swallow isNoSessionError in respawnPanesAfterReseed — headless mode
// will take over silently. Other error types (permission denied, corrupted
// state) keep surfacing.

func TestRespawnPanesAfterReseed_NoSessionErrorSilenced(t *testing.T) {
	// Cover the error-classification logic directly. Real tmux invocation
	// happens inside reconfigureVisibleAgents and is not unit-testable here,
	// but the decision to silence vs log lives in isNoSessionError.
	cases := []struct {
		name    string
		errMsg  string
		silence bool
	}{
		{"no server running", "tmux: no server running on /private/tmp/tmux-501/wuphf", true},
		{"spawn first agent wrapper", "spawn first agent: exit status 1 (tmux: no server running on /tmp/tmux-501/wuphf)", true},
		{"cant find session", "can't find session", true},
		{"permission denied", "permission denied accessing /tmp/tmux-501", false},
		{"generic spawn fail", "exec: no such file or directory", false},
	}
	for _, c := range cases {
		got := isNoSessionError(c.errMsg)
		if got != c.silence {
			t.Errorf("%s: isNoSessionError(%q) = %v, want %v", c.name, c.errMsg, got, c.silence)
		}
	}
}
