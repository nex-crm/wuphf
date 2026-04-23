package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// User typed "@pm do you know of Lenny's PM fit frameworks?" in #general.
// The web composer did not commit `@pm` into an explicit tag chip, so the
// POST body had empty `tagged`. The broker refused to auto-promote because
// the sender was `you`, and the message posted with `Tagged: []`. Routing
// then hit `addImmediate(lead)` and CEO absorbed the message instead of PM.
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

// Synthetic senders (`system`, `nex`, bridges, future automation kinds) MUST
// NOT auto-promote. A denylist approach would leak every new synthetic
// identity — the allowlist in senderMayAutoPromoteLocked stops that.
// handlePostMessage rejects `system` at the channel-access gate before the
// promote block runs, so exercise the allowlist directly here.
func TestAutoPromote_SystemAndSyntheticSendersSkipped(t *testing.T) {
	b := newBrokerWithPM(t)

	cases := []struct {
		name string
		from string
	}{
		{"system", "system"},
		{"nex", "nex"},
		{"bridge-slack", "bridge-slack"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b.mu.Lock()
			allowed := b.senderMayAutoPromoteLocked(c.from)
			b.mu.Unlock()
			if allowed {
				t.Fatalf("senderMayAutoPromoteLocked(%q) = true; synthetic senders must be denied", c.from)
			}
		})
	}
}

// End-to-end: the user's exact reproduction scenario, traversing every layer
// from HTTP POST to headless dispatch. Human posts `@pm` as text with
// tagged=[], broker auto-promotes, launcher dispatches to pm (not CEO).
// If either the broker fix (auto-promote) or the launcher fix (routing, PR
// #218) regresses, this test catches it.
func TestAutoPromote_EndToEnd_HumanTagsPM_DispatchesToPM(t *testing.T) {
	b := newBrokerWithPM(t)

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.provider = "codex"
	l.notifyLastDelivered = make(map[string]time.Time)
	l.pack = &agent.PackDefinition{
		LeadSlug: "ceo",
		Agents: []agent.AgentConfig{
			{Slug: "ceo", Name: "CEO"},
			{Slug: "pm", Name: "Product Manager"},
		},
	}

	processed := make(chan string, 4)
	oldRunTurn := headlessCodexRunTurn
	headlessCodexRunTurn = func(_ *Launcher, _ context.Context, slug, notification string, channel ...string) error {
		ch := ""
		if len(channel) > 0 {
			ch = channel[0]
		}
		processed <- slug + "|" + ch + "|" + notification
		return nil
	}
	defer func() { headlessCodexRunTurn = oldRunTurn }()

	// Simulate the user's exact flow: POST /messages with @pm in body and
	// tagged empty (composer didn't commit a chip).
	msg := postMessage(t, b, "you", "general",
		"@pm do you know of Lenny's PM fit frameworks?", nil)
	if !containsString(msg.Tagged, "pm") {
		t.Fatalf("stage 1: broker did not auto-promote @pm; tagged=%+v", msg.Tagged)
	}

	// Now dispatch: the launcher should wake PM (not CEO absorbing it).
	l.deliverMessageNotification(msg)

	// Wait for at least one dispatch, then non-blocking drain. Avoids paying
	// the full deadline on every green-path run.
	var slugs []string
	select {
	case turn := <-processed:
		if idx := strings.Index(turn, "|"); idx > 0 {
			slugs = append(slugs, turn[:idx])
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("stage 2: no dispatch within 500ms after @pm message")
	}
drain:
	for {
		select {
		case turn := <-processed:
			if idx := strings.Index(turn, "|"); idx > 0 {
				slugs = append(slugs, turn[:idx])
			}
		default:
			break drain
		}
	}

	pmWoken := false
	for _, s := range slugs {
		if s == "pm" {
			pmWoken = true
			break
		}
	}
	if !pmWoken {
		t.Fatalf("stage 2: PM was not dispatched after @pm message (CEO absorbed it). turns=%v", slugs)
	}
}

// Root-cause test: pin the tmux-error classification that decides whether
// respawnPanesAfterReseed silences or logs. Web/headless mode cannot run
// tmux directly in a unit test, but the branch selector
// (isMissingTmuxSession) is load-bearing and a public, well-tested function.
func TestRespawnPanesAfterReseed_TmuxNoServerClassifiedAsMissing(t *testing.T) {
	cases := []struct {
		name    string
		errMsg  string
		silence bool
	}{
		{"no server running", "tmux: no server running on /private/tmp/tmux-501/wuphf", true},
		{"spawn first agent wrapper", "spawn first agent: exit status 1 (tmux: no server running on /tmp/tmux-501/wuphf)", true},
		{"cant find session", "can't find session", true},
		{"failed to connect to server", "tmux: failed to connect to server", true},
		{"permission denied", "permission denied accessing /tmp/tmux-501", false},
		{"generic exec fail without tmux prefix", "exec: binary unreadable", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isMissingTmuxSession(c.errMsg); got != c.silence {
				t.Errorf("isMissingTmuxSession(%q) = %v, want %v", c.errMsg, got, c.silence)
			}
		})
	}
}
