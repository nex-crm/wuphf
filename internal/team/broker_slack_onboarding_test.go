package team

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func onboardingBroker(t *testing.T) (*Broker, string) {
	t.Helper()
	t.Setenv("WUPHF_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	// Clear any env tokens so config is the source of truth in-test.
	t.Setenv("SLACK_BOT_TOKEN", "")
	t.Setenv("SLACK_APP_TOKEN", "")
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(b.Stop)
	return b, fmt.Sprintf("http://%s", b.Addr())
}

func onboardingGET(t *testing.T, b *Broker, base, path string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+path, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s status %d: %s", path, resp.StatusCode, raw)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func onboardingPOST(t *testing.T, b *Broker, base, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestSlackAppManifestEndpoint(t *testing.T) {
	b, base := onboardingBroker(t)
	out := onboardingGET(t, b, base, "/slack/app-manifest")

	mj, _ := out["manifest_json"].(string)
	for _, want := range []string{
		"socket_mode_enabled", "app_home", "message.groups", "app_mention",
		"chat:write", "users:read", "pins:write", "is_enabled",
		// Agents & AI Apps surface: enables the native "is thinking…" status +
		// the Assistant pane (a DM surface) and its lifecycle/DM events.
		"assistant_view", "assistant_description", "assistant:write",
		"assistant_thread_started", "assistant_thread_context_changed",
		"message.im", "im:history", "im:read", "im:write",
		// Quality-feedback signal on office replies.
		"reaction_added", "reactions:read",
	} {
		if !strings.Contains(mj, want) {
			t.Errorf("office manifest missing %q:\n%s", want, mj)
		}
	}
	if _, ok := out["guide"].([]any); !ok {
		t.Fatalf("manifest response missing guide steps")
	}
	if url, _ := out["create_url"].(string); !strings.Contains(url, "api.slack.com/apps") {
		t.Fatalf("missing create_url: %q", url)
	}
}

func TestSlackTokensValidatesAndPersists(t *testing.T) {
	b, base := onboardingBroker(t)
	b.slackOnboardingAuthTest = func(_ context.Context, token string) (string, string, string, error) {
		if token != "xoxb-real-123" {
			return "", "", "", fmt.Errorf("invalid_auth")
		}
		return "U0BOT", "wuphf", "Acme", nil
	}

	// Bad prefixes are rejected with a friendly message, before any network.
	if code, out := onboardingPOST(t, b, base, "/slack/tokens", map[string]string{"bot_token": "nope", "app_token": "xapp-1"}); code != http.StatusBadRequest || !strings.Contains(fmt.Sprint(out["error"]), "xoxb-") {
		t.Fatalf("bad bot token not rejected: code=%d out=%v", code, out)
	}
	if code, _ := onboardingPOST(t, b, base, "/slack/tokens", map[string]string{"bot_token": "xoxb-real-123", "app_token": "nope"}); code != http.StatusBadRequest {
		t.Fatalf("bad app token not rejected: code=%d", code)
	}

	// Valid tokens: auth.test runs, identity returned, config persisted.
	code, out := onboardingPOST(t, b, base, "/slack/tokens", map[string]string{"bot_token": "xoxb-real-123", "app_token": "xapp-abc"})
	if code != http.StatusOK {
		t.Fatalf("valid tokens rejected: code=%d out=%v", code, out)
	}
	if out["workspace"] != "Acme" || out["bot_name"] != "wuphf" {
		t.Fatalf("identity not returned: %v", out)
	}

	// Status now reflects tokens set (channel still missing → not ready).
	st := onboardingGET(t, b, base, "/slack/status")
	if st["bot_token_set"] != true || st["app_token_set"] != true {
		t.Fatalf("status should show tokens set: %v", st)
	}
	if st["channel_connected"] != false || st["ready"] != false {
		t.Fatalf("status should not be ready without a channel: %v", st)
	}
	// The hot-start health signal: no transport is running in this unit test, so
	// the Socket Mode link reports disconnected and gates "ready".
	if st["transport_connected"] != false {
		t.Fatalf("status transport_connected should be false with no running transport: %v", st)
	}
}

func TestSlackTokensSurfacesSlackRejection(t *testing.T) {
	b, base := onboardingBroker(t)
	b.slackOnboardingAuthTest = func(_ context.Context, _ string) (string, string, string, error) {
		return "", "", "", fmt.Errorf("invalid_auth")
	}
	code, out := onboardingPOST(t, b, base, "/slack/tokens", map[string]string{"bot_token": "xoxb-bad", "app_token": "xapp-abc"})
	if code != http.StatusBadGateway {
		t.Fatalf("expected 502 on Slack rejection, got %d (%v)", code, out)
	}
	if !strings.Contains(fmt.Sprint(out["error"]), "invalid_auth") {
		t.Fatalf("error should surface Slack's reason: %v", out)
	}
}
