package team

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

func TestRegisterSlackAgent_CreatesBridgedMemberWithBinding(t *testing.T) {
	b := newTestBrokerWithSlackChannel(t, "C0123")

	created, err := b.RegisterSlackAgent("claude-bot", "Claude Bot", "U777")
	if err != nil {
		t.Fatalf("RegisterSlackAgent: %v", err)
	}
	if !created {
		t.Fatal("first registration should report created=true")
	}

	var member *officeMember
	for _, m := range b.OfficeMembers() {
		if m.Slug == "claude-bot" {
			mm := m
			member = &mm
		}
	}
	if member == nil {
		t.Fatal("registered agent should be an office member")
	}
	if member.CreatedBy != "slack" || member.Role != "Bridged agent" {
		t.Fatalf("member = %+v, want CreatedBy=slack Role=Bridged agent", member)
	}
	if member.Provider.Kind != provider.KindSlack || member.Provider.Slack == nil || member.Provider.Slack.UserID != "U777" {
		t.Fatalf("provider binding = %+v, want slack/U777", member.Provider)
	}

	// Both registry lookups round-trip.
	if got := b.SlackAgentSlugByUserID("U777"); got != "claude-bot" {
		t.Fatalf("SlackAgentSlugByUserID = %q, want claude-bot", got)
	}
	if got := b.SlackAgentUserIDBySlug("claude-bot"); got != "U777" {
		t.Fatalf("SlackAgentUserIDBySlug = %q, want U777", got)
	}

	// The agent joined the bridged room (not just #general).
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := b.findChannelLocked("slack-general")
	if ch == nil || !containsString(ch.Members, "claude-bot") {
		t.Fatalf("agent should be a member of the bridged slack channel, got %+v", ch)
	}
}

func TestRegisterSlackAgent_IdempotentAndConflicts(t *testing.T) {
	b := newTestBroker(t)
	if _, err := b.RegisterSlackAgent("claude-bot", "Claude Bot", "U777"); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	// Identical pair → idempotent, created=false.
	created, err := b.RegisterSlackAgent("claude-bot", "Claude Bot", "U777")
	if err != nil || created {
		t.Fatalf("re-registration = (created=%v, err=%v), want (false, nil)", created, err)
	}

	// Same slug, different Slack user → conflict.
	if _, err := b.RegisterSlackAgent("claude-bot", "Impostor", "U888"); err == nil {
		t.Fatal("rebinding a slug to a different slack user must error")
	}

	// Same Slack user, different slug → conflict (attribution stays 1:1).
	if _, err := b.RegisterSlackAgent("other-bot", "Other", "U777"); err == nil {
		t.Fatal("registering one slack user under two slugs must error")
	}

	// A slug already naming a native member → conflict, and the native member's
	// provider binding must be untouched.
	if _, err := b.RegisterSlackAgent("ceo", "Hijack", "U999"); err == nil {
		t.Fatal("hijacking a native member slug must error")
	}
	if got := b.MemberProviderKind("ceo"); got == provider.KindSlack {
		t.Fatal("conflicting registration must not mutate the native member")
	}
}

func TestHandleSlackAgents_HTTP(t *testing.T) {
	b := newTestBroker(t)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/slack/agents", strings.NewReader(body))
		w := httptest.NewRecorder()
		b.handleSlackAgents(w, req)
		return w
	}

	if w := post(`{"user_id":"U777","name":"Claude Bot"}`); w.Code != http.StatusOK {
		t.Fatalf("register status = %d, body=%s", w.Code, w.Body.String())
	}
	// Slug derives from the name.
	if got := b.SlackAgentSlugByUserID("U777"); got != "claude-bot" {
		t.Fatalf("derived slug = %q, want claude-bot", got)
	}

	if w := post(`{"user_id":"B123","name":"bad"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("non-user id should 400, got %d", w.Code)
	}
	if w := post(`{"name":"no id"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing user_id should 400, got %d", w.Code)
	}
	if w := post(`{"user_id":"U888","slug":"claude-bot","name":"Impostor"}`); w.Code != http.StatusConflict {
		t.Fatalf("slug conflict should 409, got %d", w.Code)
	}

	// GET lists the registration.
	req := httptest.NewRequest(http.MethodGet, "/slack/agents", nil)
	w := httptest.NewRecorder()
	b.handleSlackAgents(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"U777"`) {
		t.Fatalf("list = %d %s, want 200 containing U777", w.Code, w.Body.String())
	}
}

func TestIsSlackUserID(t *testing.T) {
	for _, id := range []string{"U0123", "W0456"} {
		if !isSlackUserID(id) {
			t.Fatalf("%q should be a valid slack user id", id)
		}
	}
	for _, id := range []string{"", "C0123", "B0123", "u", "U01 23", "U<@here>", "u0123", "Uabc"} {
		if isSlackUserID(id) {
			t.Fatalf("%q should not be a valid slack user id", id)
		}
	}
}
