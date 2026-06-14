package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// "Sign in with Composio" — a broker-driven CLI flow that replaces the manual
// API-key copy/paste on the Integrations first-run page. The broker shells out
// to the official `composio` CLI:
//
//	composio login --no-wait          → prints a login URL (…?cliKey=<id>) and exits
//	composio login --poll             → completes the pending login; writes
//	                                    ~/.composio/user_data.json (api_key = uak_…)
//	composio dev init -y --no-browser → writes COMPOSIO_API_KEY=ak_… into
//	                                    <cwd>/.env.local for the default project
//
// State machine (in-memory, single-flight): idle → awaiting_login →
// provisioning → done | error. cli_missing is a terminal start response that a
// later start retries from scratch.
//
// Only project-scoped `ak_` keys are accepted: the user-scoped `uak_` key the
// CLI stores in user_data.json is CLI-only and 401s against the SDK, so it is
// explicitly rejected rather than stored.
//
// Degraded-honest fallback: if the login URL cannot be parsed from the CLI
// output, the flow still moves to awaiting_login with an empty auth_url — the
// UI then tells the user to run `composio login` in a terminal, and the status
// poll picks up user_data.json appearing.

const (
	composioSigninStatusIdle          = "idle"
	composioSigninStatusCLIMissing    = "cli_missing"
	composioSigninStatusInstalling    = "installing"
	composioSigninStatusAwaitingLogin = "awaiting_login"
	composioSigninStatusProvisioning  = "provisioning"
	composioSigninStatusDone          = "done"
	composioSigninStatusError         = "error"
)

// composioInstallCommand is the install command surfaced verbatim by the UI as
// a copy-able fallback, and (on Unix) run by the auto-install path. It is
// OS-specific — defined in broker_composio_signin_unix.go (the official
// `curl | bash` installer) and broker_composio_signin_windows.go (npm, since
// the `curl | bash` script has no Windows equivalent) — so Windows users are
// never shown a Unix-only command they can't run.

// composioInstaller runs the official installer. It's a package var so tests
// substitute a fake install instead of shelling out, and so the real
// implementation can be platform-specific: the installer is a `curl | bash`
// pipeline that only exists on Unix, so the default lives in
// broker_composio_signin_unix.go; Windows gets a stub that reports
// not-supported (broker_composio_signin_windows.go), which cleanly falls back
// to the manual install command. It runs ONLY after the human explicitly chose
// "Sign in with Composio".
var composioInstaller = defaultComposioInstaller

// composioInstallDir is the directory the official installer drops the
// `composio` binary into: $COMPOSIO_INSTALL_DIR, else ~/.composio — the same
// default install.sh uses (COMPOSIO_INSTALL_DIR:-$HOME/.composio).
func composioInstallDir() string {
	if dir := strings.TrimSpace(os.Getenv("COMPOSIO_INSTALL_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".composio")
}

// composioBinary resolves the `composio` CLI path. It checks PATH first, then
// falls back to the installer's known location. That fallback is the whole
// point of the auto-install fix: the official installer appends its dir to PATH
// only in the user's shell profile (~/.zshrc, ~/.bashrc, …), which the
// already-running broker process never re-reads — so a CLI installed *after*
// the broker started is invisible to exec.LookPath, and the flow would wrongly
// report cli_missing immediately after a successful install. Returns
// ("", false) when no usable binary is found.
func composioBinary() (string, bool) {
	if path, err := exec.LookPath("composio"); err == nil {
		return path, true
	}
	dir := composioInstallDir()
	if dir == "" {
		return "", false
	}
	name := "composio"
	if runtime.GOOS == "windows" {
		name = "composio.exe"
	}
	candidate := filepath.Join(dir, name)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return candidate, true
	}
	return "", false
}

// composioCommand builds an exec.Cmd for the composio CLI using the resolved
// binary path, prepending its directory to the subprocess PATH so the CLI can
// find any sibling helpers it shells out to (the broker's inherited PATH may
// not include the installer's dir). Returns an error when the CLI cannot be
// located.
func composioCommand(ctx context.Context, args ...string) (*exec.Cmd, error) {
	bin, ok := composioBinary()
	if !ok {
		return nil, errors.New("composio CLI not found on PATH or in the install directory")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = composioCommandEnv(filepath.Dir(bin))
	return cmd, nil
}

// composioCommandEnv returns the process environment with binDir prepended to
// PATH (replacing the existing entry rather than duplicating it), so the
// resolved CLI's own dir is searchable by any child it spawns.
func composioCommandEnv(binDir string) []string {
	env := os.Environ()
	if binDir == "" {
		return env
	}
	out := make([]string, 0, len(env)+1)
	pathVal := binDir
	for _, kv := range env {
		if name, val, ok := strings.Cut(kv, "="); ok && strings.EqualFold(name, "PATH") {
			if val != "" {
				pathVal = binDir + string(os.PathListSeparator) + val
			}
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "PATH="+pathVal)
	return out
}

var (
	// composioProjectKeyPattern accepts only project-scoped SDK keys.
	composioProjectKeyPattern = regexp.MustCompile(`^ak_[A-Za-z0-9_-]{10,}$`)
	// composioLoginURLPattern pulls candidate URLs out of the CLI's
	// `login --no-wait` output ("Open this URL in your browser…").
	composioLoginURLPattern = regexp.MustCompile(`https://[^\s"'` + "`" + `]+`)
)

// Subprocess budgets. Package vars (not consts) so tests covering the slow
// paths can shrink them without sleeping.
var (
	composioLoginStartTimeout = 30 * time.Second
	// composioLoginPollTimeout bounds `composio login --poll`, which itself
	// polls the pending browser session for up to ~10 minutes.
	composioLoginPollTimeout = 12 * time.Minute
	composioDevInitTimeout   = 3 * time.Minute
	// composioResolveTimeout bounds the project-resolve API call in the
	// user-key provisioning fallback.
	composioResolveTimeout = 20 * time.Second
	// composioLoginWindow is how long a started flow stays in awaiting_login
	// before the status endpoint reports a timeout.
	composioLoginWindow = 15 * time.Minute
	// composioInstallTimeout bounds the auto-install of the CLI.
	composioInstallTimeout = 4 * time.Minute
	// composioInstallDeadlineGrace is how long past the install context the
	// status endpoint waits before declaring the install wedged — enough for
	// the auto-install goroutine to flip state after its context ends.
	composioInstallDeadlineGrace = 30 * time.Second
)

// composioSigninState is the wire shape both endpoints return.
type composioSigninState struct {
	Status         string `json:"status"`
	AuthURL        string `json:"auth_url,omitempty"`
	InstallCommand string `json:"install_command,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// composioSigninFlow holds the broker's in-memory sign-in state. Zero value is
// ready to use; guarded by its own mutex so the flow never contends with b.mu.
type composioSigninFlow struct {
	mu       sync.Mutex
	state    composioSigninState
	actor    string
	deadline time.Time
}

// handleComposioSigninStart kicks off (or re-joins) the sign-in flow.
// POST /integrations/composio/signin/start
func (b *Broker) handleComposioSigninStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flow := &b.composioSignin
	flow.mu.Lock()
	// Single-flight: a second start while a flow is pending returns the
	// current state instead of spawning a second CLI pipeline.
	if flow.state.Status == composioSigninStatusInstalling || flow.state.Status == composioSigninStatusAwaitingLogin || flow.state.Status == composioSigninStatusProvisioning {
		state := flow.state
		flow.mu.Unlock()
		writeComposioSigninState(w, state)
		return
	}
	actor := integrationRequestActor(r)
	if _, ok := composioBinary(); !ok {
		// Auto-install on demand: the CLI is needed for one-click sign-in, so
		// rather than dead-ending on cli_missing we run the official installer
		// once in the background and continue the flow. The manual install
		// command is still surfaced as a fallback if the install fails.
		flow.actor = actor
		flow.state = composioSigninState{
			Status:         composioSigninStatusInstalling,
			InstallCommand: composioInstallCommand,
		}
		// Backstop only the INSTALL phase (a small grace past the install
		// context so the auto-install goroutine can transition state first).
		// The login window is a separate, later deadline set by
		// composioSigninBeginLogin once the flow reaches awaiting_login; folding
		// it in here would leave a wedged install showing "installing" for the
		// whole install+login span instead of timing out at the install budget.
		flow.deadline = time.Now().Add(composioInstallTimeout + composioInstallDeadlineGrace)
		state := flow.state
		flow.mu.Unlock()
		b.recordComposioSigninEvent("integration_signin_started", actor, "Installing the Composio CLI, then signing in")
		go b.composioSigninAutoInstall()
		writeComposioSigninState(w, state)
		return
	}
	flow.actor = actor
	if composioCLILoggedIn() {
		// Fast path: user_data.json already carries a CLI session — skip the
		// browser hop and go straight to project-key provisioning.
		flow.state = composioSigninState{Status: composioSigninStatusProvisioning}
		state := flow.state
		flow.mu.Unlock()
		b.recordComposioSigninEvent("integration_signin_started", actor, "Started Composio sign-in (CLI already logged in)")
		go b.composioSigninProvision()
		writeComposioSigninState(w, state)
		return
	}
	// Claim the flow before shelling out so a concurrent start observes
	// awaiting_login and re-joins instead of minting a second login session.
	flow.state = composioSigninState{Status: composioSigninStatusAwaitingLogin}
	flow.deadline = time.Now().Add(composioLoginWindow)
	flow.mu.Unlock()

	b.recordComposioSigninEvent("integration_signin_started", actor, "Started Composio sign-in")

	authURL, err := composioMintLoginURL()
	flow.mu.Lock()
	if flow.state.Status == composioSigninStatusAwaitingLogin {
		if err != nil {
			flow.state = composioSigninState{
				Status: composioSigninStatusError,
				Reason: "composio login could not start: " + err.Error(),
			}
		} else {
			flow.state.AuthURL = authURL
		}
	}
	state := flow.state
	flow.mu.Unlock()
	if err == nil {
		go b.composioSigninAwaitLogin()
	}
	writeComposioSigninState(w, state)
}

// handleComposioSigninStatus reports the current flow state, advancing
// awaiting_login → provisioning when user_data.json shows the login landed
// (covers users who finish `composio login` in their own terminal).
// GET /integrations/composio/signin/status
func (b *Broker) handleComposioSigninStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flow := &b.composioSignin
	flow.mu.Lock()
	state := flow.state
	deadline := flow.deadline
	flow.mu.Unlock()
	if state.Status == "" {
		state.Status = composioSigninStatusIdle
	}
	if state.Status == composioSigninStatusInstalling && !deadline.IsZero() && time.Now().After(deadline) {
		flow.mu.Lock()
		if flow.state.Status == composioSigninStatusInstalling {
			flow.state = composioSigninState{
				Status:         composioSigninStatusCLIMissing,
				InstallCommand: composioInstallCommand,
				Reason:         "the Composio install is taking too long — run the install command shown, then try again",
			}
		}
		state = flow.state
		flow.mu.Unlock()
	}
	if state.Status == composioSigninStatusAwaitingLogin {
		if b.composioSigninAdvanceIfLoggedIn() {
			flow.mu.Lock()
			state = flow.state
			flow.mu.Unlock()
		} else if !deadline.IsZero() && time.Now().After(deadline) {
			flow.mu.Lock()
			if flow.state.Status == composioSigninStatusAwaitingLogin {
				flow.state = composioSigninState{
					Status: composioSigninStatusError,
					Reason: "login timed out — run `composio login` in a terminal, then try again",
				}
			}
			state = flow.state
			flow.mu.Unlock()
		}
	}
	writeComposioSigninState(w, state)
}

// composioSigninAutoInstall runs the official installer once, then continues
// the sign-in if the CLI landed. On failure it falls back to cli_missing with
// the manual install command. The binary's presence (via composioBinary, which
// also checks the installer's ~/.composio location — not just the broker's
// inherited PATH), not the exit code, is the source of truth: installers can
// warn-and-exit nonzero while still placing the binary, and the installer adds
// its dir to PATH only in shell profiles the running broker never re-reads.
func (b *Broker) composioSigninAutoInstall() {
	ctx, cancel := context.WithTimeout(context.Background(), composioInstallTimeout)
	defer cancel()
	runErr := composioInstaller(ctx)
	if _, ok := composioBinary(); !ok {
		flow := &b.composioSignin
		flow.mu.Lock()
		if flow.state.Status == composioSigninStatusInstalling {
			reason := "could not install the Composio CLI automatically — run the install command shown, then try again"
			if runErr == nil {
				reason = "the Composio installer finished but the CLI could not be located — run the install command shown, then try again"
			}
			flow.state = composioSigninState{
				Status:         composioSigninStatusCLIMissing,
				InstallCommand: composioInstallCommand,
				Reason:         reason,
			}
		}
		flow.mu.Unlock()
		return
	}
	b.composioSigninBeginLogin()
}

// composioSigninBeginLogin continues an installing flow once the CLI is
// available: straight to provisioning if a session already exists, otherwise it
// mints the login URL and moves to awaiting_login (the status poll surfaces the
// auth_url and the FE opens it). Mirrors the CLI-present branch of
// handleComposioSigninStart, but runs in the background since that request
// already returned `installing`.
func (b *Broker) composioSigninBeginLogin() {
	flow := &b.composioSignin
	flow.mu.Lock()
	if flow.state.Status != composioSigninStatusInstalling {
		flow.mu.Unlock()
		return
	}
	if composioCLILoggedIn() {
		flow.state = composioSigninState{Status: composioSigninStatusProvisioning}
		flow.mu.Unlock()
		go b.composioSigninProvision()
		return
	}
	flow.state = composioSigninState{Status: composioSigninStatusAwaitingLogin}
	flow.deadline = time.Now().Add(composioLoginWindow)
	flow.mu.Unlock()

	authURL, err := composioMintLoginURL()
	flow.mu.Lock()
	if flow.state.Status == composioSigninStatusAwaitingLogin {
		if err != nil {
			flow.state = composioSigninState{
				Status: composioSigninStatusError,
				Reason: "composio login could not start: " + err.Error(),
			}
		} else {
			flow.state.AuthURL = authURL
		}
	}
	flow.mu.Unlock()
	if err == nil {
		go b.composioSigninAwaitLogin()
	}
}

// composioSigninAwaitLogin blocks on `composio login --poll`, which completes
// the pending browser session and writes user_data.json. The exit code is
// advisory — the file is the source of truth, because the user may finish the
// login through a separate terminal instead.
func (b *Broker) composioSigninAwaitLogin() {
	ctx, cancel := context.WithTimeout(context.Background(), composioLoginPollTimeout)
	defer cancel()
	// --no-skill-install mirrors trustclaw's `composio login`: without it the CLI
	// installs a Claude Code "composio-cli" skill and flips on developer mode,
	// which makes `composio dev init` scaffold .composio/ instead of writing the
	// .env.local + ak_ key that provisioning parses.
	cmd, err := composioCommand(ctx, "login", "--poll", "--no-skill-install")
	if err == nil {
		// Output deliberately discarded: never echo CLI output that may carry
		// credentials into broker logs.
		_ = cmd.Run()
	}
	// Advance regardless: even if the poll could not run, the user may have
	// finished `composio login` in their own terminal (user_data.json), and
	// the status endpoint also calls this — the file is the source of truth.
	b.composioSigninAdvanceIfLoggedIn()
}

// composioSigninAdvanceIfLoggedIn flips awaiting_login → provisioning exactly
// once when the CLI session exists. Safe to call from both the background
// goroutine and the status poll; the status check under the lock arbitrates.
func (b *Broker) composioSigninAdvanceIfLoggedIn() bool {
	if !composioCLILoggedIn() {
		return false
	}
	flow := &b.composioSignin
	flow.mu.Lock()
	if flow.state.Status != composioSigninStatusAwaitingLogin {
		flow.mu.Unlock()
		return false
	}
	flow.state = composioSigninState{Status: composioSigninStatusProvisioning}
	flow.mu.Unlock()
	go b.composioSigninProvision()
	return true
}

// composioProvisionedCreds is the outcome of a successful provision: EITHER a
// project ak_ key (preferred — sent as x-api-key), OR the user-key trio (the
// uak_ session key + org id, and an optional project id — sent as
// x-user-api-key / x-org-id / x-project-id).
type composioProvisionedCreds struct {
	APIKey     string
	UserAPIKey string
	OrgID      string
	ProjectID  string
}

// composioProvisionCreds resolves usable Composio SDK credentials after login.
//
// Preferred path: `composio dev init` writes a project ak_ key to .env.local
// (older CLIs, and the same path trustclaw uses). The current composio CLI no
// longer mints an ak_ key this way, so we fall back to the user-key mode: the
// uak_ session key + org id the CLI already wrote to user_data.json, scoped to
// the org's default project (resolved best-effort; the SDK works without it).
func composioProvisionCreds() (composioProvisionedCreds, error) {
	if key, err := composioProvisionProjectKey(); err == nil {
		return composioProvisionedCreds{APIKey: key}, nil
	}
	ud, err := readComposioUserData()
	if err != nil {
		return composioProvisionedCreds{}, errors.New("could not read the Composio CLI session — sign in again, or paste a project API key from https://dashboard.composio.dev")
	}
	uak := strings.TrimSpace(ud.APIKey)
	org := strings.TrimSpace(ud.OrgID)
	if uak == "" || org == "" {
		return composioProvisionedCreds{}, errors.New("Composio sign-in did not return the org context — sign in again, or paste a project API key from https://dashboard.composio.dev")
	}
	creds := composioProvisionedCreds{UserAPIKey: uak, OrgID: org}
	// Best-effort project scoping; a resolve failure is not fatal (the SDK
	// falls back to the org's default project when no project id is sent).
	creds.ProjectID = composioResolveProjectID(ud.BaseURL, uak, org)
	return creds, nil
}

// composioSigninProvision resolves Composio credentials (project ak_ key or
// user-key trio) and stores them through the same config path as the manual
// paste flow. Credentials never touch logs.
func (b *Broker) composioSigninProvision() {
	flow := &b.composioSignin
	creds, err := composioProvisionCreds()
	var storeFailed bool
	if err == nil {
		if strings.TrimSpace(creds.APIKey) != "" {
			err = b.storeComposioAPIKey(creds.APIKey)
		} else {
			err = b.storeComposioUserKeyCreds(creds.UserAPIKey, creds.OrgID, creds.ProjectID)
		}
		if err != nil {
			storeFailed = true
		}
	}
	flow.mu.Lock()
	actor := flow.actor
	if err != nil {
		reason := err.Error()
		if storeFailed {
			// config.Save errors wrap OS errors carrying the user's home
			// path — keep filesystem details out of the browser-facing
			// Reason (review finding); the full error goes to the log.
			log.Printf("composio signin: %v", err)
			reason = "could not save the Composio credentials to config — check the broker logs"
		}
		flow.state = composioSigninState{Status: composioSigninStatusError, Reason: reason}
		flow.mu.Unlock()
		return
	}
	flow.mu.Unlock()
	// Record the audit entry BEFORE flipping to done: a client that observes
	// done must also observe the audit trail entry.
	b.recordComposioSigninEvent("integration_signin_completed", actor, "Signed in with Composio and stored credentials")
	flow.mu.Lock()
	flow.state = composioSigninState{Status: composioSigninStatusDone}
	flow.mu.Unlock()
}

// composioMintLoginURL runs `composio login --no-wait` and extracts the login
// URL from its output. An empty URL with a nil error is the degraded mode: the
// flow proceeds, the UI tells the user to run `composio login` themselves.
func composioMintLoginURL() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), composioLoginStartTimeout)
	defer cancel()
	// --no-skill-install: don't install the Claude Code skill / enable developer
	// mode as a side effect of signing in (see composioSigninAwaitLogin).
	cmd, err := composioCommand(ctx, "login", "--no-wait", "--no-skill-install")
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Don't echo raw CLI output into the error: it is user-visible.
		return "", fmt.Errorf("composio login --no-wait: %w", err)
	}
	return parseComposioLoginURL(string(out)), nil
}

// parseComposioLoginURL prefers the cliKey-bearing session URL; falls back to
// the first https URL in the output.
func parseComposioLoginURL(output string) string {
	matches := composioLoginURLPattern.FindAllString(output, -1)
	for _, m := range matches {
		if strings.Contains(m, "cliKey=") {
			return strings.TrimRight(m, ".,)")
		}
	}
	if len(matches) > 0 {
		return strings.TrimRight(matches[0], ".,)")
	}
	return ""
}

// composioProvisionProjectKey runs `composio dev init -y --no-browser` in a
// temp dir and parses the project API key it writes to .env.local. The temp
// dir is always removed; the key never touches logs.
func composioProvisionProjectKey() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), composioDevInitTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "wuphf-composio-signin-")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	cmd, err := composioCommand(ctx, "dev", "init", "-y", "--no-browser")
	if err != nil {
		return "", err
	}
	cmd.Dir = dir
	// Output discarded on purpose: dev init may echo project metadata and the
	// key it writes; nothing from it belongs in broker logs or errors.
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("composio dev init: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".env.local"))
	if err != nil {
		return "", errors.New("composio dev init did not write .env.local — paste a project API key from https://dashboard.composio.dev instead")
	}
	key := parseEnvFileValue(string(data), "COMPOSIO_API_KEY")
	if key == "" {
		return "", errors.New("composio dev init produced no COMPOSIO_API_KEY — paste a project API key from https://dashboard.composio.dev instead")
	}
	if strings.HasPrefix(key, "uak_") {
		return "", errors.New("composio returned a user-scoped uak_ key, which does not work with the SDK — paste a project ak_ key from https://dashboard.composio.dev instead")
	}
	if !composioProjectKeyPattern.MatchString(key) {
		return "", errors.New("composio returned an unrecognized API key format — paste a project ak_ key from https://dashboard.composio.dev instead")
	}
	return key, nil
}

// storeComposioAPIKey persists the key exactly like the manual paste path
// (POST /config composio_api_key): load-modify-save under configMu.
func (b *Broker) storeComposioAPIKey(key string) error {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config load failed: %w", err)
	}
	cfg.ComposioAPIKey = strings.TrimSpace(key)
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("config save failed: %w", err)
	}
	return nil
}

// storeComposioUserKeyCreds persists the user-key trio (uak_ session key + org
// + optional project) via the same load-modify-save path as storeComposioAPIKey.
func (b *Broker) storeComposioUserKeyCreds(userAPIKey, orgID, projectID string) error {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config load failed: %w", err)
	}
	cfg.ComposioUserAPIKey = strings.TrimSpace(userAPIKey)
	cfg.ComposioOrgID = strings.TrimSpace(orgID)
	cfg.ComposioProjectID = strings.TrimSpace(projectID)
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("config save failed: %w", err)
	}
	return nil
}

// composioResolveProjectID returns the org's default project id via the CLI's
// resolve endpoint (POST {base}/api/v3/org/consumer/project/resolve with the
// user key + org id). It's a package var so tests can stub the network call.
// Returns "" on any failure — the project id is optional for the SDK.
var composioResolveProjectID = defaultComposioResolveProjectID

func defaultComposioResolveProjectID(baseURL, userAPIKey, orgID string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://backend.composio.dev"
	}
	ctx, cancel := context.WithTimeout(context.Background(), composioResolveTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v3/org/consumer/project/resolve", strings.NewReader("{}"))
	if err != nil {
		return ""
	}
	req.Header.Set("x-user-api-key", userAPIKey)
	req.Header.Set("x-org-id", orgID)
	req.Header.Set("User-Agent", "wuphf")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}
	var doc struct {
		// The resolve endpoint returns the pr_-prefixed id as project_nano_id;
		// project_id is a bare UUID that the API REJECTS (401) as x-project-id.
		// So we want the nano id — verified live.
		ProjectNanoID string `json:"project_nano_id"`
		ProjectID     string `json:"project_id"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return ""
	}
	if id := strings.TrimSpace(doc.ProjectNanoID); id != "" {
		return id
	}
	// Only fall back to project_id when it's already the pr_ form (some
	// endpoints name the pr_ id "project_id"); never return the UUID.
	if id := strings.TrimSpace(doc.ProjectID); strings.HasPrefix(id, "pr_") {
		return id
	}
	return ""
}

// composioUserDataPath mirrors the CLI's credential cache location:
// $COMPOSIO_CACHE_DIR/user_data.json, defaulting to ~/.composio/user_data.json.
func composioUserDataPath() string {
	if dir := strings.TrimSpace(os.Getenv("COMPOSIO_CACHE_DIR")); dir != "" {
		return filepath.Join(dir, "user_data.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".composio", "user_data.json")
}

// composioUserData is the subset of ~/.composio/user_data.json the sign-in flow
// reads: the uak_ session key, the org id, and the backend base URL.
type composioUserData struct {
	APIKey  string `json:"api_key"`
	OrgID   string `json:"org_id"`
	BaseURL string `json:"base_url"`
}

// readComposioUserData loads the CLI's session file written by `composio login`.
func readComposioUserData() (composioUserData, error) {
	path := composioUserDataPath()
	if path == "" {
		return composioUserData{}, errors.New("composio user_data path unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return composioUserData{}, err
	}
	var ud composioUserData
	if err := json.Unmarshal(data, &ud); err != nil {
		return composioUserData{}, err
	}
	return ud, nil
}

// composioCLILoggedIn reports whether the CLI has a stored session — the same
// check trustclaw uses: user_data.json exists and carries a non-empty api_key.
func composioCLILoggedIn() bool {
	ud, err := readComposioUserData()
	if err != nil {
		return false
	}
	return strings.TrimSpace(ud.APIKey) != ""
}

// parseEnvFileValue extracts KEY=VALUE from dotenv content, skipping comments
// and stripping surrounding quotes — the same shape the composio CLI writes.
func parseEnvFileValue(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		return value
	}
	return ""
}

// recordComposioSigninEvent writes the audit-trail entry for the flow. The
// summary is fixed copy — the key must never flow through here.
func (b *Broker) recordComposioSigninEvent(kind, actor, summary string) {
	if strings.TrimSpace(actor) == "" {
		actor = "human"
	}
	_ = b.RecordActionWithMetadata(kind, "composio", "general", actor, summary, "", nil, "", map[string]string{
		"provider": "composio",
	})
}

func writeComposioSigninState(w http.ResponseWriter, state composioSigninState) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}
