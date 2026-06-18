package team

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

// spawnPost drives POST /slack/agents/spawn directly against the handler.
func spawnPost(t *testing.T, b *Broker, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/slack/agents/spawn", strings.NewReader(body))
	w := httptest.NewRecorder()
	b.handleSlackAgentsSpawn(w, req)
	return w
}

// spawnComplete drives POST /slack/agents/spawn/complete directly.
func spawnComplete(t *testing.T, b *Broker, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/slack/agents/spawn/complete", strings.NewReader(body))
	w := httptest.NewRecorder()
	b.handleSlackAgentsSpawnComplete(w, req)
	return w
}

func TestHandleSlackAgentsSpawn_ManifestAndGuide(t *testing.T) {
	b := newTestBroker(t)

	w := spawnPost(t, b, `{"slug":"researcher","name":"Ress","role":"Research"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("spawn status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Slug     string           `json:"slug"`
		Name     string           `json:"name"`
		Role     string           `json:"role"`
		TokenEnv string           `json:"token_env"`
		Manifest slackAppManifest `json:"manifest"`
		Guide    []string         `json:"guide"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode spawn response: %v", err)
	}
	if resp.Slug != "researcher" || resp.Name != "Ress" || resp.Role != "Research" {
		t.Fatalf("identity = %+v, want researcher/Ress/Research", resp)
	}
	if resp.TokenEnv != "WUPHF_SLACK_AGENT_RESEARCHER_TOKEN" {
		t.Fatalf("token_env = %q", resp.TokenEnv)
	}
	// The manifest names the bot "Name (Role)" so the role is legible in Slack,
	// and asks for the posting + native-status scopes (spawned agents POST as
	// themselves and show the native "is thinking…" status).
	if resp.Manifest.DisplayInformation.Name != "Ress (Research)" || resp.Manifest.Features.BotUser.DisplayName != "Ress (Research)" {
		t.Fatalf("manifest bot name = %+v, want Ress (Research)", resp.Manifest)
	}
	if got := resp.Manifest.OauthConfig.Scopes.Bot; len(got) != 3 || got[0] != "assistant:write" || got[1] != "chat:write" || got[2] != "users:read" {
		t.Fatalf("scopes = %v, want [assistant:write chat:write users:read]", got)
	}
	// No socket mode / event subscriptions: inbound stays on the main bot.
	if strings.Contains(w.Body.String(), "socket_mode") || strings.Contains(w.Body.String(), "event_subscriptions") {
		t.Fatalf("manifest must not enable socket mode or events: %s", w.Body.String())
	}
	// The guide is numbered and names the env var + complete endpoint; the
	// raw token never transits a request body.
	if len(resp.Guide) != 6 {
		t.Fatalf("guide steps = %d, want 6", len(resp.Guide))
	}
	joined := strings.Join(resp.Guide, "\n")
	for _, want := range []string{
		"api.slack.com/apps",
		"WUPHF_SLACK_AGENT_RESEARCHER_TOKEN",
		"/slack/agents/spawn/complete",
		"/invite @Ress",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("guide missing %q:\n%s", want, joined)
		}
	}

	// GET lists the pending spawn.
	req := httptest.NewRequest(http.MethodGet, "/slack/agents/spawn", nil)
	get := httptest.NewRecorder()
	b.handleSlackAgentsSpawn(get, req)
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"researcher"`) {
		t.Fatalf("list = %d %s, want 200 containing researcher", get.Code, get.Body.String())
	}

	// Re-spawning the same slug is idempotent (the guide can be re-issued).
	if w := spawnPost(t, b, `{"slug":"researcher","name":"Ress"}`); w.Code != http.StatusOK {
		t.Fatalf("re-spawn status = %d", w.Code)
	}
	if got := len(b.pendingSlackSpawns()); got != 1 {
		t.Fatalf("pending spawns = %d, want 1", got)
	}
}

func TestHandleSlackAgentsSpawn_Validation(t *testing.T) {
	b := newTestBroker(t)

	if w := spawnPost(t, b, `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty body should 400, got %d", w.Code)
	}
	if w := spawnPost(t, b, `{"slug":"general"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("reserved slug should 400, got %d", w.Code)
	}
	// Existing native member → conflict.
	if w := spawnPost(t, b, `{"slug":"ceo"}`); w.Code != http.StatusConflict {
		t.Fatalf("existing member slug should 409, got %d", w.Code)
	}
	// Registered foreign agent → conflict (it is an office member too).
	if _, err := b.RegisterSlackAgent("claude-bot", "Claude Bot", "U777"); err != nil {
		t.Fatalf("RegisterSlackAgent: %v", err)
	}
	if w := spawnPost(t, b, `{"slug":"claude-bot"}`); w.Code != http.StatusConflict {
		t.Fatalf("foreign agent slug should 409, got %d", w.Code)
	}
	// Name-only requests derive the slug (and default the name handling).
	if w := spawnPost(t, b, `{"name":"Research Agent"}`); w.Code != http.StatusOK {
		t.Fatalf("name-only spawn = %d, body=%s", w.Code, w.Body.String())
	}
	if got := b.pendingSlackSpawns(); len(got) != 1 || got[0].Slug != "research-agent" {
		t.Fatalf("pending = %+v, want one record with slug research-agent", got)
	}
}

func TestSlackSpawnPendingSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broker-state.json")
	b := NewBrokerAt(path)
	if _, err := b.SpawnSlackAgent("researcher", "Ress", "Research"); err != nil {
		t.Fatalf("SpawnSlackAgent: %v", err)
	}

	b2 := NewBrokerAt(path)
	if err := b2.loadState(); err != nil {
		t.Fatalf("loadState: %v", err)
	}
	got := b2.pendingSlackSpawns()
	if len(got) != 1 || got[0].Slug != "researcher" || got[0].TokenEnv != "WUPHF_SLACK_AGENT_RESEARCHER_TOKEN" {
		t.Fatalf("reloaded pending spawns = %+v", got)
	}
}

func TestCompleteSlackAgentSpawn_CreatesRealOfficeAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broker-state.json")
	b := NewBrokerAt(path)
	b.slackSpawnAuthTest = func(_ context.Context, token string) (string, string, error) {
		if token != "bot-token-test-555" {
			t.Fatalf("auth.test called with token %q", token)
		}
		return "U555", "Ress", nil
	}
	if _, err := b.SpawnSlackAgent("researcher", "Ress", "Research"); err != nil {
		t.Fatalf("SpawnSlackAgent: %v", err)
	}
	t.Setenv("WUPHF_SLACK_AGENT_RESEARCHER_TOKEN", "bot-token-test-555")

	w := spawnComplete(t, b, `{"slug":"researcher"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("complete status = %d, body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"U555"`) || !strings.Contains(w.Body.String(), `"created":true`) {
		t.Fatalf("complete body = %s", w.Body.String())
	}

	// The member is a REAL office agent on the default runtime, carrying its
	// Slack identity — NOT a gateway/foreign kind.
	var member *officeMember
	for _, m := range b.OfficeMembers() {
		if m.Slug == "researcher" {
			mm := m
			member = &mm
		}
	}
	if member == nil {
		t.Fatal("completed spawn should be an office member")
	}
	if member.Provider.Kind != "" || provider.IsGatewayKind(member.Provider.Kind) {
		t.Fatalf("provider kind = %q, want install-default (empty)", member.Provider.Kind)
	}
	if member.Provider.Slack == nil || member.Provider.Slack.UserID != "U555" ||
		member.Provider.Slack.BotTokenEnv != "WUPHF_SLACK_AGENT_RESEARCHER_TOKEN" {
		t.Fatalf("slack binding = %+v", member.Provider.Slack)
	}
	if member.CreatedBy != "slack-spawn" || member.Role != "Research" {
		t.Fatalf("member = %+v, want CreatedBy=slack-spawn Role=Research", member)
	}

	// Spawned ≠ foreign: the echo-guard lookup matches, the ingress
	// allowlist does NOT (its posts must never re-ingress).
	if !b.IsSpawnedSlackAgentUserID("U555") {
		t.Fatal("IsSpawnedSlackAgentUserID(U555) should be true")
	}
	if got := b.SlackAgentSlugByUserID("U555"); got != "" {
		t.Fatalf("SlackAgentSlugByUserID = %q, want empty (not a foreign agent)", got)
	}
	if got := b.SpawnedSlackAgentTokenEnv("researcher"); got != "WUPHF_SLACK_AGENT_RESEARCHER_TOKEN" {
		t.Fatalf("SpawnedSlackAgentTokenEnv = %q", got)
	}

	// Pending record is cleared; re-completing is idempotent only via a new
	// spawn record, so a bare retry 404s.
	if got := len(b.pendingSlackSpawns()); got != 0 {
		t.Fatalf("pending spawns after complete = %d, want 0", got)
	}

	// The binding survives a restart.
	b2 := NewBrokerAt(path)
	if err := b2.loadState(); err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if !b2.IsSpawnedSlackAgentUserID("U555") {
		t.Fatal("spawned binding should survive a restart")
	}
	if got := b2.SpawnedSlackAgentTokenEnv("researcher"); got != "WUPHF_SLACK_AGENT_RESEARCHER_TOKEN" {
		t.Fatalf("reloaded SpawnedSlackAgentTokenEnv = %q", got)
	}
}

func TestCompleteSlackAgentSpawn_Errors(t *testing.T) {
	b := newTestBroker(t)

	// No pending spawn → 404.
	if w := spawnComplete(t, b, `{"slug":"ghost"}`); w.Code != http.StatusNotFound {
		t.Fatalf("unknown slug should 404, got %d", w.Code)
	}
	if w := spawnComplete(t, b, `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing slug should 400, got %d", w.Code)
	}

	if _, err := b.SpawnSlackAgent("researcher", "Ress", ""); err != nil {
		t.Fatalf("SpawnSlackAgent: %v", err)
	}

	// Env var unset → 409 naming the env var, never a token in the body.
	w := spawnComplete(t, b, `{"slug":"researcher"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "WUPHF_SLACK_AGENT_RESEARCHER_TOKEN") {
		t.Fatalf("missing env should 409 naming the env var, got %d %s", w.Code, w.Body.String())
	}

	// auth.test failure surfaces as a conflict-class error, not a member.
	t.Setenv("WUPHF_SLACK_AGENT_RESEARCHER_TOKEN", "xoxb-bad")
	b.slackSpawnAuthTest = func(context.Context, string) (string, string, error) {
		return "", "", errors.New("invalid_auth")
	}
	if w := spawnComplete(t, b, `{"slug":"researcher"}`); w.Code != http.StatusConflict {
		t.Fatalf("auth failure should 409, got %d", w.Code)
	}
	if b.hasMember("researcher") {
		t.Fatal("failed complete must not create a member")
	}

	// The discovered user id colliding with a registered FOREIGN agent is
	// rejected — attribution stays one-to-one across both registries.
	if _, err := b.RegisterSlackAgent("claude-bot", "Claude Bot", "U777"); err != nil {
		t.Fatalf("RegisterSlackAgent: %v", err)
	}
	b.slackSpawnAuthTest = func(context.Context, string) (string, string, error) {
		return "U777", "Ress", nil
	}
	if w := spawnComplete(t, b, `{"slug":"researcher"}`); w.Code != http.StatusConflict {
		t.Fatalf("user-id collision should 409, got %d", w.Code)
	}

	// Malformed auth.test responses are rejected too.
	b.slackSpawnAuthTest = func(context.Context, string) (string, string, error) {
		return "not-a-user-id", "Ress", nil
	}
	if w := spawnComplete(t, b, `{"slug":"researcher"}`); w.Code != http.StatusConflict {
		t.Fatalf("invalid user id should 409, got %d", w.Code)
	}
}

func TestRegisterSlackAgent_RejectsSpawnedIdentity(t *testing.T) {
	b := newTestBroker(t)
	b.slackSpawnAuthTest = func(context.Context, string) (string, string, error) {
		return "U555", "Ress", nil
	}
	if _, err := b.SpawnSlackAgent("researcher", "Ress", ""); err != nil {
		t.Fatalf("SpawnSlackAgent: %v", err)
	}
	t.Setenv("WUPHF_SLACK_AGENT_RESEARCHER_TOKEN", "bot-token-test-555")
	if _, _, err := b.CompleteSlackAgentSpawn(context.Background(), "researcher"); err != nil {
		t.Fatalf("CompleteSlackAgentSpawn: %v", err)
	}

	// A spawned identity cannot be re-registered as FOREIGN — that would
	// flip its runtime to the gateway kind and break the echo guard.
	if _, err := b.RegisterSlackAgent("researcher", "Ress", "U555"); err == nil {
		t.Fatal("registering a spawned slug+user id as foreign must error")
	}
	if _, err := b.RegisterSlackAgent("other-bot", "Other", "U555"); err == nil {
		t.Fatal("registering a spawned user id under another slug must error")
	}
	if got := b.MemberProviderKind("researcher"); got == provider.KindSlack {
		t.Fatal("spawned member's provider kind must not flip to slack")
	}
}
