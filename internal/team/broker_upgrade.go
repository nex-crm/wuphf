package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/nex-crm/wuphf/internal/buildinfo"
	"github.com/nex-crm/wuphf/internal/upgradecheck"
)

func (b *Broker) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(buildinfo.Current())
}

// handleUpgradeCheck proxies the npm-registry comparison through the broker
// instead of letting the browser hit registry.npmjs.org directly. The web
// banner uses this so that (a) we have one server-side place to cache the
// result, (b) a corporate-NAT'd workstation doesn't burn every user's
// anonymous npm/GitHub quota on every page load.
func (b *Broker) handleUpgradeCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	res, err := upgradeCheckCached(r.Context())
	w.Header().Set("Content-Type", "application/json")
	// upgradecheck.Check populates Current from buildinfo BEFORE the
	// upstream call, so a transport failure still arrives with
	// Current set. The right "do we have anything useful" signal is
	// Latest — empty Latest with an error means we couldn't talk to
	// npm at all (cold cache + outage), which deserves a 502 so
	// observability tools see the upstream failure. Banner's
	// .catch() already swallows non-2xx silently.
	if err != nil && res.Latest == "" {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"current": res.Current,
			"error":   err.Error(),
		})
		return
	}
	// Run install detection so the banner can render the EXACT command
	// that POST /upgrade/run would execute (global vs local). Without
	// this, the chip showed "npm install -g wuphf@latest" universally
	// even when the broker would actually run `npm install wuphf@latest`
	// in a project dir — a misleading click target. Detection is
	// filesystem-bound (one Stat for global, walk-up for local), capped
	// in detectUpgradeInstall by upgradeRunDetectTime.
	plan := detectUpgradeInstallFn(r.Context())
	payload := map[string]any{
		"current":           res.Current,
		"latest":            res.Latest,
		"upgrade_available": res.UpgradeAvailable,
		"is_dev_build":      res.IsDevBuild,
		"compare_url":       res.CompareURL,
		// upgrade_command stays as the canonical "what we'd recommend
		// in docs" string; install_command is what the click ACTUALLY
		// runs on this host, so the banner can render the truthful
		// label. They MAY differ — local installs prefer
		// `npm install wuphf@latest`.
		"upgrade_command": res.UpgradeCommand,
		"install_method":  plan.Method,
		"install_command": plan.Command,
	}
	if err != nil {
		// Partial result (Current AND Latest populated, but something
		// else failed) — degrade gracefully with 200 + error field
		// so the banner can render nothing without a console-spamming
		// 5xx for a non-critical check.
		payload["error"] = err.Error()
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// handleUpgradeChangelog proxies the GitHub compare API for the same reasons
// as handleUpgradeCheck. The `from` and `to` query params are validated
// against a strict version regex so a crafted call cannot inject path
// segments into the upstream compare URL. Mirrors VERSION_RE in
// web/src/components/layout/upgradeBanner.utils.ts — keep both regexes in
// sync so a future tightening doesn't drift.
func (b *Broker) handleUpgradeChangelog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	w.Header().Set("Content-Type", "application/json")
	if !upgradeVersionParam.MatchString(from) || !upgradeVersionParam.MatchString(to) {
		// Match the upstream-failure shape so the banner's .catch
		// path renders one error message instead of branching on
		// HTTP status.
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"commits": []any{},
			"error":   "from/to must look like v0.79.10 (or 0.79.10)",
		})
		return
	}
	entries, err := upgradeChangelogCached(r.Context(), from, to)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"commits": []any{},
			"error":   err.Error(),
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"commits": entries})
}

// Semver-leaning version param. Accepts an optional `v`, 2-4 dotted numeric
// segments, an optional pre-release after `-` (e.g. `-rc.1`, `-beta-2`), and
// an optional build-metadata after `+` (e.g. `+build.5`). Hyphen is allowed
// inside the suffix character class so `-beta-1` validates.
//
// MIRRORED in web/src/components/layout/upgradeBanner.utils.ts as
// VERSION_RE — keep them in sync so a future tightening doesn't drift.
var upgradeVersionParam = regexp.MustCompile(`^v?\d+(\.\d+){1,3}(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// ── Upgrade-run ──────────────────────────────────────────────────────────
//
// POST /upgrade/run executes whichever `npm install` flavour matches how the
// running wuphf was installed:
//
//   • global (npm install -g wuphf)  → `npm install -g wuphf@latest`
//   • local  (project devDep)        → `npm install wuphf@latest` in that dir
//   • source build / standalone      → 400 with install_method="unknown"
//
// The detection runs `npm root -g` and walks up from the caller's cwd
// looking for `node_modules/wuphf/package.json`, so we never lie about which
// command we'd run. The label rendered by the banner is whatever this
// detection picks — banner cannot drift from server reality.

const (
	upgradeRunOutputCap  = 64 << 10 // truncate captured npm output at 64 KiB to bound response size
	upgradeRunDetectTime = 5 * time.Second
)

// upgradeRunTimeout caps the npm install at 120s. Declared as a var so
// tests can reduce it to a few milliseconds and exercise the timed-out
// branch deterministically; production never mutates it.
var upgradeRunTimeout = 120 * time.Second

type upgradeRunResult struct {
	OK            bool   `json:"ok"`
	InstallMethod string `json:"install_method"`        // "global", "local", or "unknown"
	Command       string `json:"command,omitempty"`     // human-readable rendered cmd (e.g. "npm install -g wuphf@latest")
	WorkingDir    string `json:"working_dir,omitempty"` // for local installs, the project dir we ran in
	Output        string `json:"output,omitempty"`      // combined stdout+stderr, truncated
	Error         string `json:"error,omitempty"`       // surfaces non-zero exit / spawn failure / detection failure
	TimedOut      bool   `json:"timed_out,omitempty"`
}

// upgradeInstallPlan is what `detectUpgradeInstall` returns: the args we'd
// hand to exec.Command and the human-readable rendering for the banner.
type upgradeInstallPlan struct {
	Method     string   // "global" | "local" | "unknown"
	Args       []string // e.g. ["install", "-g", "wuphf@latest"]; empty when Method=="unknown"
	WorkingDir string   // cwd to run npm in (project root for local installs; empty for global)
	Command    string   // human rendering (so the banner can show exactly what runs)
}

// detectUpgradeInstallFn is the seam tests use to pin a synthetic install
// plan without shelling out to `npm root -g`. Production points at the real
// implementation; broker_upgrade_test.go can swap it via t.Cleanup-restored
// assignment to drive the unknown / global / local handler paths
// deterministically.
var detectUpgradeInstallFn = detectUpgradeInstall

// runUpgradeCmdFn is the seam tests use to drive npm-success / npm-failure /
// npm-timeout paths without shelling out. Returns the trailing slice of
// merged stdout+stderr (capped at upgradeRunOutputCap), a `truncated` flag
// indicating whether anything was discarded ahead of that tail, and the
// underlying exec error (if any). Tests swap this to assert the handler
// maps results to the wire shape correctly. Production runs npm.
var runUpgradeCmdFn = runUpgradeCmd

func runUpgradeCmd(ctx context.Context, plan upgradeInstallPlan) ([]byte, bool, error) {
	cmd := exec.CommandContext(ctx, "npm", plan.Args...)
	if plan.WorkingDir != "" {
		cmd.Dir = plan.WorkingDir
	}
	// Bounded capture. CombinedOutput() (which this previously used)
	// buffers ALL of npm's stdout+stderr in memory before returning —
	// a verbose install (lots of warnings, integrity errors, progress
	// frames in some configs) could grow the broker process arbitrarily
	// even though the JSON response is later trimmed. tailBuffer caps
	// the in-flight buffer at upgradeRunOutputCap regardless of how
	// chatty npm gets, while preserving the tail (which is where the
	// real error / `+ wuphf@x.y.z` line lives). Setting the same
	// *tailBuffer for Stdout and Stderr also leans on os/exec's
	// guarantee that "at most one goroutine at a time will call Write"
	// when the two writers compare equal, so merge ordering matches
	// what the user would see in a terminal.
	buf := newTailBuffer(upgradeRunOutputCap)
	cmd.Stdout = buf
	cmd.Stderr = buf
	err := cmd.Run()
	return buf.Bytes(), buf.Truncated(), err
}

// tailBuffer is a thread-safe io.Writer that retains only the most
// recent maxBytes of input and tracks whether anything was discarded
// ahead of that tail. Used to bound broker memory while running npm
// (see runUpgradeCmd) without losing the trailing diagnostic the
// user actually needs.
type tailBuffer struct {
	mu        sync.Mutex
	buf       []byte
	maxBytes  int
	truncated bool
}

func newTailBuffer(maxBytes int) *tailBuffer {
	return &tailBuffer{maxBytes: maxBytes, buf: make([]byte, 0, maxBytes)}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n >= b.maxBytes {
		// p alone fills or exceeds the cap — any prior bytes are
		// older than the tail of p, so reset and keep p's suffix.
		// Mark truncated whenever we discard content: either p itself
		// overflowed (n > maxBytes) OR there were already buffered
		// bytes that are now being thrown away by the reset.
		if n > b.maxBytes || len(b.buf) > 0 {
			b.truncated = true
		}
		b.buf = append(b.buf[:0], p[n-b.maxBytes:]...)
		return n, nil
	}
	if len(b.buf)+n <= b.maxBytes {
		b.buf = append(b.buf, p...)
		return n, nil
	}
	// Combined size would exceed cap — drop the oldest bytes to make
	// room for p. The overlapping copy is safe per Go spec.
	excess := len(b.buf) + n - b.maxBytes
	copy(b.buf, b.buf[excess:])
	b.buf = b.buf[:len(b.buf)-excess]
	b.buf = append(b.buf, p...)
	b.truncated = true
	return n, nil
}

func (b *tailBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, len(b.buf))
	copy(out, b.buf)
	return out
}

func (b *tailBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// detectUpgradeInstall figures out how wuphf is installed on this machine.
// Order: global wins over local (most users `npm install -g`); local fallback
// catches the project-scoped install case.
//
// Local-install detection walks up from cwd looking for the nearest ancestor
// that BOTH declares wuphf in dependencies/devDependencies AND has
// `node_modules/wuphf/package.json` materialised. A non-declaring ancestor
// (e.g. a `packages/sub/package.json` in an npm-workspace monorepo where
// the dep lives at the workspace root) is skipped, not treated as a hard
// stop — otherwise detection would silently fail for monorepo users. A
// declaring ancestor whose dep is NOT yet installed (fresh clone before
// `npm install`) IS a hard stop: walking past would risk upgrading the
// wrong project's wuphf, and "unknown" plus the fallback copy is safer.
//
// The walk is bounded by:
//   - 32-iteration cap (belt-and-suspenders for runaway symlink loops),
//   - filesystem root (parent == dir),
//   - $HOME — even if a `package.json` at $HOME declared wuphf, running
//     `npm install` there would visibly mutate the user's home directory.
//
// Assumption: the broker process's cwd equals the user's cwd. Holds today
// because the wuphf CLI starts the broker and inherits cwd. A future
// long-lived daemon entry point (launchctl, systemd, sidecar container)
// would put the broker at "/", and this detection would silently fall
// back to "unknown" for everyone. If we add a daemon mode, plumb a
// per-request `working_dir` from the client and prefer it over os.Getwd()
// here, or move detection out of the broker entirely.
func detectUpgradeInstall(ctx context.Context) upgradeInstallPlan {
	if _, err := exec.LookPath("npm"); err != nil {
		return upgradeInstallPlan{Method: "unknown"}
	}
	// `npm root -g` prints the global node_modules path. Short timeout so a
	// borked npm config can't block the whole detect for the upstream budget.
	rootCtx, cancel := context.WithTimeout(ctx, upgradeRunDetectTime)
	defer cancel()
	if out, err := exec.CommandContext(rootCtx, "npm", "root", "-g").Output(); err == nil {
		globalRoot := strings.TrimSpace(string(out))
		if globalRoot != "" {
			if _, err := os.Stat(filepath.Join(globalRoot, "wuphf", "package.json")); err == nil {
				return upgradeInstallPlan{
					Method:  "global",
					Args:    []string{"install", "-g", "wuphf@latest"},
					Command: "npm install -g wuphf@latest",
				}
			}
		}
	}
	// Local install: walk up from cwd, skipping non-declaring ancestors so
	// monorepos with wuphf at the workspace root still detect cleanly when
	// cwd is inside a sub-package that doesn't declare it directly.
	homeDir, _ := os.UserHomeDir()
	cwd, err := os.Getwd()
	if err != nil {
		return upgradeInstallPlan{Method: "unknown"}
	}
	return detectLocalInstall(cwd, homeDir)
}

// detectLocalInstall walks up from `cwd` looking for the nearest ancestor
// that declares wuphf in package.json AND has node_modules/wuphf installed.
// Skips ancestors that don't declare wuphf (monorepo sub-packages); stops
// hard at a declaring-but-not-installed ancestor (fresh clone case),
// $HOME, the filesystem root, or after 32 hops. Extracted from
// detectUpgradeInstall so it can be unit-tested without depending on a
// real `npm` binary or a real user $HOME.
func detectLocalInstall(cwd, homeDir string) upgradeInstallPlan {
	dir := cwd
	for i := 0; i < 32; i++ {
		// $HOME guard. Stop BEFORE inspecting a package.json at
		// $HOME so a stray ~/package.json that happens to mention
		// wuphf can't redirect the install into the user's home.
		if homeDir != "" && dir == homeDir {
			break
		}
		projectPkg := filepath.Join(dir, "package.json")
		if data, err := os.ReadFile(projectPkg); err == nil {
			if pkgDeclaresWuphf(data) {
				candidate := filepath.Join(dir, "node_modules", "wuphf", "package.json")
				if _, err := os.Stat(candidate); err == nil {
					return upgradeInstallPlan{
						Method:     "local",
						Args:       []string{"install", "wuphf@latest"},
						WorkingDir: dir,
						Command:    "npm install wuphf@latest",
					}
				}
				// Declares wuphf but the dep isn't materialised
				// (fresh clone, skipped `npm install`). Don't keep
				// walking — a parent project's wuphf is NOT this
				// project's wuphf, and silently upgrading it would
				// be the wrong fix for "this project hasn't been
				// installed yet." Surface "unknown" and let the
				// click-to-run UI render its explicit fallback.
				break
			}
			// package.json that doesn't declare wuphf: a monorepo
			// sub-package or an unrelated project at this level.
			// Keep walking up — the workspace root above might be
			// the actual install target. The $HOME guard above
			// and the filesystem-root check below bound this.
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return upgradeInstallPlan{Method: "unknown"}
}

// pkgDeclaresWuphf reports whether the given package.json bytes declare
// wuphf in dependencies or devDependencies. Robust to extra unknown fields
// (peerDependencies, optionalDependencies, etc. — those do NOT count for
// `npm install <pkg>@latest` semantics, which only updates real deps).
func pkgDeclaresWuphf(data []byte) bool {
	var pkg struct {
		Dependencies    map[string]json.RawMessage `json:"dependencies"`
		DevDependencies map[string]json.RawMessage `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	if _, ok := pkg.Dependencies["wuphf"]; ok {
		return true
	}
	if _, ok := pkg.DevDependencies["wuphf"]; ok {
		return true
	}
	return false
}

// handleUpgradeRun executes the matching install command and returns the
// captured output. Auth-required, POST-only. Output is truncated to
// upgradeRunOutputCap so a chatty npm log can't bloat the JSON response.
//
// Concurrency: a process-wide single-flight (atomic.Bool) refuses overlapping
// runs. Two browser tabs racing to click "install" would otherwise spawn
// concurrent `npm install` against the same node_modules and risk a partial-
// extract corruption. The second arrival gets a JSON error and the first
// run remains the source of truth.
func (b *Broker) handleUpgradeRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Body is required to be JSON {} by the client, but we don't read
	// any fields from it. Bound it AND actually read it so a
	// misbehaving (authenticated) caller can't inflate broker memory
	// by streaming a large body — net/http buffers headers but reads
	// bodies on demand, so MaxBytesReader without a read attempt is a
	// no-op (the wrapped Read is never called). io.Copy here forces
	// MaxBytesReader to enforce the cap; if exceeded it short-circuits
	// the connection and we surface a 413 so the client gets a clear
	// error rather than a cryptic decode failure.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	defer r.Body.Close()
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		http.Error(w, "request body exceeds 1 KiB cap", http.StatusRequestEntityTooLarge)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	// Single-flight gate. CompareAndSwap returns false if a run is
	// already underway; the second arrival short-circuits with a 200 +
	// error so the banner can render "another install is running" copy
	// without breaking the .catch / non-2xx path.
	if !b.upgradeRunInFlight.CompareAndSwap(false, true) {
		_ = json.NewEncoder(w).Encode(upgradeRunResult{
			OK: false,
			// Set InstallMethod explicitly so the wire shape stays
			// inside the "global"|"local"|"unknown" union the client
			// types as. The single-flight rejection happens before we
			// run detection, so we don't have a real method to report
			// — "unknown" is the documented fallback for "we can't say".
			InstallMethod: "unknown",
			Error:         "an upgrade is already running on this broker — wait for it to finish",
		})
		return
	}
	defer b.upgradeRunInFlight.Store(false)

	plan := detectUpgradeInstallFn(r.Context())
	if plan.Method == "unknown" {
		// 200 so the banner can render the message inline without
		// triggering a console-spamming 4xx for what is fundamentally
		// "we can't help here, here's why."
		_ = json.NewEncoder(w).Encode(upgradeRunResult{
			OK:            false,
			InstallMethod: plan.Method,
			Error:         "Could not detect how wuphf was installed (no npm, or wuphf isn't under a global/local node_modules). Try `npm install -g wuphf@latest` from a terminal.",
		})
		return
	}

	// Use a broker-owned ctx so a CLIENT cancellation (page nav, tab
	// close) does not abort npm mid-install — interrupting npm between
	// download and rename can leave a half-extracted node_modules entry
	// that the next launch trips over. Process-level termination
	// (SIGKILL, OS shutdown) can still leave a partial install — npm's
	// own atomic-rename usually recovers, but this is not a hard
	// guarantee. The 120s ceiling caps a runaway without being so
	// short it trips on a typical 5-30s install.
	runCtx, cancel := context.WithTimeout(context.Background(), upgradeRunTimeout)
	defer cancel()
	// Couple to b.stopCh so a graceful broker shutdown (Stop()) cancels
	// an in-flight install. Without this, `wuphf restart` clicked right
	// after install-click would leave the OLD broker's npm running for
	// up to 120s while the NEW broker spawns its own npm against the
	// same prefix — exactly the half-extract race that the single-flight
	// in this handler is meant to prevent. The defer-cancel above closes
	// runCtx on normal return, which races the goroutine to the same
	// cancel(); cancel() is documented as safe to call multiple times.
	go func() {
		select {
		case <-b.stopCh:
			cancel()
		case <-runCtx.Done():
		}
	}()

	// runUpgradeCmdFn merges stdout+stderr in invocation order — npm
	// progress hits stderr, the final `+ wuphf@x.y.z` line hits stdout,
	// and the banner shows them in the order the user would see in a
	// terminal. The capture is bounded inside runUpgradeCmdFn so a
	// verbose log can't bloat broker memory; `truncated` tells us
	// whether bytes were dropped ahead of the returned tail so we can
	// surface a sentinel to the client.
	out, truncated, err := runUpgradeCmdFn(runCtx, plan)
	output := string(out)
	if truncated {
		output = withTruncationSentinel(output)
	}

	res := upgradeRunResult{
		InstallMethod: plan.Method,
		Command:       plan.Command,
		WorkingDir:    plan.WorkingDir,
		Output:        output,
	}
	if err != nil {
		res.OK = false
		// runCtx.Err() distinguishes a real timeout from the underlying
		// process exit error — npm exits non-zero on EACCES too, and we
		// want the banner to tell those apart so users see "needs sudo"
		// vs "took too long" with separate copy.
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			res.TimedOut = true
			res.Error = fmt.Sprintf("npm install timed out after %s", upgradeRunTimeout)
		} else {
			res.Error = err.Error()
		}
	} else {
		res.OK = true
	}
	_ = json.NewEncoder(w).Encode(res)
}

// truncationSentinel marks the front of an output string that had earlier
// bytes dropped (because the bounded tailBuffer threw them away). 18 bytes
// in UTF-8.
const truncationSentinel = "…[truncated]…\n"

// withTruncationSentinel prepends truncationSentinel to s, first skipping
// any leading UTF-8 continuation bytes so the returned string starts on a
// complete rune. The bounded write in tailBuffer can cut mid-rune; this
// turns that orphan continuation into a clean cut so the JSON encoder
// doesn't replace the leading byte with U+FFFD on the client.
func withTruncationSentinel(s string) string {
	start := 0
	for start < len(s) && s[start]&0xC0 == 0x80 {
		// 10xxxxxx: continuation byte. Skip until we land on a
		// leading byte (0xxxxxxx, 110xxxxx, 1110xxxx, 11110xxx).
		start++
	}
	return truncationSentinel + s[start:]
}

// ── Upgrade-check caching ─────────────────────────────────────────────────
//
// Memoise the npm registry result for 30 minutes and the GitHub compare
// result for 1 hour so N tabs / page reloads / shell remounts on one
// machine result in at most one upstream hit per TTL window. Coalesces
// concurrent requests via singleflight so a parallel page-load burst still
// only sends one upstream call. Without this, an office full of users on
// the same NAT could exhaust npm/GitHub's anonymous quota in normal use.

const (
	upgradeCheckTTL     = 30 * time.Minute
	upgradeChangelogTTL = time.Hour
	upgradeUpstreamTime = 5 * time.Second
	// Negative-cache window: pin upstream failures briefly so an outage
	// doesn't bypass the cache and let every banner load hammer
	// npm/GitHub. Short enough that recovery is observed quickly;
	// singleflight only collapses concurrent bursts, not back-to-back
	// retries.
	upgradeFailureTTL = 60 * time.Second
)

// upgradeChangelogKey is the canonical cache key for the changelog entry
// keyed by (from, to). Exported (lowercase but used from
// broker_upgrade_test.go in the same package) so the test that pre-seeds
// the cache uses the same encoding the production code does — keeps them
// from drifting.
func upgradeChangelogKey(from, to string) string { return from + "→" + to }

type upgradeCheckCacheEntry struct {
	res     upgradecheck.Result
	err     error
	storeAt time.Time
}

type upgradeChangelogCacheEntry struct {
	entries []upgradecheck.CommitEntry
	err     error
	storeAt time.Time
}

var (
	upgradeCheckCache   atomic.Pointer[upgradeCheckCacheEntry]
	upgradeChangelog    sync.Map // key "from→to" → *upgradeChangelogCacheEntry
	upgradeCheckGroup   singleflight.Group
	upgradeChangelogFlt singleflight.Group
)

func upgradeCheckCached(_ context.Context) (upgradecheck.Result, error) {
	if e := upgradeCheckCache.Load(); e != nil {
		// Successes use the long TTL; failures use the short
		// negative-TTL so we recover from an outage without
		// re-hammering during the window.
		ttl := upgradeCheckTTL
		if e.err != nil {
			ttl = upgradeFailureTTL
		}
		if time.Since(e.storeAt) < ttl {
			return e.res, e.err
		}
	}
	// Singleflight leader uses a broker-owned context, NOT the caller's
	// HTTP ctx — otherwise a single client cancellation (page nav,
	// browser close) aborts the upstream fetch for every concurrent
	// waiter. The waiters have their own contexts that gate when they
	// observe the result; only the leader's context governs the actual
	// upstream call.
	v, _, _ := upgradeCheckGroup.Do("check", func() (any, error) {
		fctx, cancel := context.WithTimeout(context.Background(), upgradeUpstreamTime)
		defer cancel()
		res, err := upgradecheck.Check(fctx, nil)
		// Cache success AND failure (different TTLs above) so a
		// stream of failed retries during an outage doesn't bypass
		// the broker shielding this feature exists to provide.
		upgradeCheckCache.Store(&upgradeCheckCacheEntry{res: res, err: err, storeAt: time.Now()})
		return upgradeCheckCacheEntry{res: res, err: err}, nil
	})
	entry := v.(upgradeCheckCacheEntry)
	return entry.res, entry.err
}

func upgradeChangelogCached(_ context.Context, from, to string) ([]upgradecheck.CommitEntry, error) {
	key := upgradeChangelogKey(from, to)
	if v, ok := upgradeChangelog.Load(key); ok {
		e := v.(*upgradeChangelogCacheEntry)
		ttl := upgradeChangelogTTL
		if e.err != nil {
			ttl = upgradeFailureTTL
		}
		if time.Since(e.storeAt) < ttl {
			return e.entries, e.err
		}
	}
	// See upgradeCheckCached: leader uses broker-owned background ctx
	// so client cancellations don't kill the shared upstream fetch.
	v, _, _ := upgradeChangelogFlt.Do(key, func() (any, error) {
		fctx, cancel := context.WithTimeout(context.Background(), upgradeUpstreamTime+3*time.Second)
		defer cancel()
		entries, err := upgradecheck.FetchChangelog(fctx, nil, from, to)
		upgradeChangelog.Store(key, &upgradeChangelogCacheEntry{entries: entries, err: err, storeAt: time.Now()})
		return upgradeChangelogCacheEntry{entries: entries, err: err}, nil
	})
	entry := v.(upgradeChangelogCacheEntry)
	return entry.entries, entry.err
}
