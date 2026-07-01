package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// waitForPending polls the registry until the app has n pending asks, so a test
// can synchronise with a step that paused in another goroutine without a sleep.
func waitForPending(t *testing.T, appID string, n int) []browserApproval {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p := browserApprovals.pendingFor(appID); len(p) == n {
			return p
		}
		// Yield to the paused-step goroutine instead of sleeping — the pending ask
		// appears within microseconds, and this keeps the helper free of a
		// time.Sleep (which the no-sleeps-in-tests lint forbids).
		runtime.Gosched()
	}
	t.Fatalf("timed out waiting for %d pending approvals for %s", n, appID)
	return nil
}

func TestBrowserApprovalResolveApprove(t *testing.T) {
	const app = "app_resolve"
	got := make(chan bool, 1)
	go func() { got <- browserApprovals.ask(context.Background(), app, browserApprovalControl, "do it") }()

	p := waitForPending(t, app, 1)
	if p[0].Kind != browserApprovalControl || p[0].Goal != "do it" || p[0].AppID != app {
		t.Fatalf("unexpected pending: %#v", p[0])
	}
	if !browserApprovals.resolve(p[0].ID, true) {
		t.Fatal("resolve should succeed for a live ask")
	}
	if allow := <-got; !allow {
		t.Fatal("ask should return true after approve")
	}
	if len(browserApprovals.pendingFor(app)) != 0 {
		t.Fatal("resolved ask should no longer be pending")
	}
}

func TestBrowserApprovalDenyAndUnknown(t *testing.T) {
	const app = "app_deny"
	got := make(chan bool, 1)
	go func() { got <- browserApprovals.ask(context.Background(), app, browserApprovalSend, "Send it") }()
	p := waitForPending(t, app, 1)
	browserApprovals.resolve(p[0].ID, false)
	if allow := <-got; allow {
		t.Fatal("ask should return false after deny")
	}
	// A second resolve of the same (now gone) id is a no-op, not a panic.
	if browserApprovals.resolve(p[0].ID, true) {
		t.Fatal("resolving an unknown id should return false")
	}
}

func TestBrowserApprovalContextCancelDenies(t *testing.T) {
	const app = "app_cancel"
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan bool, 1)
	go func() { got <- browserApprovals.ask(ctx, app, browserApprovalControl, "x") }()
	waitForPending(t, app, 1)
	cancel() // client disconnect / Stop → default deny
	if allow := <-got; allow {
		t.Fatal("cancelled ask must deny")
	}
}

func TestBrowserPendingAndApproveEndpoints(t *testing.T) {
	const app = "app_http"
	b := &Broker{}
	got := make(chan bool, 1)
	go func() {
		got <- browserApprovals.ask(context.Background(), app, browserApprovalControl, "email the digest")
	}()
	p := waitForPending(t, app, 1)

	// GET pending surfaces the ask for the app's chat.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/operator/apps/"+app+"/workflow/browser/pending", nil)
	b.handleOperatorAppWorkflow(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pending status = %d", rec.Code)
	}
	var pendingResp struct {
		Pending []browserApproval `json:"pending"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pendingResp); err != nil {
		t.Fatalf("decode pending: %v", err)
	}
	if len(pendingResp.Pending) != 1 || pendingResp.Pending[0].Goal != "email the digest" {
		t.Fatalf("pending body = %s", rec.Body.String())
	}

	// POST approve resolves it → the paused step resumes with allow=true.
	rec = httptest.NewRecorder()
	body := strings.NewReader(`{"approval_id":"` + p[0].ID + `","decision":"approve"}`)
	req = httptest.NewRequest(http.MethodPost, "/operator/apps/"+app+"/workflow/browser/approve", body)
	b.handleOperatorAppWorkflow(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d body=%s", rec.Code, rec.Body.String())
	}
	if allow := <-got; !allow {
		t.Fatal("approve endpoint should resume the step with allow")
	}

	// Approving an unknown id is a 404 so the UI can drop a stale card.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/operator/apps/"+app+"/workflow/browser/approve",
		strings.NewReader(`{"approval_id":"deadbeef","decision":"approve"}`))
	b.handleOperatorAppWorkflow(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown approve status = %d", rec.Code)
	}
}

// TestBrowserApproveEndpointRejectsMalformedDecision proves a decision that is
// neither "approve" nor "deny" is a 400 and does NOT consume/resolve the pending
// ask: a malformed request must fail fast, not silently deny (and eat) the run.
func TestBrowserApproveEndpointRejectsMalformedDecision(t *testing.T) {
	const app = "app_malformed"
	b := &Broker{}
	got := make(chan bool, 1)
	go func() { got <- browserApprovals.ask(context.Background(), app, browserApprovalControl, "x") }()
	p := waitForPending(t, app, 1)

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"approval_id":"` + p[0].ID + `","decision":"maybe"}`)
	req := httptest.NewRequest(http.MethodPost, "/operator/apps/"+app+"/workflow/browser/approve", body)
	b.handleOperatorAppWorkflow(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed decision should be 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	// The ask must still be pending — a malformed request cannot consume it.
	if len(browserApprovals.pendingFor(app)) != 1 {
		t.Fatal("a malformed decision must leave the ask pending")
	}
	// A valid deny now resolves it (and drains the goroutine).
	browserApprovals.resolve(p[0].ID, false)
	if allow := <-got; allow {
		t.Fatal("ask should deny after the follow-up deny")
	}
}

// TestBrowserApprovalResolveForAppScopes proves an approval raised by one app
// cannot be resolved through another app's endpoint: resolveForApp refuses a
// mismatched app id (leaving the ask pending) and only the owning app resolves.
func TestBrowserApprovalResolveForAppScopes(t *testing.T) {
	const app = "app_owner"
	got := make(chan bool, 1)
	go func() { got <- browserApprovals.ask(context.Background(), app, browserApprovalControl, "drive it") }()
	p := waitForPending(t, app, 1)

	// A different app must not resolve this app's approval id.
	if browserApprovals.resolveForApp("app_intruder", p[0].ID, true) {
		t.Fatal("resolveForApp must refuse an id owned by a different app")
	}
	if len(browserApprovals.pendingFor(app)) != 1 {
		t.Fatal("a cross-app resolve attempt must leave the ask pending")
	}
	// The owning app resolves it.
	if !browserApprovals.resolveForApp(app, p[0].ID, true) {
		t.Fatal("the owning app should resolve its own approval")
	}
	if allow := <-got; !allow {
		t.Fatal("ask should return true after the owning app approves")
	}
}

// TestBrowserApproveEndpointRejectsForeignApp proves the HTTP approve endpoint is
// app-scoped: posting a valid approval id to a DIFFERENT app's URL is a 404 and
// does not resume the paused step.
func TestBrowserApproveEndpointRejectsForeignApp(t *testing.T) {
	const app = "app_real"
	b := &Broker{}
	go func() { _ = browserApprovals.ask(context.Background(), app, browserApprovalControl, "x") }()
	p := waitForPending(t, app, 1)

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"approval_id":"` + p[0].ID + `","decision":"approve"}`)
	req := httptest.NewRequest(http.MethodPost, "/operator/apps/app_other/workflow/browser/approve", body)
	b.handleOperatorAppWorkflow(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign-app approve should be 404, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(browserApprovals.pendingFor(app)) != 1 {
		t.Fatal("a foreign-app approve must leave the real app's ask pending")
	}
	// Clean up the still-pending ask so it does not leak into other tests.
	browserApprovals.resolve(p[0].ID, false)
}

// TestBrowserStepHeadlessSkips proves a run with no operator app id (scheduler/
// cron/headless) never seizes the browser — it skips without asking or spawning.
func TestBrowserStepHeadlessSkips(t *testing.T) {
	out, err := runBrowserStepViaCua(context.Background(), "email the digest")
	if err != nil {
		t.Fatalf("headless browser step: %v", err)
	}
	if out["skipped"] != "browser control needs operator approval" {
		t.Fatalf("headless step should skip without asking, got %#v", out)
	}
}

// TestBrowserStepControlDenyDoesNotDrive proves the in-chat pause gates driving:
// a denied control ask returns skipped and never reaches the runner (no cua).
func TestBrowserStepControlDenyDoesNotDrive(t *testing.T) {
	const app = "app_gate"
	ctx := context.WithValue(context.Background(), browserStepAppIDKey, app)
	res := make(chan map[string]any, 1)
	go func() {
		out, _ := runBrowserStepViaCua(ctx, "email the digest")
		res <- out
	}()
	p := waitForPending(t, app, 1)
	if p[0].Kind != browserApprovalControl {
		t.Fatalf("first ask should be control, got %q", p[0].Kind)
	}
	browserApprovals.resolve(p[0].ID, false) // "not now"
	out := <-res
	if out["skipped"] != "browser control not approved" {
		t.Fatalf("denied control should skip, got %#v", out)
	}
}

// writeFakeCuaRunner writes a shell script that stands in for the Python cua
// runner and points the step at it (WUPHF_CUA_PYTHON=sh). The key envs are set so
// the availability gate passes and the step actually spawns the fake.
func writeFakeCuaRunner(t *testing.T, script string) {
	t.Helper()
	runner := filepath.Join(t.TempDir(), "fake_cua.sh")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WUPHF_OPENAI_API_KEY", "test-key")
	t.Setenv("WUPHF_CUA_PYTHON", "sh")
	t.Setenv("WUPHF_CUA_RUNNER", runner)
}

// resolveNextPending waits for the app's next single pending ask and resolves it.
func resolveNextPending(t *testing.T, app string, allow bool) browserApproval {
	t.Helper()
	p := waitForPending(t, app, 1)
	browserApprovals.resolve(p[0].ID, allow)
	return p[0]
}

// TestBrowserStepDrivesAndAggregates exercises the full happy path through the
// fake runner: control approval passes the gate, an in-step send is gated in chat
// (its decision is forwarded to the runner's stdin), and the action/done events
// are aggregated into the result.
func TestBrowserStepDrivesAndAggregates(t *testing.T) {
	// Emits an action, then requests a send and echoes back the stdin decision as
	// the run result, so the test can prove the send-gate forwarded "approve".
	writeFakeCuaRunner(t, strings.Join([]string{
		`echo '{"type":"action","label":"Opened portal"}'`,
		`echo '{"type":"approval_request","label":"Submit refund"}'`,
		`read decision`,
		`echo "{\"type\":\"done\",\"result\":\"$decision\"}"`,
	}, "\n")+"\n")

	const app = "app_drive"
	ctx := context.WithValue(context.Background(), browserStepAppIDKey, app)
	res := make(chan map[string]any, 1)
	go func() {
		out, _ := runBrowserStepViaCua(ctx, "submit the refund")
		res <- out
	}()

	if a := resolveNextPending(t, app, true); a.Kind != browserApprovalControl {
		t.Fatalf("first ask should be control, got %q", a.Kind)
	}
	if a := resolveNextPending(t, app, true); a.Kind != browserApprovalSend {
		t.Fatalf("second ask should be send, got %q", a.Kind)
	}

	out := <-res
	if out["error"] != nil {
		t.Fatalf("clean run should carry no error, got %#v", out["error"])
	}
	if out["actions_count"] != 1 || out["result"] != "approve" {
		t.Fatalf("expected one action and the forwarded send decision, got %#v", out)
	}
	if acts, _ := out["actions"].([]string); len(acts) != 1 || acts[0] != "Opened portal" {
		t.Fatalf("actions not aggregated, got %#v", out["actions"])
	}
}

// TestBrowserStepSurfacesRunnerCrash proves an abnormal runner exit that emits no
// "done"/"error" event is surfaced as an error in the result rather than looking
// like a clean, empty success.
func TestBrowserStepSurfacesRunnerCrash(t *testing.T) {
	writeFakeCuaRunner(t, "echo '{\"type\":\"action\",\"label\":\"Started\"}'\nexit 3\n")

	const app = "app_crash"
	ctx := context.WithValue(context.Background(), browserStepAppIDKey, app)
	res := make(chan map[string]any, 1)
	go func() {
		out, _ := runBrowserStepViaCua(ctx, "do the thing")
		res <- out
	}()
	resolveNextPending(t, app, true) // approve control

	out := <-res
	msg, _ := out["error"].(string)
	if !strings.Contains(msg, "runner exited abnormally") {
		t.Fatalf("abnormal exit should surface an error, got %#v", out)
	}
}

// TestBrowserStepControlApprovePassesGate proves an approved control ask passes
// the gate; with no key/runner on the test host it then degrades to the
// unavailable marker (distinct from the not-approved skip), so the gate order is
// verifiable without a real browser.
func TestBrowserStepControlApprovePassesGate(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("WUPHF_OPENAI_API_KEY", "")
	t.Setenv("WUPHF_CUA_RUNNER", "/nonexistent/cua_exec.py")
	const app = "app_pass"
	ctx := context.WithValue(context.Background(), browserStepAppIDKey, app)
	res := make(chan map[string]any, 1)
	go func() {
		out, _ := runBrowserStepViaCua(ctx, "email the digest")
		res <- out
	}()
	p := waitForPending(t, app, 1)
	browserApprovals.resolve(p[0].ID, true)
	out := <-res
	if out["skipped"] != "browser execution unavailable on this host" {
		t.Fatalf("approved control should pass the gate then hit unavailable, got %#v", out)
	}
}
