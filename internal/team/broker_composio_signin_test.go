package team

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
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
	// Sandbox the CLI install dir too: composioBinary falls back to
	// $COMPOSIO_INSTALL_DIR (else ~/.composio) when composio isn't on PATH, so
	// a real CLI installed on the dev box (the common case) would otherwise leak
	// into these tests. Empty → defaults under the isolated temp HOME.
	t.Setenv("COMPOSIO_INSTALL_DIR", "")
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
	var last composioSigninState
	testTickUntil(t, 10*time.Second, func() bool {
		_, state, _ := composioSigninRequest(t, b, http.MethodGet, "/integrations/composio/signin/status")
		last = state
		return state.Status == want || state.Status == composioSigninStatusError
	})
	if last.Status == want || last.Status == composioSigninStatusError {
		return last
	}
	t.Fatalf("timed out waiting for status %q, last %+v", want, last)
	return last
}

// TestComposioSigninStart_AutoInstallFailsFallsBackToCLIMissing: with no
// composio binary on PATH, start kicks off the auto-install and returns
// `installing`. When the install fails, the flow falls back to cli_missing
// carrying the manual install command (the UI renders it with a copy button).
func TestComposioSigninStart_AutoInstallFailsFallsBackToCLIMissing(t *testing.T) {
	dir := t.TempDir() // empty PATH dir — no composio
	b := newComposioSigninBroker(t, dir)

	orig := composioInstaller
	composioInstaller = func(_ context.Context) error {
		return errors.New("install blocked in test")
	}
	t.Cleanup(func() { composioInstaller = orig })

	code, state, raw := composioSigninRequest(t, b, http.MethodPost, "/integrations/composio/signin/start")
	if code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", code, raw)
	}
	if state.Status != composioSigninStatusInstalling {
		t.Fatalf("expected installing on start, got %+v", state)
	}

	missing := pollComposioSigninUntil(t, b, composioSigninStatusCLIMissing)
	if missing.Status != composioSigninStatusCLIMissing {
		t.Fatalf("expected cli_missing after failed install, got %+v", missing)
	}
	if missing.InstallCommand != composioInstallCommand {
		t.Fatalf("expected install command %q, got %q", composioInstallCommand, missing.InstallCommand)
	}
}

// TestComposioSigninStart_InstallDeadlineExcludesLoginWindow: the "installing"
// backstop must be bounded by the install budget (+ a small grace), NOT
// stretched by the login window — otherwise a wedged install lingers in
// "installing" for the whole install+login span before timing out.
func TestComposioSigninStart_InstallDeadlineExcludesLoginWindow(t *testing.T) {
	dir := t.TempDir() // empty PATH dir — no composio
	b := newComposioSigninBroker(t, dir)

	// Block the installer so the flow stays in "installing" while we inspect
	// the deadline the start handler set. `entered` lets cleanup confirm the
	// background goroutine actually read the stub before restoring the global —
	// otherwise an unlucky schedule could let the goroutine run the REAL
	// installer after restore.
	entered := make(chan struct{})
	block := make(chan struct{})
	orig := composioInstaller
	composioInstaller = func(_ context.Context) error {
		close(entered)
		<-block
		return errors.New("unblocked in cleanup")
	}
	t.Cleanup(func() {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
		}
		close(block)
		composioInstaller = orig
	})

	start := time.Now()
	code, state, raw := composioSigninRequest(t, b, http.MethodPost, "/integrations/composio/signin/start")
	if code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", code, raw)
	}
	if state.Status != composioSigninStatusInstalling {
		t.Fatalf("expected installing on start, got %+v", state)
	}

	b.composioSignin.mu.Lock()
	deadline := b.composioSignin.deadline
	b.composioSignin.mu.Unlock()

	maxAllowed := start.Add(composioInstallTimeout + composioInstallDeadlineGrace + time.Second)
	if deadline.After(maxAllowed) {
		t.Fatalf("install deadline %v exceeds install budget+grace (max %v) — login window leaked into the install backstop", deadline, maxAllowed)
	}
	if !deadline.After(start.Add(composioInstallTimeout)) {
		t.Fatalf("install deadline %v should sit past the install timeout from start", deadline)
	}
}

// TestComposioSigninStart_AutoInstallSucceedsThenSignsIn: the CLI is absent, so
// start auto-installs (the stub drops a fake composio + a logged-in session),
// then the flow continues straight through provisioning to done — one user
// gesture, no manual install step.
func TestComposioSigninStart_AutoInstallSucceedsThenSignsIn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script requires a POSIX shell")
	}
	dir := t.TempDir() // empty PATH dir initially
	b := newComposioSigninBroker(t, dir)
	home := os.Getenv("HOME")

	orig := composioInstaller
	composioInstaller = func(_ context.Context) error {
		// Drop a fake composio onto PATH (handles `dev init`) and a logged-in
		// session, mirroring what the real installer + a prior login leave.
		body := "case \"$1 $2\" in\n  \"dev init\")\n    printf 'COMPOSIO_API_KEY=\"" +
			testComposioProjectKey + "\"\\n' > .env.local\n    ;;\n  *) exit 1 ;;\nesac"
		script := "#!/bin/sh\nPATH=\"/bin:/usr/bin:$PATH\"\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(dir, "composio"), []byte(script), 0o755); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(home, ".composio"), 0o700); err != nil {
			return err
		}
		return os.WriteFile(
			filepath.Join(home, ".composio", "user_data.json"),
			[]byte(`{"api_key":"uak_cli_only_session_key"}`),
			0o600,
		)
	}
	t.Cleanup(func() { composioInstaller = orig })

	code, state, raw := composioSigninRequest(t, b, http.MethodPost, "/integrations/composio/signin/start")
	if code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", code, raw)
	}
	if state.Status != composioSigninStatusInstalling {
		t.Fatalf("expected installing on start, got %+v", state)
	}

	done := pollComposioSigninUntil(t, b, composioSigninStatusDone)
	if done.Status != composioSigninStatusDone {
		t.Fatalf("expected done after auto-install, got %+v", done)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if cfg.ComposioAPIKey != testComposioProjectKey {
		t.Fatalf("expected stored composio key %q, got %q", testComposioProjectKey, cfg.ComposioAPIKey)
	}
}

// TestComposioSigninStart_AutoInstallNotOnPATHResolvesFromInstallDir is the
// regression for the reported bug: the official installer drops `composio` into
// ~/.composio and only appends that dir to PATH in the user's shell profile,
// which the already-running broker never re-reads. So a CLI installed *after*
// the broker started is invisible to exec.LookPath, and the flow wrongly
// reported cli_missing right after a successful install ("it attempts and then
// says: The Composio CLI isn't installed").
//
// Here the install stub places the binary ONLY in ~/.composio (NOT on PATH).
// The flow must still resolve it (via composioBinary's install-dir fallback)
// and run to done — never cli_missing.
func TestComposioSigninStart_AutoInstallNotOnPATHResolvesFromInstallDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script requires a POSIX shell")
	}
	pathDir := t.TempDir() // PATH stays empty of composio for the whole flow
	b := newComposioSigninBroker(t, pathDir)
	home := os.Getenv("HOME")
	// Default the install dir to ~/.composio (the installer's default), making
	// the test independent of any COMPOSIO_INSTALL_DIR in the dev environment.
	t.Setenv("COMPOSIO_INSTALL_DIR", "")
	installDir := filepath.Join(home, ".composio")

	orig := composioInstaller
	composioInstaller = func(_ context.Context) error {
		// Mirror the real installer: binary lands in ~/.composio (NOT on PATH),
		// plus a logged-in session so the flow skips the browser hop.
		body := "case \"$1 $2\" in\n  \"dev init\")\n    printf 'COMPOSIO_API_KEY=\"" +
			testComposioProjectKey + "\"\\n' > .env.local\n    ;;\n  *) exit 1 ;;\nesac"
		script := "#!/bin/sh\nPATH=\"/bin:/usr/bin:$PATH\"\n" + body + "\n"
		if err := os.MkdirAll(installDir, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(installDir, "composio"), []byte(script), 0o755); err != nil {
			return err
		}
		return os.WriteFile(
			filepath.Join(installDir, "user_data.json"),
			[]byte(`{"api_key":"uak_cli_only_session_key"}`),
			0o600,
		)
	}
	t.Cleanup(func() { composioInstaller = orig })

	code, state, raw := composioSigninRequest(t, b, http.MethodPost, "/integrations/composio/signin/start")
	if code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", code, raw)
	}
	if state.Status != composioSigninStatusInstalling {
		t.Fatalf("expected installing on start, got %+v", state)
	}

	done := pollComposioSigninUntil(t, b, composioSigninStatusDone)
	if done.Status != composioSigninStatusDone {
		t.Fatalf("expected done (CLI resolved from install dir, not PATH), got %+v", done)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if cfg.ComposioAPIKey != testComposioProjectKey {
		t.Fatalf("expected stored composio key %q, got %q", testComposioProjectKey, cfg.ComposioAPIKey)
	}
}

// TestComposioBinary_ResolvesFromInstallDir exercises the resolver directly:
// with no composio on PATH, it falls back to $COMPOSIO_INSTALL_DIR/composio
// (and to ~/.composio/composio when the override is unset).
func TestComposioBinary_ResolvesFromInstallDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit semantics differ on Windows")
	}
	emptyPath := t.TempDir()
	t.Setenv("PATH", emptyPath) // nothing named composio on PATH
	installDir := t.TempDir()
	t.Setenv("COMPOSIO_INSTALL_DIR", installDir) // empty so far — pinned for hermeticity
	if _, ok := composioBinary(); ok {
		t.Fatal("composioBinary resolved a binary with an empty PATH and an empty install dir")
	}

	bin := filepath.Join(installDir, "composio")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake composio: %v", err)
	}
	got, ok := composioBinary()
	if !ok || got != bin {
		t.Fatalf("composioBinary() = (%q, %v), want (%q, true)", got, ok, bin)
	}

	// A non-executable file at the install path must NOT be treated as the CLI.
	if err := os.Chmod(bin, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	if _, ok := composioBinary(); ok {
		t.Fatal("composioBinary resolved a non-executable file as the CLI")
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

// TestComposioSigninProvision_RejectsUserScopedKey: a uak_ key from dev init's
// .env.local must NOT be stored as a project ak_ key (it 401s against the SDK
// as x-api-key). Here user_data.json carries no org id, so the user-key
// fallback can't complete either — the flow errors and stores nothing.
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
	// No org context (markComposioLoggedIn writes only api_key): the fallback
	// can't proceed and the flow points the user at the manual paste path.
	if !strings.Contains(errState.Reason, "dashboard.composio.dev") {
		t.Fatalf("expected the reason to offer the manual paste fallback, got %q", errState.Reason)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if cfg.ComposioAPIKey != "" {
		t.Fatalf("uak_ key must not be stored as a project key, got %q", cfg.ComposioAPIKey)
	}
	if cfg.ComposioUserAPIKey != "" {
		t.Fatalf("user key must not be stored without an org id, got %q", cfg.ComposioUserAPIKey)
	}
}

// TestComposioSigninProvision_UserKeyFallback is the regression for the current
// composio CLI: `dev init` no longer writes an ak_ key to .env.local, so the
// flow falls back to the user-key trio — the uak_ + org id from user_data.json,
// scoped to the project the resolve endpoint returns — and stores it so the
// SDK can authenticate with x-user-api-key/x-org-id/x-project-id.
func TestComposioSigninProvision_UserKeyFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script requires a POSIX shell")
	}
	dir := t.TempDir()
	// dev init exits cleanly but writes NO .env.local (current CLI behavior),
	// so the ak_ path fails and the user-key fallback takes over.
	writeFakeComposioCLI(t, dir, `
case "$1 $2" in
  "dev init") exit 0 ;;
  *) exit 1 ;;
esac`)

	b := newComposioSigninBroker(t, dir)
	// Logged-in session WITH an org id (what `composio login` writes).
	home := os.Getenv("HOME")
	if err := os.MkdirAll(filepath.Join(home, ".composio"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(home, ".composio", "user_data.json"),
		[]byte(`{"api_key":"uak_session_xyz","org_id":"ok_test123","base_url":"https://backend.composio.dev"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	// Stub the project-resolve network call.
	origResolve := composioResolveProjectID
	composioResolveProjectID = func(baseURL, userAPIKey, orgID string) string {
		if userAPIKey != "uak_session_xyz" || orgID != "ok_test123" {
			t.Errorf("resolve got unexpected creds: key=%q org=%q", userAPIKey, orgID)
		}
		return "pr_resolved789"
	}
	t.Cleanup(func() { composioResolveProjectID = origResolve })

	_, state, _ := composioSigninRequest(t, b, http.MethodPost, "/integrations/composio/signin/start")
	if state.Status != composioSigninStatusProvisioning {
		t.Fatalf("expected provisioning on the logged-in fast path, got %+v", state)
	}
	done := pollComposioSigninUntil(t, b, composioSigninStatusDone)
	if done.Status != composioSigninStatusDone {
		t.Fatalf("expected done via user-key fallback, got %+v", done)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if cfg.ComposioAPIKey != "" {
		t.Fatalf("no project ak_ key should be stored, got %q", cfg.ComposioAPIKey)
	}
	if cfg.ComposioUserAPIKey != "uak_session_xyz" {
		t.Fatalf("expected stored user key, got %q", cfg.ComposioUserAPIKey)
	}
	if cfg.ComposioOrgID != "ok_test123" {
		t.Fatalf("expected stored org id, got %q", cfg.ComposioOrgID)
	}
	if cfg.ComposioProjectID != "pr_resolved789" {
		t.Fatalf("expected stored project id, got %q", cfg.ComposioProjectID)
	}
	if !config.IsComposioConfigured() {
		t.Fatal("IsComposioConfigured should be true after a user-key sign-in")
	}
}

// TestDefaultComposioResolveProjectID verifies the resolve call sends the user
// key + org and returns the pr_-prefixed nano id (the form x-project-id
// accepts), NEVER the bare project_id UUID (which the API 401s on).
func TestDefaultComposioResolveProjectID(t *testing.T) {
	var gotKey, gotOrg, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-user-api-key")
		gotOrg = r.Header.Get("x-org-id")
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"project_id":"e3485bdf-5441-4c03-9d28-61bc1c7a2df4","project_nano_id":"pr_nano123"}`))
	}))
	defer srv.Close()

	got := defaultComposioResolveProjectID(srv.URL, "uak_test", "ok_test")
	if got != "pr_nano123" {
		t.Fatalf("expected the pr_ nano id, got %q", got)
	}
	if gotKey != "uak_test" || gotOrg != "ok_test" {
		t.Fatalf("resolve sent wrong auth headers: key=%q org=%q", gotKey, gotOrg)
	}
	if gotPath != "/api/v3/org/consumer/project/resolve" {
		t.Fatalf("resolve hit wrong path: %q", gotPath)
	}
}

// TestDefaultComposioResolveProjectID_RejectsUUIDOnly: when only the UUID
// project_id is returned (no nano id), resolve yields "" rather than a value
// the SDK would 401 on. The SDK then runs against the org's default project.
func TestDefaultComposioResolveProjectID_RejectsUUIDOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"project_id":"e3485bdf-5441-4c03-9d28-61bc1c7a2df4"}`))
	}))
	defer srv.Close()
	if got := defaultComposioResolveProjectID(srv.URL, "uak_test", "ok_test"); got != "" {
		t.Fatalf("expected empty (UUID is not a valid x-project-id), got %q", got)
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
