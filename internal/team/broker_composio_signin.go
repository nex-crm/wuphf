package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	if _, err := exec.LookPath("composio"); err != nil {
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
// the sign-in if the CLI landed on PATH. On failure it falls back to
// cli_missing with the manual install command. PATH (not the exit code) is the
// source of truth — installers can warn-and-exit nonzero while still placing
// the binary.
func (b *Broker) composioSigninAutoInstall() {
	ctx, cancel := context.WithTimeout(context.Background(), composioInstallTimeout)
	defer cancel()
	runErr := composioInstaller(ctx)
	if _, err := exec.LookPath("composio"); err != nil {
		flow := &b.composioSignin
		flow.mu.Lock()
		if flow.state.Status == composioSigninStatusInstalling {
			reason := "could not install the Composio CLI automatically — run the install command shown, then try again"
			if runErr == nil {
				reason = "the Composio installer finished but the CLI is still not on PATH — run the install command shown, then try again"
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
	cmd := exec.CommandContext(ctx, "composio", "login", "--poll")
	// Output deliberately discarded: never echo CLI output that may carry
	// credentials into broker logs.
	_ = cmd.Run()
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

// composioSigninProvision mints the project-scoped ak_ key via
// `composio dev init` in a throwaway directory and stores it through the same
// config path as the manual paste flow.
func (b *Broker) composioSigninProvision() {
	flow := &b.composioSignin
	key, err := composioProvisionProjectKey()
	var storeFailed bool
	if err == nil {
		if err = b.storeComposioAPIKey(key); err != nil {
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
			reason = "could not save the Composio API key to config — check the broker logs"
		}
		flow.state = composioSigninState{Status: composioSigninStatusError, Reason: reason}
		flow.mu.Unlock()
		return
	}
	flow.mu.Unlock()
	// Record the audit entry BEFORE flipping to done: a client that observes
	// done must also observe the audit trail entry.
	b.recordComposioSigninEvent("integration_signin_completed", actor, "Signed in with Composio and stored the project API key")
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
	out, err := exec.CommandContext(ctx, "composio", "login", "--no-wait").CombinedOutput()
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
	cmd := exec.CommandContext(ctx, "composio", "dev", "init", "-y", "--no-browser")
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

// composioCLILoggedIn reports whether the CLI has a stored session — the same
// check trustclaw uses: user_data.json exists and carries a non-empty api_key.
func composioCLILoggedIn() bool {
	path := composioUserDataPath()
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false
	}
	return strings.TrimSpace(doc.APIKey) != ""
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
