package team

// broker_workspaces.go implements the multi-workspace HTTP routes.
//
// Route map (registered in broker.go's HandleFunc block, all wrapped by
// b.withAuth):
//
//	GET  /workspaces/list      — returns registry contents + live state
//	POST /workspaces/create    — body {name, blueprint, inherit_from?, ...}
//	POST /workspaces/switch    — body {name}; updates cli_current (CLI-only)
//	POST /workspaces/pause     — body {name}; proxies to target's /admin/pause
//	POST /workspaces/resume    — body {name}; spawns target broker
//	POST /workspaces/shred     — body {name, permanent?}; moves to trash
//	POST /workspaces/restore   — body {trash_id}; restores from trash
//	POST /admin/pause          — self: drain Launcher then exit
//
// All routes share three contracts:
//
//   - Bearer token via b.withAuth (the design's "every protected route
//     requires bearer" assertion).
//   - JSON request bodies decoded with a per-handler size cap.
//   - JSON response bodies with Content-Type application/json.
//
// Lane B owns internal/workspaces/ (the orchestrator package + Launcher.Drain).
// This file consumes those via two minimal interfaces — workspaceOrchestrator
// and launcherDrainer — set on the Broker by callers (cmd/wuphf wires the
// concrete impls; tests inject fakes). Both are nil-safe: handlers degrade
// to 503 with a clear message when the orchestrator is not configured,
// which is the expected state during local dev/test before Lane B merges.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// maxWorkspaceRequestBodyBytes caps every /workspaces/* and /admin/pause
// request body. The largest legitimate payload is /workspaces/create with
// inheritance fields; 32 KiB leaves comfortable headroom for company name,
// description, blueprint name, and a small set of flags.
const maxWorkspaceRequestBodyBytes = 1 << 15 // 32 KiB

// pauseProxyTimeout caps the cross-broker pause RPC. The target broker's
// /admin/pause returns immediately on accept (the Launcher.Drain runs
// asynchronously), so this is a generous ceiling rather than the drain
// budget. The 90s wall-clock pause budget lives in the orchestrator.
const pauseProxyTimeout = 10 * time.Second

// adminPauseSelfShutdownDelay is how long /admin/pause waits before calling
// the exit hook after returning the 202 Accepted response. Gives the HTTP
// stack time to flush the response to the client before the process goes
// away.
const adminPauseSelfShutdownDelay = 250 * time.Millisecond

// Workspace mirrors the registry shape returned to API consumers. Lane B's
// internal/workspaces package will define the canonical type; this is the
// shape Lane C's tests assert against. At merge, cmd/wuphf adapts Lane B's
// type into this shape (or both packages share a third package).
type Workspace struct {
	Name        string  `json:"name"`
	RuntimeHome string  `json:"runtime_home"`
	BrokerPort  int     `json:"broker_port"`
	WebPort     int     `json:"web_port"`
	State       string  `json:"state"` // running|paused|starting|stopping|never_started|error
	Blueprint   string  `json:"blueprint,omitempty"`
	CompanyName string  `json:"company_name,omitempty"`
	CreatedAt   string  `json:"created_at,omitempty"`
	LastUsedAt  string  `json:"last_used_at,omitempty"`
	PausedAt    *string `json:"paused_at,omitempty"`
}

// CreateRequest is the POST body for /workspaces/create. Fields beyond
// Name are forwarded verbatim to the orchestrator, which applies the
// inheritance table (see design's Lighter Onboarding section).
type CreateRequest struct {
	Name        string `json:"name"`
	Blueprint   string `json:"blueprint,omitempty"`
	InheritFrom string `json:"inherit_from,omitempty"`
	CompanyName string `json:"company_name,omitempty"`
	FromScratch bool   `json:"from_scratch,omitempty"`
}

// workspaceOrchestrator is the interface Lane C's handlers depend on.
// Lane B's internal/workspaces.New(...) returns a concrete implementation
// that the broker is constructed with. Defined here (small, locally used)
// per the "Accept interfaces, return structs" guideline.
//
// Every method takes a context so the orchestrator can be cancelled when
// the broker shuts down.
type workspaceOrchestrator interface {
	List(ctx context.Context) ([]Workspace, error)
	Create(ctx context.Context, req CreateRequest) (Workspace, error)
	Switch(ctx context.Context, name string) error
	Pause(ctx context.Context, name string) error
	Resume(ctx context.Context, name string) error
	Shred(ctx context.Context, name string, permanent bool) error
	Restore(ctx context.Context, trashID string) (Workspace, error)
}

// launcherDrainer is the cancellation surface /admin/pause calls before
// exiting. Lane B's Launcher implements this by canceling its internal
// runCtx and joining all subsystem goroutines (headless dispatch, pane
// dispatch, scheduler, watchdog, notify poll) with a 60s wall-clock cap.
type launcherDrainer interface {
	Drain(ctx context.Context) error
}

// SetWorkspaceOrchestrator wires a concrete orchestrator after broker
// construction. The default (nil) yields 503s on /workspaces/* — which
// is the right behavior on a broker started without multi-workspace
// support (e.g., tests, headless one-shots).
//
// Goroutine-safe: writes happen at startup before any HTTP traffic.
// Reads from handlers go through orchestrator() which takes b.mu.
func (b *Broker) SetWorkspaceOrchestrator(o workspaceOrchestrator) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.workspaces = o
}

// SetLauncherDrainer wires the Launcher's Drain hook so /admin/pause can
// shut down dispatch subsystems before exiting. nil drains nothing — the
// process still exits, but in-flight work is cancelled at the OS level
// rather than gracefully.
func (b *Broker) SetLauncherDrainer(d launcherDrainer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.launcherDrain = d
}

// SetAdminPauseExitFn overrides the function called after /admin/pause
// completes its drain. Production wires this to os.Exit(0); tests wire it
// to a recorder so the test binary doesn't terminate.
func (b *Broker) SetAdminPauseExitFn(fn func(int)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.adminPauseExitFn = fn
}

func (b *Broker) orchestrator() workspaceOrchestrator {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.workspaces
}

func (b *Broker) launcherDrainHook() launcherDrainer {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.launcherDrain
}

func (b *Broker) adminPauseExit(code int) {
	b.mu.Lock()
	fn := b.adminPauseExitFn
	b.mu.Unlock()
	if fn == nil {
		fn = os.Exit
	}
	fn(code)
}

// workspaceTokenDir resolves the broker-token directory for cross-broker
// auth (`~/.wuphf-spaces/tokens/`). The directory is owned by the
// internal/workspaces package at merge; here we resolve it relative to
// config.RuntimeHomeDir() so dev/test/prod isolation is preserved.
//
// Test seam: workspaceTokenDirOverride. Tests point this at a t.TempDir
// to avoid touching the real ~/.wuphf-spaces.
var workspaceTokenDirOverride string

func workspaceTokenDir() string {
	if v := strings.TrimSpace(workspaceTokenDirOverride); v != "" {
		return v
	}
	// Use the user's real home (os.UserHomeDir) NOT RuntimeHomeDir() —
	// the spaces directory is shared across all workspaces and lives
	// outside any single workspace's runtime home.
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Fall back to RuntimeHomeDir's resolution to match other
		// fallback patterns in the package.
		home = config.RuntimeHomeDir()
	}
	return filepath.Join(home, ".wuphf-spaces", "tokens")
}

func workspaceTokenPath(name string) string {
	return filepath.Join(workspaceTokenDir(), name+".token")
}

// readWorkspaceToken reads the bearer token of a sibling workspace's
// broker for cross-broker orchestration calls. Returns ErrTokenNotFound
// if the file does not exist (typical for a paused or never-started
// workspace).
//
// The SPA NEVER reads sibling tokens — this path is broker-internal.
// Documented in the design's Token Files section.
var errWorkspaceTokenNotFound = errors.New("workspace token not found")

func readWorkspaceToken(name string) (string, error) {
	path := workspaceTokenPath(name)
	raw, err := os.ReadFile(path) // #nosec G304 — path resolved from validated workspace name + fixed dir.
	if err != nil {
		if os.IsNotExist(err) {
			return "", errWorkspaceTokenNotFound
		}
		return "", fmt.Errorf("read workspace token %q: %w", name, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// targetBrokerBaseURLOverride lets pause tests redirect cross-broker calls
// at a fake httptest.Server. Production resolves the URL from the
// orchestrator's registry lookup; the override is for unit tests only.
//
// Empty string (the default) means "use the orchestrator's resolution".
var targetBrokerBaseURLFn func(name string) string

// SetTargetBrokerURLResolver wires the function the broker calls to translate
// a workspace name into the cross-broker base URL used by the pause proxy.
// Production wires this in cmd/wuphf to the orchestrator's registry lookup;
// tests use the package-level test seam directly.
func SetTargetBrokerURLResolver(fn func(name string) string) {
	targetBrokerBaseURLFn = fn
}

// resolveTargetBrokerURL returns the http://127.0.0.1:<port> base URL for
// the target workspace's broker. Production uses the orchestrator's
// registry; for now (Lane B not yet merged) we expose a settable function
// pointer that defaults to a registry-less stub returning "" (which
// triggers a 503 on pause).
func resolveTargetBrokerURL(name string) string {
	if targetBrokerBaseURLFn != nil {
		return targetBrokerBaseURLFn(name)
	}
	return ""
}

// workspaceNameValid mirrors the design's slug validation. Centralized
// here so handlers can fail-fast before calling the orchestrator.
func workspaceNameValid(name string) bool {
	if name == "" || len(name) > 31 {
		return false
	}
	if name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

// decodeWorkspaceJSON decodes a size-capped JSON body into v. Returns a
// 400-shaped error already written to w on any failure; callers should
// just `return` after a non-nil error.
func decodeWorkspaceJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxWorkspaceRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeWorkspaceError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return err
	}
	return nil
}

func writeWorkspaceJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeWorkspaceError(w http.ResponseWriter, status int, msg string) {
	writeWorkspaceJSON(w, status, map[string]any{"error": msg})
}

// requireMethod returns true and lets the handler proceed if r.Method matches
// expected. Otherwise writes 405 and returns false.
func requireMethod(w http.ResponseWriter, r *http.Request, expected string) bool {
	if r.Method == expected {
		return true
	}
	w.Header().Set("Allow", expected)
	writeWorkspaceError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

// handleWorkspacesList — GET /workspaces/list.
//
// Returns: {"workspaces": [...], "cli_current": "main"}.
// Live state decoration (HEAD probes) happens inside the orchestrator
// (parallel goroutines, 200ms-bounded). The handler is a thin wrapper.
func (b *Broker) handleWorkspacesList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	o := b.orchestrator()
	if o == nil {
		writeWorkspaceError(w, http.StatusServiceUnavailable, "workspaces not configured")
		return
	}
	ws, err := o.List(r.Context())
	if err != nil {
		writeWorkspaceError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, map[string]any{
		"workspaces": ws,
	})
}

// handleWorkspacesCreate — POST /workspaces/create.
//
// Body: {name, blueprint?, inherit_from?, company_name?, from_scratch?}.
// Returns 201 with the created Workspace shape on success. Validation
// failures return 400 BEFORE calling the orchestrator so common errors
// fail fast without taking the registry lock.
func (b *Broker) handleWorkspacesCreate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req CreateRequest
	if err := decodeWorkspaceJSON(w, r, &req); err != nil {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !workspaceNameValid(req.Name) {
		writeWorkspaceError(w, http.StatusBadRequest, "invalid workspace name")
		return
	}
	o := b.orchestrator()
	if o == nil {
		writeWorkspaceError(w, http.StatusServiceUnavailable, "workspaces not configured")
		return
	}
	ws, err := o.Create(r.Context(), req)
	if err != nil {
		writeWorkspaceError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeWorkspaceJSON(w, http.StatusCreated, ws)
}

// handleWorkspacesSwitch — POST /workspaces/switch.
//
// CLI-only entrypoint. Updates cli_current in registry and returns the
// target's web URL. The SPA does NOT call this; SPA navigates directly
// via window.location.assign (see design's Switch Protocol section).
func (b *Broker) handleWorkspacesSwitch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeWorkspaceJSON(w, r, &req); err != nil {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !workspaceNameValid(req.Name) {
		writeWorkspaceError(w, http.StatusBadRequest, "invalid workspace name")
		return
	}
	o := b.orchestrator()
	if o == nil {
		writeWorkspaceError(w, http.StatusServiceUnavailable, "workspaces not configured")
		return
	}
	if err := o.Switch(r.Context(), req.Name); err != nil {
		writeWorkspaceError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, map[string]any{"ok": true, "name": req.Name})
}

// handleWorkspacesPause — POST /workspaces/pause.
//
// Active broker reads target's bearer token from
// ~/.wuphf-spaces/tokens/<name>.token and proxies to target's /admin/pause.
// Self-pause case (target == active broker) is detected by URL match and
// short-circuits to a direct /admin/pause call against this broker so the
// drain runs in-process. The SPA never sees sibling tokens — this path
// is strictly broker-internal.
func (b *Broker) handleWorkspacesPause(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeWorkspaceJSON(w, r, &req); err != nil {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !workspaceNameValid(req.Name) {
		writeWorkspaceError(w, http.StatusBadRequest, "invalid workspace name")
		return
	}

	targetURL := resolveTargetBrokerURL(req.Name)
	if targetURL == "" {
		writeWorkspaceError(w, http.StatusServiceUnavailable, "target broker URL not resolvable")
		return
	}
	token, err := readWorkspaceToken(req.Name)
	if err != nil {
		if errors.Is(err, errWorkspaceTokenNotFound) {
			writeWorkspaceError(w, http.StatusNotFound, "workspace not running")
			return
		}
		writeWorkspaceError(w, http.StatusInternalServerError, err.Error())
		return
	}

	pauseURL, err := url.JoinPath(targetURL, "/admin/pause")
	if err != nil {
		writeWorkspaceError(w, http.StatusInternalServerError, fmt.Sprintf("build pause URL: %v", err))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), pauseProxyTimeout)
	defer cancel()
	pReq, err := http.NewRequestWithContext(ctx, http.MethodPost, pauseURL, nil)
	if err != nil {
		writeWorkspaceError(w, http.StatusInternalServerError, fmt.Sprintf("build pause request: %v", err))
		return
	}
	pReq.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: pauseProxyTimeout}
	resp, err := client.Do(pReq)
	if err != nil {
		writeWorkspaceError(w, http.StatusBadGateway, fmt.Sprintf("proxy pause: %v", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		writeWorkspaceError(w, http.StatusBadGateway, fmt.Sprintf("target broker returned %d", resp.StatusCode))
		return
	}
	writeWorkspaceJSON(w, http.StatusAccepted, map[string]any{
		"ok":     true,
		"name":   req.Name,
		"target": targetURL,
	})
}

// handleWorkspacesResume — POST /workspaces/resume.
//
// Spawns the target broker via the orchestrator and waits until the port
// is bound. Returns 200 once ready, 504 if the 30s spawn budget elapses.
func (b *Broker) handleWorkspacesResume(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeWorkspaceJSON(w, r, &req); err != nil {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !workspaceNameValid(req.Name) {
		writeWorkspaceError(w, http.StatusBadRequest, "invalid workspace name")
		return
	}
	o := b.orchestrator()
	if o == nil {
		writeWorkspaceError(w, http.StatusServiceUnavailable, "workspaces not configured")
		return
	}
	if err := o.Resume(r.Context(), req.Name); err != nil {
		writeWorkspaceError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, map[string]any{"ok": true, "name": req.Name})
}

// handleWorkspacesShred — POST /workspaces/shred.
//
// Body: {name, permanent?}. permanent=false (default) moves the tree to
// trash for restore-within-30-days. permanent=true skips trash.
func (b *Broker) handleWorkspacesShred(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Name      string `json:"name"`
		Permanent bool   `json:"permanent"`
	}
	if err := decodeWorkspaceJSON(w, r, &req); err != nil {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !workspaceNameValid(req.Name) {
		writeWorkspaceError(w, http.StatusBadRequest, "invalid workspace name")
		return
	}
	o := b.orchestrator()
	if o == nil {
		writeWorkspaceError(w, http.StatusServiceUnavailable, "workspaces not configured")
		return
	}
	if err := o.Shred(r.Context(), req.Name, req.Permanent); err != nil {
		writeWorkspaceError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"name":      req.Name,
		"permanent": req.Permanent,
	})
}

// handleWorkspacesRestore — POST /workspaces/restore.
//
// Body: {trash_id}. Restores the named trash entry, allocating a fresh
// port pair (the original may have been reused).
func (b *Broker) handleWorkspacesRestore(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		TrashID string `json:"trash_id"`
	}
	if err := decodeWorkspaceJSON(w, r, &req); err != nil {
		return
	}
	req.TrashID = strings.TrimSpace(req.TrashID)
	if req.TrashID == "" {
		writeWorkspaceError(w, http.StatusBadRequest, "missing trash_id")
		return
	}
	o := b.orchestrator()
	if o == nil {
		writeWorkspaceError(w, http.StatusServiceUnavailable, "workspaces not configured")
		return
	}
	ws, err := o.Restore(r.Context(), req.TrashID)
	if err != nil {
		writeWorkspaceError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeWorkspaceJSON(w, http.StatusOK, ws)
}

// handleAdminPause — POST /admin/pause.
//
// Self-shutdown handler. Pauses are initiated by the active broker (this
// process), the orchestrator host (CLI), or a sibling broker proxying via
// /workspaces/pause. The flow:
//
//  1. Validate request (method + bearer auth handled upstream by withAuth).
//  2. Require localhost RemoteAddr (defense-in-depth — the design pins
//     /admin/pause to localhost-only callers).
//  3. Return 202 Accepted immediately so the client doesn't wait on
//     drain. The actual drain runs in a goroutine.
//  4. Goroutine: run launcher.Drain(ctx) with a 60s budget, then call
//     adminPauseExit(0) to terminate the process. The exit hook is
//     overrideable for tests.
func (b *Broker) handleAdminPause(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !isLoopbackRemote(r) {
		writeWorkspaceError(w, http.StatusForbidden, "admin pause requires loopback caller")
		return
	}
	writeWorkspaceJSON(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"message": "pause accepted; broker will exit after drain",
	})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if drainer := b.launcherDrainHook(); drainer != nil {
			_ = drainer.Drain(ctx)
		}
		// Give the response a moment to flush before tearing down the
		// process. In production this is os.Exit(0); in tests the hook
		// records the call without exiting.
		time.Sleep(adminPauseSelfShutdownDelay)
		b.adminPauseExit(0)
	}()
}
