package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// These tests cover the "Sign in with Composio" broker flow
// (broker_composio_signin.go) against a fake `composio` CLI on PATH — the
// same shell-script-fixture precedent as writeFakeNexCLIForBroker. The fake
// writes a canned user_data.json and .env.local, so the full state machine
// (cli_missing / awaiting_login / provisioning / done / error) runs without
// the real CLI or network.

const testComposioProjectKey = "ak_test_project_key_123456"

// writeFakeComposioCLI drops a `composio` shell script into dir that handles
// the three subcommands the broker invokes. Behavior is driven by the body.
func writeFakeComposioCLI(t *testing.T, dir, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script requires a POSIX shell")
	}
	path := filepath.Join(dir, "composio")
	// The test PATH is stripped down to the fixture dir, so the script
	// restores the system bins it needs (sleep, mkdir) itself.
	script := "#!/bin/sh\nPATH=\"/bin:/usr/bin:$PATH\"\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake composio: %v", err)
	}
}

// newComposioSigninBroker boots a broker with an isolated HOME (so
// user_data.json + ~/.wuphf/config.json are test-scoped) and the given PATH.
func newComposioSigninBroker(t *testing.T, pathDir string) *Broker {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("COMPOSIO_CACHE_DIR", "")
	t.Setenv("WUPHF_CONFIG_PATH", filepath.Join(home, ".wuphf", "config.json"))
	if err := os.MkdirAll(filepath.Join(home, ".wuphf"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)

	b := newTestBroker(t)
	b.token = "test-token"
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(func() {
		if b.server != nil {
			_ = b.server.Shutdown(context.Background())
		}
	})
	return b
}

func markComposioLoggedIn(t *testing.T) {
	t.Helper()
	home := os.Getenv("HOME")
	if err := os.MkdirAll(filepath.Join(home, ".composio"), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"api_key":"uak_cli_only_session_key"}`)
	if err := os.WriteFile(filepath.Join(home, ".composio", "user_data.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

func composioSigninRequest(t *testing.T, b *Broker, method, path string) (int, composioSigninState, string) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://"+b.addr+path, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var state composioSigninState
	_ = json.Unmarshal(raw, &state)
	return resp.StatusCode, state, string(raw)
}

func pollComposioSigninUntil(t *testing.T, b *Broker, want string) composioSigninState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last composioSigninState
	for time.Now().Before(deadline) {
		_, state, _ := composioSigninRequest(t, b, http.MethodGet, "/integrations/composio/signin/status")
		last = state
		if state.Status == want || state.Status == composioSigninStatusError {
			return state
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status %q, last %+v", want, last)
	return last
}

// TestComposioSigninStart_CLIMissing: with no composio binary on PATH, start
// must return the structured cli_missing state carrying the install command —
// the UI renders it with a copy button.
func TestComposioSigninStart_CLIMissing(t *testing.T) {
	b := newComposioSigninBroker(t, t.TempDir()) // empty PATH dir

	code, state, raw := composioSigninRequest(t, b, http.MethodPost, "/integrations/composio/signin/start")
	if code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", code, raw)
	}
	if state.Status != composioSigninStatusCLIMissing {
		t.Fatalf("expected cli_missing, got %+v", state)
	}
	if state.InstallCommand != composioInstallCommand {
		t.Fatalf("expected install command %q, got %q", composioInstallCommand, state.InstallCommand)
	}
}

// TestComposioSigninStart_AlreadyLoggedInFastPath: user_data.json carries a
// session, so start skips the browser hop, provisions via `composio dev init`,
// stores the parsed ak_ key into broker config exactly like the manual paste
// path — and the key never appears in broker logs.
func TestComposioSigninStart_AlreadyLoggedInFastPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script requires a POSIX shell")
	}
	dir := t.TempDir()
	writeFakeComposioCLI(t, dir, `
case "$1 $2" in
  "dev init")
    printf 'COMPOSIO_API_KEY="`+testComposioProjectKey+`"\n' > .env.local
    echo "initialized project"
    ;;
  *) exit 1 ;;
esac`)

	var logBuf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prev) })

	b := newComposioSigninBroker(t, dir)
	markComposioLoggedIn(t)

	code, state, raw := composioSigninRequest(t, b, http.MethodPost, "/integrations/composio/signin/start")
	if code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", code, raw)
	}
	if state.Status != composioSigninStatusProvisioning {
		t.Fatalf("expected provisioning on the logged-in fast path, got %+v", state)
	}

	done := pollComposioSigninUntil(t, b, composioSigninStatusDone)
	if done.Status != composioSigninStatusDone {
		t.Fatalf("expected done, got %+v", done)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if cfg.ComposioAPIKey != testComposioProjectKey {
		t.Fatalf("expected stored composio key %q, got %q", testComposioProjectKey, cfg.ComposioAPIKey)
	}
	if strings.Contains(logBuf.String(), testComposioProjectKey) {
		t.Fatalf("the project key leaked into broker logs")
	}
	// The audit trail records the sign-in without the key.
	b.mu.Lock()
	acts := make([]officeActionLog, len(b.actions))
	copy(acts, b.actions)
	b.mu.Unlock()
	for _, act := range acts {
		if strings.Contains(act.Summary, testComposioProjectKey) {
			t.Fatalf("the project key leaked into the action audit: %q", act.Summary)
		}
	}
}

// TestComposioSigninProvision_RejectsUserScopedKey: a uak_ key from dev init
// must be rejected (it 401s against the SDK) and must NOT be stored.
func TestComposioSigninProvision_RejectsUserScopedKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script requires a POSIX shell")
	}
	dir := t.TempDir()
	writeFakeComposioCLI(t, dir, `
case "$1 $2" in
  "dev init")
    printf 'COMPOSIO_API_KEY=uak_user_scoped_key_456789\n' > .env.local
    ;;
  *) exit 1 ;;
esac`)

	b := newComposioSigninBroker(t, dir)
	markComposioLoggedIn(t)

	_, state, _ := composioSigninRequest(t, b, http.MethodPost, "/integrations/composio/signin/start")
	if state.Status != composioSigninStatusProvisioning {
		t.Fatalf("expected provisioning, got %+v", state)
	}
	errState := pollComposioSigninUntil(t, b, composioSigninStatusError)
	if errState.Status != composioSigninStatusError {
		t.Fatalf("expected error for uak_ key, got %+v", errState)
	}
	if !strings.Contains(errState.Reason, "uak_") {
		t.Fatalf("expected the reason to explain the uak_ rejection, got %q", errState.Reason)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if cfg.ComposioAPIKey != "" {
		t.Fatalf("uak_ key must not be stored, got %q", cfg.ComposioAPIKey)
	}
}

// TestComposioSigninStart_AwaitingLoginAndSingleFlight: when not logged in,
// start runs `composio login --no-wait` once, surfaces the parsed auth URL,
// and a second start re-joins the pending flow instead of minting a second
// login session. Once the background `login --poll` lands user_data.json, the
// flow advances to provisioning → done.
func TestComposioSigninStart_AwaitingLoginAndSingleFlight(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script requires a POSIX shell")
	}
	dir := t.TempDir()
	counter := filepath.Join(dir, "no-wait-count")
	pollGate := filepath.Join(dir, "poll-gate")
	// login --no-wait prints the session URL (and bumps a counter file);
	// login --poll blocks until the gate file appears, then writes
	// user_data.json; dev init writes the project key.
	writeFakeComposioCLI(t, dir, `
case "$1 $2" in
  "login --no-wait")
    echo started >> "`+counter+`"
    echo 'Open this URL in your browser to log in:'
    echo '  https://platform.composio.dev/?cliKey=sess_abc123'
    ;;
  "login --poll")
    i=0
    while [ ! -f "`+pollGate+`" ] && [ $i -lt 200 ]; do sleep 0.05; i=$((i+1)); done
    mkdir -p "$HOME/.composio"
    printf '{"api_key":"uak_cli_only_session_key"}' > "$HOME/.composio/user_data.json"
    ;;
  "dev init")
    printf 'COMPOSIO_API_KEY=`+testComposioProjectKey+`\n' > .env.local
    ;;
  *) exit 1 ;;
esac`)

	b := newComposioSigninBroker(t, dir)

	_, first, _ := composioSigninRequest(t, b, http.MethodPost, "/integrations/composio/signin/start")
	if first.Status != composioSigninStatusAwaitingLogin {
		t.Fatalf("expected awaiting_login, got %+v", first)
	}
	if first.AuthURL != "https://platform.composio.dev/?cliKey=sess_abc123" {
		t.Fatalf("expected the parsed cliKey URL, got %q", first.AuthURL)
	}

	// Second start while pending: same state, and the CLI must not have been
	// asked for a second login session.
	_, second, _ := composioSigninRequest(t, b, http.MethodPost, "/integrations/composio/signin/start")
	if second.Status != composioSigninStatusAwaitingLogin || second.AuthURL != first.AuthURL {
		t.Fatalf("expected single-flight re-join with the same auth_url, got %+v", second)
	}
	if data, err := os.ReadFile(counter); err != nil || strings.Count(string(data), "started") != 1 {
		t.Fatalf("expected exactly one `login --no-wait` invocation, got %q (err=%v)", string(data), err)
	}

	// Status while the browser step is pending stays awaiting_login.
	_, pending, _ := composioSigninRequest(t, b, http.MethodGet, "/integrations/composio/signin/status")
	if pending.Status != composioSigninStatusAwaitingLogin {
		t.Fatalf("expected awaiting_login from status, got %+v", pending)
	}

	// Release the gate: --poll writes user_data.json and the flow finishes.
	if err := os.WriteFile(pollGate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := pollComposioSigninUntil(t, b, composioSigninStatusDone)
	if done.Status != composioSigninStatusDone {
		t.Fatalf("expected done after login lands, got %+v", done)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if cfg.ComposioAPIKey != testComposioProjectKey {
		t.Fatalf("expected stored composio key after full flow, got %q", cfg.ComposioAPIKey)
	}
}

// TestComposioSigninEndpoints_MethodGuards: start is POST-only, status is
// GET-only — wrong verbs fail fast with 405.
func TestComposioSigninEndpoints_MethodGuards(t *testing.T) {
	b := newComposioSigninBroker(t, t.TempDir())

	if code, _, _ := composioSigninRequest(t, b, http.MethodGet, "/integrations/composio/signin/start"); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET start: expected 405, got %d", code)
	}
	if code, _, _ := composioSigninRequest(t, b, http.MethodPost, "/integrations/composio/signin/status"); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status: expected 405, got %d", code)
	}
	// Status before any start reports idle.
	if _, state, _ := composioSigninRequest(t, b, http.MethodGet, "/integrations/composio/signin/status"); state.Status != composioSigninStatusIdle {
		t.Fatalf("expected idle before start, got %+v", state)
	}
}

// TestParseComposioLoginURL pins the URL extraction against the CLI's
// non-interactive output shape (and the degraded no-URL case).
func TestParseComposioLoginURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"cliKey URL preferred",
			"Open this URL in your browser to log in:\n\n  https://platform.composio.dev/?cliKey=sess_1\n\nthen run:\n\n  composio login --poll\n",
			"https://platform.composio.dev/?cliKey=sess_1",
		},
		{
			"falls back to first https URL",
			"see https://docs.composio.dev for help",
			"https://docs.composio.dev",
		},
		{"no URL → degraded empty", "login pending", ""},
		{
			"trailing punctuation trimmed",
			"visit https://platform.composio.dev/?cliKey=sess_2.",
			"https://platform.composio.dev/?cliKey=sess_2",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseComposioLoginURL(tc.in); got != tc.want {
				t.Fatalf("parseComposioLoginURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseEnvFileValue pins the dotenv parsing against the shapes the
// composio CLI writes (quoted, unquoted, comments, unrelated keys).
func TestParseEnvFileValue(t *testing.T) {
	content := "# generated\nOTHER=1\nCOMPOSIO_API_KEY=\"ak_quoted_key_0123456789\"\n"
	if got := parseEnvFileValue(content, "COMPOSIO_API_KEY"); got != "ak_quoted_key_0123456789" {
		t.Fatalf("quoted parse = %q", got)
	}
	if got := parseEnvFileValue("COMPOSIO_API_KEY=ak_plain_key_0123456789", "COMPOSIO_API_KEY"); got != "ak_plain_key_0123456789" {
		t.Fatalf("plain parse = %q", got)
	}
	if got := parseEnvFileValue("# COMPOSIO_API_KEY=ak_commented", "COMPOSIO_API_KEY"); got != "" {
		t.Fatalf("comment must not parse, got %q", got)
	}
}
