package team

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nex-crm/wuphf/internal/upgradecheck"
)

// resetUpgradeCaches wipes the package-level upgrade cache state between
// tests so a stale entry from one test can't satisfy another.
func resetUpgradeCaches(t *testing.T) {
	t.Helper()
	upgradeCheckCache.Store(nil)
	upgradeChangelog.Range(func(k, _ any) bool {
		upgradeChangelog.Delete(k)
		return true
	})
}

func TestHandleUpgradeChangelog_RejectsPathTraversalParams(t *testing.T) {
	// The version-param regex MUST reject anything that isn't a clean
	// dotted-numeric (with optional v / pre-release) so a crafted call
	// can't inject path segments into the upstream GitHub compare URL.
	resetUpgradeCaches(t)
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeChangelog))
	defer srv.Close()

	cases := []string{
		"../../etc/passwd",
		"v0.79.10/extra",
		"v0.79.10;rm",
		"",
		"latest",
	}
	for _, bad := range cases {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"?from="+bad+"&to=v0.79.15", nil)
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("from=%q: request: %v", bad, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("from=%q: expected 400, got %d", bad, resp.StatusCode)
		}
	}
}

func TestHandleUpgradeChangelog_BadParamMatchesUpstreamErrorShape(t *testing.T) {
	// Callers should be able to render a bad-param response with the
	// same .catch path as an upstream-failure response — both must
	// expose `commits: []` + `error: "..."`. This locks the contract.
	resetUpgradeCaches(t)
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeChangelog))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"?from=garbage&to=v0.79.15", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var body struct {
		Commits []any  `json:"commits"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error == "" {
		t.Errorf("expected non-empty error field on 400, got %+v", body)
	}
	// Lock the documented contract: the bad-param path returns the
	// SAME shape as the upstream-failure path so the banner's .catch
	// can render one message instead of branching on HTTP status. A
	// future change that removes commits or returns null must fail
	// the test loudly.
	if body.Commits == nil {
		t.Errorf("expected commits to be an empty array, got nil")
	}
	if len(body.Commits) != 0 {
		t.Errorf("expected empty commits, got %d entries", len(body.Commits))
	}
}

func TestUpgradeEndpoints_RequireAuthToken(t *testing.T) {
	// Both endpoints MUST be behind requireAuth — they hit upstream
	// network and an unauthenticated caller could trivially exhaust
	// our shared rate-limit budget.
	resetUpgradeCaches(t)
	b := newTestBroker(t)
	cases := []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"upgrade-check", b.requireAuth(b.handleUpgradeCheck), ""},
		{"upgrade-changelog", b.requireAuth(b.handleUpgradeChangelog), "?from=v0.1.0&to=v0.1.1"},
	}
	for _, ep := range cases {
		t.Run(ep.name, func(t *testing.T) {
			srv := httptest.NewServer(ep.handler)
			defer srv.Close()
			resp, err := http.Get(srv.URL + ep.path) // no Authorization header
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("expected 401 without token, got %d", resp.StatusCode)
			}
		})
	}
}

func TestUpgradeEndpoints_RejectNonGetMethods(t *testing.T) {
	// Defense-in-depth: the upgrade endpoints are read-only and should
	// not respond to POST/PUT/DELETE — keeps the HTTP contract tight
	// for proxies that gate on method.
	resetUpgradeCaches(t)
	b := newTestBroker(t)
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"upgrade-check", b.requireAuth(b.handleUpgradeCheck)},
		{"upgrade-changelog", b.requireAuth(b.handleUpgradeChangelog)},
	}
	for _, ep := range cases {
		t.Run(ep.name, func(t *testing.T) {
			srv := httptest.NewServer(ep.handler)
			defer srv.Close()
			req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
			req.Header.Set("Authorization", "Bearer "+b.Token())
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("POST: expected 405, got %d", resp.StatusCode)
			}
			if got := resp.Header.Get("Allow"); got != "GET" {
				t.Errorf("expected Allow: GET, got %q", got)
			}
		})
	}
}

// pinDetectInstall swaps detectUpgradeInstallFn for the duration of the
// test. Restores the original on cleanup so concurrent tests don't observe
// each other's pin.
func pinDetectInstall(t *testing.T, plan upgradeInstallPlan) {
	t.Helper()
	prev := detectUpgradeInstallFn
	detectUpgradeInstallFn = func(_ context.Context) upgradeInstallPlan { return plan }
	t.Cleanup(func() { detectUpgradeInstallFn = prev })
}

func TestHandleUpgradeRun_RequiresAuth(t *testing.T) {
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeRun))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", resp.StatusCode)
	}
}

func TestHandleUpgradeRun_RejectsGet(t *testing.T) {
	// Mirrors the read-only endpoints' method gate: /upgrade/run is
	// state-changing and must reject GET. Without this, a proxy that
	// re-issues a GET on a 30x could trigger an unintended install.
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeRun))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: expected 405, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Allow"); got != "POST" {
		t.Errorf("expected Allow: POST, got %q", got)
	}
}

func TestHandleUpgradeRun_UnknownInstallMethodReturns200WithGuidance(t *testing.T) {
	// When detection can't find a global or local install (source build,
	// standalone download, broken npm), return 200 + install_method=
	// "unknown" + a human error message so the banner can render the
	// fallback path inline. We deliberately do NOT 4xx this — the
	// frontend's .catch path treats non-2xx as "broker unreachable" and
	// hides the banner, which would swallow the user-visible reason.
	pinDetectInstall(t, upgradeInstallPlan{Method: "unknown"})
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeRun))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, string(body))
	}
	var body upgradeRunResult
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.OK {
		t.Errorf("expected ok=false on unknown install method, got %+v", body)
	}
	if body.InstallMethod != "unknown" {
		t.Errorf("expected install_method=unknown, got %q", body.InstallMethod)
	}
	if !strings.Contains(body.Error, "npm install") {
		// Lock the documented contract: the unknown-method response
		// MUST include a copy-pasteable command in the error message
		// so a user without an automated path still has the manual
		// recipe one click away.
		t.Errorf("unknown-method error should reference npm install, got %q", body.Error)
	}
}

// pinRunCmd swaps runUpgradeCmdFn for the duration of the test. The fake
// receives the install plan so assertions can verify the handler passes
// through Args/WorkingDir correctly, and returns whatever
// (output, truncated, err) tuple the test wants the handler to observe.
func pinRunCmd(t *testing.T, fake func(ctx context.Context, plan upgradeInstallPlan) ([]byte, bool, error)) {
	t.Helper()
	prev := runUpgradeCmdFn
	runUpgradeCmdFn = fake
	t.Cleanup(func() { runUpgradeCmdFn = prev })
}

func TestHandleUpgradeRun_SuccessReturnsOKWithOutput(t *testing.T) {
	// Happy path: the fake npm exits 0 with characteristic + wuphf@…
	// output. Handler must surface ok=true, the install_method/command
	// from detection, and the trimmed output.
	pinDetectInstall(t, upgradeInstallPlan{
		Method:  "global",
		Args:    []string{"install", "-g", "wuphf@latest"},
		Command: "npm install -g wuphf@latest",
	})
	pinRunCmd(t, func(_ context.Context, _ upgradeInstallPlan) ([]byte, bool, error) {
		return []byte("added 1 package in 2s\n+ wuphf@99.0.0\n"), false, nil
	})
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeRun))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body upgradeRunResult
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Errorf("expected ok=true, got %+v", body)
	}
	if body.InstallMethod != "global" {
		t.Errorf("install_method=%q want global", body.InstallMethod)
	}
	if body.Command != "npm install -g wuphf@latest" {
		t.Errorf("command=%q drift", body.Command)
	}
	if !strings.Contains(body.Output, "wuphf@99.0.0") {
		t.Errorf("output should pass through unchanged on success, got %q", body.Output)
	}
	if body.Error != "" {
		t.Errorf("expected empty error on success, got %q", body.Error)
	}
}

func TestHandleUpgradeRun_FailureSurfacesError(t *testing.T) {
	// Failure path: exec returns a real error (e.g. EACCES, ENOENT, or
	// a non-zero npm exit). Handler must set ok=false and populate
	// `error` with the underlying message so the banner can render the
	// "needs sudo" / "npm not installed" copy distinctively.
	pinDetectInstall(t, upgradeInstallPlan{
		Method:  "global",
		Args:    []string{"install", "-g", "wuphf@latest"},
		Command: "npm install -g wuphf@latest",
	})
	pinRunCmd(t, func(_ context.Context, _ upgradeInstallPlan) ([]byte, bool, error) {
		return []byte("npm ERR! EACCES: permission denied\n"), false, &execError{msg: "exit status 243"}
	})
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeRun))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body upgradeRunResult
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.OK {
		t.Errorf("expected ok=false on exec error, got %+v", body)
	}
	if !strings.Contains(body.Output, "EACCES") {
		t.Errorf("output should include npm stderr on failure, got %q", body.Output)
	}
	if !strings.Contains(body.Error, "exit status 243") {
		t.Errorf("error should surface underlying exec error, got %q", body.Error)
	}
	if body.TimedOut {
		t.Errorf("non-timeout failure must NOT set timed_out=true, got %+v", body)
	}
}

func TestHandleUpgradeRun_TimeoutSetsTimedOutFlag(t *testing.T) {
	// Timeout path: drive the broker-side runCtx to actually expire by
	// shrinking upgradeRunTimeout to 1ms and having the fake block on
	// the ctx until it cancels. The handler's
	// errors.Is(runCtx.Err(), context.DeadlineExceeded) branch must
	// then set timed_out=true and surface the timeout-specific message
	// — distinct from a generic exec failure so the banner copy can
	// distinguish "still running, just slow" from "outright failed."
	prev := upgradeRunTimeout
	upgradeRunTimeout = 1 * time.Millisecond
	t.Cleanup(func() { upgradeRunTimeout = prev })

	pinDetectInstall(t, upgradeInstallPlan{
		Method:  "global",
		Args:    []string{"install", "-g", "wuphf@latest"},
		Command: "npm install -g wuphf@latest",
	})
	pinRunCmd(t, func(ctx context.Context, _ upgradeInstallPlan) ([]byte, bool, error) {
		<-ctx.Done() // honour the broker-owned ctx so runCtx.Err() trips
		return nil, false, ctx.Err()
	})
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeRun))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var body upgradeRunResult
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.OK {
		t.Errorf("expected ok=false on timeout, got %+v", body)
	}
	if !body.TimedOut {
		t.Errorf("expected timed_out=true, got %+v", body)
	}
	if !strings.Contains(body.Error, "timed out") {
		t.Errorf("expected timeout message, got %q", body.Error)
	}
}

// execError is a lightweight stand-in for *exec.ExitError that satisfies the
// `error` interface. We use it instead of running a real process so tests
// don't depend on /usr/bin/false (or its absence) on the build host. The
// handler treats this opaquely (only calls .Error()), so the
// non-*exec.ExitError type is fine — locks the contract that future
// changes must NOT type-assert on *exec.ExitError without updating tests.
type execError struct{ msg string }

func (e *execError) Error() string { return e.msg }

func TestHandleUpgradeRun_ConcurrentRunsSinglefligted(t *testing.T) {
	// Two parallel POSTs to /upgrade/run must NOT both spawn npm — the
	// first wins, the second gets an "already running" error. Without
	// this guard, two browser tabs hammering install simultaneously
	// could leave node_modules in a partial-extract state. Drives a
	// fake `runUpgradeCmdFn` that blocks on a channel so the second
	// arrival lands while the first is still in flight.
	pinDetectInstall(t, upgradeInstallPlan{
		Method:  "global",
		Args:    []string{"install", "-g", "wuphf@latest"},
		Command: "npm install -g wuphf@latest",
	})
	release := make(chan struct{})
	// Buffered so a (failing) double-entry can't deadlock the producer
	// goroutine — the test shouldn't observe that, but a bug in the
	// single-flight gate would otherwise hang here instead of failing.
	entered := make(chan struct{}, 2)
	var runCalls atomic.Int32
	pinRunCmd(t, func(_ context.Context, _ upgradeInstallPlan) ([]byte, bool, error) {
		// Only the first invocation parks on `release`. If the
		// single-flight gate regresses and a second request reaches
		// runUpgradeCmdFn, return immediately rather than parking —
		// otherwise the test deadlocks on `<-results` and CI hangs
		// instead of reporting the bug.
		if runCalls.Add(1) == 1 {
			entered <- struct{}{}
			<-release
			return []byte("done"), false, nil
		}
		entered <- struct{}{}
		return []byte("unexpected second spawn"), false, nil
	})

	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeRun))
	defer srv.Close()

	type result struct {
		body upgradeRunResult
		err  error
	}
	results := make(chan result, 2)
	send := func() {
		req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			results <- result{err: err}
			return
		}
		defer resp.Body.Close()
		var body upgradeRunResult
		_ = json.NewDecoder(resp.Body).Decode(&body)
		results <- result{body: body}
	}

	go send()
	// Wait for the leader to reach runUpgradeCmdFn (where it blocks on
	// `release`) before firing the second request. The fake handler
	// signals on `entered` as its first action. If the first send
	// errored before reaching the handler, we'd otherwise spin forever
	// — fail fast with an explicit timeout instead.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatalf("leader never reached runUpgradeCmdFn within 5s")
	}
	go send()

	// Collect the loser first — it returns immediately. The leader
	// stays blocked until we close `release`.
	first := <-results
	if first.err != nil {
		t.Fatalf("loser: %v", first.err)
	}
	if first.body.OK {
		t.Errorf("loser must be ok=false (got %+v)", first.body)
	}
	if !strings.Contains(first.body.Error, "already running") {
		t.Errorf("loser error should mention 'already running', got %q", first.body.Error)
	}
	close(release) // unblock the leader
	second := <-results
	if second.err != nil {
		t.Fatalf("leader: %v", second.err)
	}
	if !second.body.OK {
		t.Errorf("leader must be ok=true (got %+v)", second.body)
	}
}

func TestHandleUpgradeCheck_IncludesInstallMethod(t *testing.T) {
	// The chip's text is set from /upgrade-check's install_command so
	// the click target's label matches what /upgrade/run would actually
	// execute. This test pins detection to "local" and asserts the
	// served JSON forwards both fields. Without this, the chip silently
	// reverts to the hard-coded `npm install -g …` fallback even when
	// the broker would run the local-install variant.
	resetUpgradeCaches(t)
	pinDetectInstall(t, upgradeInstallPlan{
		Method:     "local",
		Command:    "npm install wuphf@latest",
		WorkingDir: "/workspace/some-app",
	})
	// Pre-seed upgradecheck cache with a happy result so the handler
	// doesn't try to hit npm during the test.
	upgradeCheckCache.Store(&upgradeCheckCacheEntry{
		res: upgradecheck.Result{
			Current:          "0.83.7",
			Latest:           "0.83.10",
			UpgradeAvailable: true,
			UpgradeCommand:   "npm install -g wuphf@latest",
		},
		storeAt: time.Now(),
	})

	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeCheck))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Current        string `json:"current"`
		Latest         string `json:"latest"`
		InstallMethod  string `json:"install_method"`
		InstallCommand string `json:"install_command"`
		UpgradeCommand string `json:"upgrade_command"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.InstallMethod != "local" {
		t.Errorf("install_method=%q want local", body.InstallMethod)
	}
	if body.InstallCommand != "npm install wuphf@latest" {
		t.Errorf("install_command=%q want local-install variant", body.InstallCommand)
	}
	// upgrade_command stays as the canonical doc-string variant —
	// install_command is the per-host truthful one. They MAY differ.
	if body.UpgradeCommand != "npm install -g wuphf@latest" {
		t.Errorf("upgrade_command should keep its canonical value, got %q", body.UpgradeCommand)
	}
}

func TestPkgDeclaresWuphf(t *testing.T) {
	cases := []struct {
		name string
		json string
		want bool
	}{
		{
			name: "declared in dependencies",
			json: `{"name":"app","dependencies":{"wuphf":"^0.83.0","react":"19"}}`,
			want: true,
		},
		{
			name: "declared in devDependencies",
			json: `{"name":"app","devDependencies":{"wuphf":"^0.83.0"}}`,
			want: true,
		},
		{
			name: "absent — must NOT misfire as local install (the $HOME risk)",
			json: `{"name":"app","dependencies":{"react":"19"}}`,
			want: false,
		},
		{
			name: "peerDependencies do NOT count (npm install <pkg>@latest only updates real deps)",
			json: `{"name":"app","peerDependencies":{"wuphf":"^0.83.0"}}`,
			want: false,
		},
		{
			name: "malformed package.json — refuse rather than crash",
			json: `{not valid json`,
			want: false,
		},
		{
			name: "empty object",
			json: `{}`,
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pkgDeclaresWuphf([]byte(c.json)); got != c.want {
				t.Errorf("pkgDeclaresWuphf(%q)=%v want %v", c.json, got, c.want)
			}
		})
	}
}

func TestTailBuffer(t *testing.T) {
	t.Run("under cap retains everything, no truncation", func(t *testing.T) {
		buf := newTailBuffer(64)
		_, _ = buf.Write([]byte("hello"))
		_, _ = buf.Write([]byte(" world"))
		if got := string(buf.Bytes()); got != "hello world" {
			t.Errorf("Bytes()=%q want %q", got, "hello world")
		}
		if buf.Truncated() {
			t.Error("Truncated()=true on under-cap input")
		}
	})

	t.Run("multi-write overflow keeps tail and flips truncated", func(t *testing.T) {
		buf := newTailBuffer(5)
		_, _ = buf.Write([]byte("abc"))
		_, _ = buf.Write([]byte("defg"))
		// Combined "abcdefg" (7 bytes) > cap (5). Keep last 5 = "cdefg".
		if got := string(buf.Bytes()); got != "cdefg" {
			t.Errorf("Bytes()=%q want %q", got, "cdefg")
		}
		if !buf.Truncated() {
			t.Error("Truncated()=false after dropping bytes")
		}
	})

	t.Run("single write exceeding cap keeps that tail", func(t *testing.T) {
		buf := newTailBuffer(4)
		_, _ = buf.Write([]byte("abcdefgh"))
		if got := string(buf.Bytes()); got != "efgh" {
			t.Errorf("Bytes()=%q want %q", got, "efgh")
		}
		if !buf.Truncated() {
			t.Error("Truncated()=false after dropping bytes from a single write")
		}
	})

	t.Run("single write exactly equal to cap is not flagged truncated", func(t *testing.T) {
		// Edge case: an oversize-equal write fills the buffer without
		// dropping any bytes. The tailBuffer must NOT advertise
		// truncation in that case, otherwise the wire surface would
		// claim truncation on a clean fill.
		buf := newTailBuffer(4)
		_, _ = buf.Write([]byte("abcd"))
		if got := string(buf.Bytes()); got != "abcd" {
			t.Errorf("Bytes()=%q want %q", got, "abcd")
		}
		if buf.Truncated() {
			t.Error("Truncated()=true on exact-cap fill")
		}
	})

	t.Run("memory cap is bounded — repeated overflow does not grow the underlying array", func(t *testing.T) {
		// Pour 100x the cap through the buffer in 1 KiB chunks and
		// assert the underlying array never exceeds maxBytes. Prevents
		// regressions where append growth pins old allocations.
		const maxBytes = 4 * 1024
		buf := newTailBuffer(maxBytes)
		chunk := make([]byte, 1024)
		for i := range chunk {
			chunk[i] = byte(i)
		}
		for i := 0; i < 100*maxBytes/len(chunk); i++ {
			_, _ = buf.Write(chunk)
		}
		if got := cap(buf.buf); got > maxBytes {
			t.Errorf("underlying cap=%d exceeds maxBytes=%d — memory not bounded", got, maxBytes)
		}
		if got := len(buf.Bytes()); got != maxBytes {
			t.Errorf("Bytes() length=%d want %d", got, maxBytes)
		}
	})
}

func TestWithTruncationSentinel(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty input", "", "…[truncated]…\n"},
		{"clean leading byte", "fghij", "…[truncated]…\nfghij"},
		{
			// "é" is 0xC3 0xA9. If the bounded write cut between the
			// leading byte and its continuation, the captured tail
			// would start with 0xA9 (a stray continuation byte). The
			// sentinel helper must skip past that so the result is
			// valid UTF-8 and the JSON encoder doesn't substitute
			// U+FFFD on the client.
			name: "skips orphan continuation byte at start",
			in:   string([]byte{0xA9, 'a', 'b'}),
			want: "…[truncated]…\nab",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := withTruncationSentinel(c.in)
			if got != c.want {
				t.Errorf("withTruncationSentinel(%q)=%q want %q", c.in, got, c.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("output is not valid UTF-8: %q", got)
			}
		})
	}
}

func TestDetectLocalInstall(t *testing.T) {
	// Drives detectLocalInstall against an isolated tmpdir layout so
	// the walk logic is exercised without depending on a real `npm`,
	// the developer's actual cwd, or their real $HOME. Exercises the
	// monorepo-walk fix (don't bail at the first non-declaring
	// ancestor) plus the safety stops ($HOME, declared-but-not-
	// installed, no package.json at all).
	writeFile := func(t *testing.T, path, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	declaresWuphf := `{"dependencies":{"wuphf":"^0.83.0"}}`
	noWuphf := `{"name":"sub"}`
	wuphfPkg := `{"name":"wuphf","version":"0.83.0"}`

	t.Run("monorepo: subpackage doesn't declare, root does — walks past sub", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "package.json"), declaresWuphf)
		writeFile(t, filepath.Join(root, "node_modules", "wuphf", "package.json"), wuphfPkg)
		writeFile(t, filepath.Join(root, "packages", "sub", "package.json"), noWuphf)
		got := detectLocalInstall(filepath.Join(root, "packages", "sub"), "")
		if got.Method != "local" {
			t.Fatalf("Method=%q want local", got.Method)
		}
		if got.WorkingDir != root {
			// Crucial: root, not the sub-package. A regression that
			// returned `packages/sub` would silently run `npm install`
			// in the wrong directory.
			t.Errorf("WorkingDir=%q want %q", got.WorkingDir, root)
		}
	})

	t.Run("declares + materialised wins immediately — does not keep walking", func(t *testing.T) {
		// If the cwd's package.json BOTH declares wuphf AND has it
		// installed, we accept it — even if a parent project also
		// declares wuphf. The nearest match is the user's intent.
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "package.json"), declaresWuphf)
		writeFile(t, filepath.Join(root, "node_modules", "wuphf", "package.json"), wuphfPkg)
		sub := filepath.Join(root, "packages", "sub")
		writeFile(t, filepath.Join(sub, "package.json"), declaresWuphf)
		writeFile(t, filepath.Join(sub, "node_modules", "wuphf", "package.json"), wuphfPkg)
		got := detectLocalInstall(sub, "")
		if got.Method != "local" {
			t.Fatalf("Method=%q want local", got.Method)
		}
		if got.WorkingDir != sub {
			t.Errorf("WorkingDir=%q want %q (nearest match should win)", got.WorkingDir, sub)
		}
	})

	t.Run("declared but not installed — bails to unknown without walking past", func(t *testing.T) {
		// Fresh-clone case: package.json declares wuphf but
		// node_modules/wuphf isn't materialised yet. Walking past to
		// a parent project's wuphf would silently upgrade the WRONG
		// project. Expect "unknown" so the click-to-run UI surfaces
		// its explicit fallback copy.
		root := t.TempDir()
		// Parent has a fully-installed wuphf — tempting to walk to
		// it, but we must not, because the sub-package's intent is
		// clear: install ITS wuphf.
		writeFile(t, filepath.Join(root, "package.json"), declaresWuphf)
		writeFile(t, filepath.Join(root, "node_modules", "wuphf", "package.json"), wuphfPkg)
		sub := filepath.Join(root, "packages", "sub")
		writeFile(t, filepath.Join(sub, "package.json"), declaresWuphf)
		got := detectLocalInstall(sub, "")
		if got.Method != "unknown" {
			t.Errorf("Method=%q want unknown (declared-but-not-installed must not walk past)", got.Method)
		}
	})

	t.Run("no ancestor declares wuphf — unknown", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "package.json"), noWuphf)
		writeFile(t, filepath.Join(root, "packages", "sub", "package.json"), noWuphf)
		got := detectLocalInstall(filepath.Join(root, "packages", "sub"), "")
		if got.Method != "unknown" {
			t.Errorf("Method=%q want unknown (no declaring ancestor)", got.Method)
		}
	})

	t.Run("$HOME stops the walk before inspecting its package.json", func(t *testing.T) {
		// Even if a stray ~/package.json declared wuphf and a phantom
		// ~/node_modules/wuphf existed (some exotic Volta/nvm setup),
		// running `npm install wuphf@latest` in $HOME would mutate
		// the user's home directory. The $HOME guard must stop the
		// walk BEFORE the package.json check.
		home := t.TempDir()
		writeFile(t, filepath.Join(home, "package.json"), declaresWuphf)
		writeFile(t, filepath.Join(home, "node_modules", "wuphf", "package.json"), wuphfPkg)
		sub := filepath.Join(home, "project", "sub")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		got := detectLocalInstall(sub, home)
		if got.Method != "unknown" {
			t.Errorf("Method=%q want unknown ($HOME guard must fire)", got.Method)
		}
	})

	t.Run("filesystem-root stop terminates a deep walk cleanly", func(t *testing.T) {
		// No package.json anywhere up the chain. The walk must reach
		// the filesystem root (parent == dir) and bail without
		// looping forever. Use a tmpdir as cwd; no $HOME hint so the
		// only stop is the root.
		got := detectLocalInstall(t.TempDir(), "")
		if got.Method != "unknown" {
			t.Errorf("Method=%q want unknown (no package.json on path to root)", got.Method)
		}
	})
}

func TestHandleUpgradeRun_BoundedOutputSurfacesTruncationSentinel(t *testing.T) {
	// End-to-end: when runUpgradeCmdFn reports truncated=true, the
	// handler must surface a sentinel-prefixed `output` so the banner
	// can render the "earlier output dropped" indicator. This locks
	// the wire contract — without it, a verbose npm install would
	// silently appear as a clean log even though bytes were thrown
	// away during capture.
	pinDetectInstall(t, upgradeInstallPlan{
		Method:  "global",
		Args:    []string{"install", "-g", "wuphf@latest"},
		Command: "npm install -g wuphf@latest",
	})
	pinRunCmd(t, func(_ context.Context, _ upgradeInstallPlan) ([]byte, bool, error) {
		return []byte("…final lines…\n+ wuphf@99.0.0\n"), true, nil
	})
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeRun))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	var body upgradeRunResult
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(body.Output, "…[truncated]…\n") {
		t.Errorf("output should start with truncation sentinel, got %q", body.Output)
	}
	if !strings.Contains(body.Output, "wuphf@99.0.0") {
		t.Errorf("output should still surface the trailing tail, got %q", body.Output)
	}
}

func TestHandleUpgradeChangelog_ResponseShapeMatchesBannerExpectations(t *testing.T) {
	// Lock in the JSON wire shape the React banner reads — without
	// JSON struct tags on CommitEntry, Go's default capitalised
	// encoding silently broke the banner (every entry rendered as
	// `{type: "other"}` because c.type was undefined). This test
	// short-circuits the upstream GitHub call by pre-seeding the
	// changelog cache, then asserts the served JSON uses lowercase
	// keys exactly matching what UpgradeBanner.tsx reads.
	resetUpgradeCaches(t)
	// Use the production key helper so the test stays in sync if the
	// broker's encoding ever changes.
	upgradeChangelog.Store(upgradeChangelogKey("v0.1.0", "v0.1.1"), &upgradeChangelogCacheEntry{
		entries: []upgradecheck.CommitEntry{{
			Type: "feat", Scope: "wiki", Description: "x",
			PR: "1", SHA: "abc", Breaking: true,
		}},
		storeAt: time.Now(),
	})
	b := newTestBroker(t)
	srv := httptest.NewServer(b.requireAuth(b.handleUpgradeChangelog))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"?from=v0.1.0&to=v0.1.1", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, body=%s", resp.StatusCode, string(body))
	}

	// Decode into a struct using the LOWERCASE field names the React
	// banner reads. If JSON tags ever drift back to defaults, all
	// fields decode to zero values and the test fails loudly.
	var payload struct {
		Commits []struct {
			Type        string `json:"type"`
			Scope       string `json:"scope"`
			Description string `json:"description"`
			PR          string `json:"pr"`
			SHA         string `json:"sha"`
			Breaking    bool   `json:"breaking"`
		} `json:"commits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(payload.Commits))
	}
	c := payload.Commits[0]
	if c.Type != "feat" || c.Scope != "wiki" || c.Description != "x" ||
		c.PR != "1" || c.SHA != "abc" || !c.Breaking {
		t.Errorf("wire shape drifted: %+v", c)
	}
}
