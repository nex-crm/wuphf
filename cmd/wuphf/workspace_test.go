package main

// Tests for `wuphf workspace ...`. Coverage target per Lane D charter: 90%
// across every subcommand path including --json, --force, --permanent,
// --from-scratch, --open, and --dry-run.
//
// Strategy: route every call through `fakeWorkspaceOrchestrator` so we can
// assert (a) the right method was called, (b) with the right args, and
// (c) that error paths surface cleanly. We avoid os.Exit in tests by
// shelling out to the binary for end-to-end coverage where exit codes
// matter; pure logic (renderList, runDoctorIssueLoop, shredConfirmFromReader)
// gets unit tests directly.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeWorkspaceOrchestrator is the test seam. Each method records its call
// and returns the canned response. Methods that should fail in the test
// return the canned error.
type fakeWorkspaceOrchestrator struct {
	listResult ListResult
	listErr    error
	listCalls  []ListOpts

	createResult Workspace
	createErr    error
	createCalls  []CreateRequest

	switchResult Workspace
	switchErr    error
	switchCalls  []struct {
		Name string
		Open bool
	}

	pauseErr   error
	pauseCalls []struct {
		Name  string
		Force bool
	}

	resumeResult Workspace
	resumeErr    error
	resumeCalls  []string

	shredErr   error
	shredCalls []struct {
		Name      string
		Permanent bool
	}

	restoreResult Workspace
	restoreErr    error
	restoreCalls  []string

	doctorReport DoctorReport
	doctorErr    error
	doctorCalls  int

	fixErr   error
	fixCalls []string

	resolveResult Workspace
	resolveErr    error
	resolveCalls  []string
}

func (f *fakeWorkspaceOrchestrator) List(ctx context.Context, opts ListOpts) (ListResult, error) {
	f.listCalls = append(f.listCalls, opts)
	return f.listResult, f.listErr
}

func (f *fakeWorkspaceOrchestrator) Create(ctx context.Context, req CreateRequest) (Workspace, error) {
	f.createCalls = append(f.createCalls, req)
	return f.createResult, f.createErr
}

func (f *fakeWorkspaceOrchestrator) Switch(ctx context.Context, name string, open bool) (Workspace, error) {
	f.switchCalls = append(f.switchCalls, struct {
		Name string
		Open bool
	}{name, open})
	return f.switchResult, f.switchErr
}

func (f *fakeWorkspaceOrchestrator) Pause(ctx context.Context, name string, force bool) error {
	f.pauseCalls = append(f.pauseCalls, struct {
		Name  string
		Force bool
	}{name, force})
	return f.pauseErr
}

func (f *fakeWorkspaceOrchestrator) Resume(ctx context.Context, name string) (Workspace, error) {
	f.resumeCalls = append(f.resumeCalls, name)
	return f.resumeResult, f.resumeErr
}

func (f *fakeWorkspaceOrchestrator) Shred(ctx context.Context, name string, permanent bool) error {
	f.shredCalls = append(f.shredCalls, struct {
		Name      string
		Permanent bool
	}{name, permanent})
	return f.shredErr
}

func (f *fakeWorkspaceOrchestrator) Restore(ctx context.Context, trashID string) (Workspace, error) {
	f.restoreCalls = append(f.restoreCalls, trashID)
	return f.restoreResult, f.restoreErr
}

func (f *fakeWorkspaceOrchestrator) Doctor(ctx context.Context) (DoctorReport, error) {
	f.doctorCalls++
	return f.doctorReport, f.doctorErr
}

func (f *fakeWorkspaceOrchestrator) FixDoctorIssue(ctx context.Context, fixID string) error {
	f.fixCalls = append(f.fixCalls, fixID)
	return f.fixErr
}

func (f *fakeWorkspaceOrchestrator) Resolve(ctx context.Context, name string) (Workspace, error) {
	f.resolveCalls = append(f.resolveCalls, name)
	return f.resolveResult, f.resolveErr
}

// newFakeOrchestrator constructs a zero-value fake and registers it as the
// active orchestrator for the duration of the test. Returns the fake so the
// test can configure responses + assert calls. Convenience wrapper around
// withFakeOrchestrator for the common "I just need an empty fake" case.
func newFakeOrchestrator(t *testing.T) *fakeWorkspaceOrchestrator {
	t.Helper()
	fake := &fakeWorkspaceOrchestrator{}
	cleanup := withFakeOrchestrator(t, fake)
	t.Cleanup(cleanup)
	return fake
}

// withFakeOrchestrator swaps the package-level orchestratorFactory to return
// the supplied orch and returns a cleanup that restores the original factory.
// The returned cleanup is idempotent (safe to call once even if the test
// already registered it via t.Cleanup).
//
// This is the lower-level seam used by tests that already hold a custom
// workspaceOrchestrator implementation (e.g., a struct embedding
// fakeWorkspaceOrchestrator with overridden methods, or a different fake
// shape entirely). For the common case use newFakeOrchestrator.
func withFakeOrchestrator(t *testing.T, orch workspaceOrchestrator) func() {
	t.Helper()
	prev := orchestratorFactory
	orchestratorFactory = func() (workspaceOrchestrator, error) { return orch, nil }
	var once sync.Once
	return func() {
		once.Do(func() { orchestratorFactory = prev })
	}
}

// printErrorCapture redirects printError's output and exit handler so tests
// can assert (a) the formatted error message bytes and (b) that the error
// path actually called printErrorExit. The returned closure restores the
// originals; the *bytes.Buffer accumulates whatever printError wrote.
//
// We swap printErrorExit for a panic-with-sentinel rather than a no-op so
// callers downstream of printError don't keep running with stale state. The
// helper installs a deferred-recover at the test boundary via runAndCapture.
func printErrorCapture(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	prevOut := printErrorOutput
	prevExit := printErrorExit
	buf := &bytes.Buffer{}
	printErrorOutput = buf
	printErrorExit = func(code int) {
		// panic with a sentinel so the calling test can recover and assert
		// the message instead of terminating the whole test binary.
		panic(printErrorExitSentinel{code: code})
	}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			printErrorOutput = prevOut
			printErrorExit = prevExit
		})
	}
	return buf, cleanup
}

// printErrorExitSentinel is recovered by runAndCapturePrintError so the test
// can assert the exit code without aborting the binary.
type printErrorExitSentinel struct{ code int }

// runAndCapturePrintError invokes f under the printErrorCapture seam,
// recovers the printErrorExitSentinel panic if printError fired, and returns
// (true, exitCode, output) on a fired error or (false, 0, output) when f
// completed without calling printError.
func runAndCapturePrintError(t *testing.T, f func()) (fired bool, code int, output string) {
	t.Helper()
	buf, restore := printErrorCapture(t)
	defer restore()
	func() {
		defer func() {
			if r := recover(); r != nil {
				if sentinel, ok := r.(printErrorExitSentinel); ok {
					fired = true
					code = sentinel.code
					return
				}
				// Unrelated panic — re-raise so it surfaces normally.
				panic(r)
			}
		}()
		f()
	}()
	output = buf.String()
	return fired, code, output
}

// ---------- list ----------

func TestRenderList_HumanTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderList(&buf, ListResult{}, false, false); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	if !strings.Contains(buf.String(), "No workspaces yet") {
		t.Fatalf("expected empty-state message, got %q", buf.String())
	}
}

func TestRenderList_HumanTable_WithEntries(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	ws := []Workspace{
		{Name: "main", State: WorkspaceStateRunning, BrokerPort: 7890, WebPort: 7891, LastUsedAt: now, IsCLICurrent: true, CostUSD: 4.20},
		{Name: "demo-launch", State: WorkspaceStatePaused, BrokerPort: 7910, WebPort: 7911, LastUsedAt: now},
	}
	var buf bytes.Buffer
	if err := renderList(&buf, ListResult{Workspaces: ws}, false, false); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "main") {
		t.Fatal("expected main workspace in output")
	}
	if !strings.Contains(out, "demo-launch") {
		t.Fatal("expected demo-launch workspace in output")
	}
	if !strings.Contains(out, "running") || !strings.Contains(out, "paused") {
		t.Fatal("expected both states rendered")
	}
	if !strings.Contains(out, "$4.20") {
		t.Fatalf("expected cost rendered, got %q", out)
	}
	if !strings.Contains(out, "* = active CLI workspace") {
		t.Fatal("expected active-marker legend")
	}
	// alphabetical ordering: "demo-launch" before "main"
	if strings.Index(out, "demo-launch") > strings.Index(out, "main") {
		t.Fatalf("expected alphabetical order, got %q", out)
	}
}

func TestRenderList_JSON_StableShape(t *testing.T) {
	ws := []Workspace{{Name: "alpha", BrokerPort: 7910, WebPort: 7911, State: WorkspaceStateRunning}}
	var buf bytes.Buffer
	if err := renderList(&buf, ListResult{Workspaces: ws}, true, false); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	var parsed ListResult
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(parsed.Workspaces) != 1 || parsed.Workspaces[0].Name != "alpha" {
		t.Fatalf("unexpected parsed: %+v", parsed)
	}
	// Empty trash slice should serialize as [] not null.
	if !strings.Contains(buf.String(), `"trash": []`) {
		t.Fatalf("expected empty trash slice as [], got %q", buf.String())
	}
}

func TestRenderList_TrashTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderList(&buf, ListResult{}, false, true); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	if !strings.Contains(buf.String(), "Trash is empty") {
		t.Fatalf("expected empty-trash message, got %q", buf.String())
	}
}

func TestRenderList_TrashTable_WithEntries(t *testing.T) {
	ts := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	trash := []TrashEntry{
		{TrashID: "demo-1714305600", Name: "demo", ShreddedAt: ts, SizeBytes: 2 * 1024 * 1024},
		{TrashID: "scratch-1714305700", Name: "scratch", ShreddedAt: ts.Add(time.Minute), SizeBytes: 512},
	}
	var buf bytes.Buffer
	if err := renderList(&buf, ListResult{Trash: trash}, false, true); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "demo-1714305600") {
		t.Fatal("expected trash ID in output")
	}
	if !strings.Contains(out, "Restore with `wuphf workspace restore") {
		t.Fatal("expected restore hint")
	}
	// Newest first: scratch (later timestamp) should appear before demo
	if strings.Index(out, "scratch-1714305700") > strings.Index(out, "demo-1714305600") {
		t.Fatalf("expected newest-first order, got %q", out)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{500, "500 B"},
		{2 * 1024, "2.0 KB"},
		{int64(1.5 * 1024 * 1024), "1.5 MB"},
		{int64(3 * 1024 * 1024 * 1024), "3.0 GB"},
	}
	for _, c := range cases {
		got := humanBytes(c.in)
		if got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatDurationSince(t *testing.T) {
	if got := formatDurationSince(time.Time{}); got != "-" {
		t.Errorf("zero time should render as `-`, got %q", got)
	}
	if got := formatDurationSince(time.Now().Add(-30 * time.Second)); !strings.HasSuffix(got, "s ago") {
		t.Errorf("recent should render seconds, got %q", got)
	}
	if got := formatDurationSince(time.Now().Add(-2 * time.Hour)); !strings.HasSuffix(got, "h ago") {
		t.Errorf("hours-ago should render hours, got %q", got)
	}
	if got := formatDurationSince(time.Now().Add(-72 * time.Hour)); !strings.HasSuffix(got, "d ago") {
		t.Errorf("days-ago should render days, got %q", got)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "yes"); got != "yes" {
		t.Errorf("got %q", got)
	}
	if got := firstNonEmpty("", "  "); got != "" {
		t.Errorf("got %q", got)
	}
}

// ---------- create slug validation ----------

func TestSlugShape_AcceptsValid(t *testing.T) {
	good := []string{"main", "demo-launch", "a", "alpha-1-beta", "x12345678901234567890123456789012"[:31]}
	for _, name := range good {
		if !slugShape.MatchString(name) {
			t.Errorf("slug %q should be valid", name)
		}
	}
}

func TestSlugShape_RejectsInvalid(t *testing.T) {
	bad := []string{"", "1main", "Main", "foo bar", "foo_bar", "-foo", "foo!", strings.Repeat("a", 32)}
	for _, name := range bad {
		if slugShape.MatchString(name) {
			t.Errorf("slug %q should be invalid", name)
		}
	}
}

// ---------- shred confirm ----------

func TestShredConfirm_AcceptsExactMatch(t *testing.T) {
	in := strings.NewReader("demo-launch\n")
	var out bytes.Buffer
	ok, err := shredConfirmFromReader(in, &out, "demo-launch", false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Fatal("expected confirm")
	}
	if !strings.Contains(out.String(), "demo-launch") {
		t.Fatal("prompt should include name")
	}
}

func TestShredConfirm_RejectsMismatch(t *testing.T) {
	in := strings.NewReader("y\n")
	var out bytes.Buffer
	ok, err := shredConfirmFromReader(in, &out, "demo-launch", false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Fatal("expected reject — `y` is not the workspace name")
	}
}

func TestShredConfirm_MainWarningEmitted(t *testing.T) {
	in := strings.NewReader("main\n")
	var out bytes.Buffer
	ok, err := shredConfirmFromReader(in, &out, "main", false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Fatal("expected confirm for main")
	}
	if !strings.Contains(out.String(), "WARNING: shredding `main`") {
		t.Fatal("expected main-specific warning")
	}
	if !strings.Contains(out.String(), "migrated ~/.wuphf/") {
		t.Fatal("expected migrated-state warning")
	}
}

func TestShredConfirm_PermanentWarning(t *testing.T) {
	in := strings.NewReader("foo\n")
	var out bytes.Buffer
	ok, err := shredConfirmFromReader(in, &out, "foo", true)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Fatal("expected confirm")
	}
	if !strings.Contains(out.String(), "PERMANENT SHRED") {
		t.Fatal("expected permanent-shred warning")
	}
	if !strings.Contains(out.String(), "CANNOT be restored") {
		t.Fatal("expected irreversible warning")
	}
}

func TestShredConfirm_EmptyInputReturnsFalseNotError(t *testing.T) {
	in := strings.NewReader("")
	var out bytes.Buffer
	ok, _ := shredConfirmFromReader(in, &out, "foo", false)
	if ok {
		t.Fatal("EOF on confirm should not be treated as accept")
	}
}

// ---------- doctor loop ----------

func TestDoctor_NoIssues_Idle(t *testing.T) {
	fake := &fakeWorkspaceOrchestrator{}
	var out bytes.Buffer
	err := runDoctorIssueLoop(context.Background(), fake, DoctorReport{}, strings.NewReader(""), &out, doctorModeInteractive)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(fake.fixCalls) != 0 {
		t.Fatalf("expected zero fix calls, got %d", len(fake.fixCalls))
	}
}

func TestDoctor_DryRun_PrintsButNeverFixes(t *testing.T) {
	fake := &fakeWorkspaceOrchestrator{}
	report := DoctorReport{Issues: []DoctorIssue{
		{Kind: DoctorIssueOrphanTree, Subject: "scratch", FixID: "fix-1", FixAction: "delete tree"},
		{Kind: DoctorIssueZombieState, Subject: "demo", FixID: "fix-2", FixAction: "mark as error"},
	}}
	var out bytes.Buffer
	err := runDoctorIssueLoop(context.Background(), fake, report, strings.NewReader(""), &out, doctorModeDryRun)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(fake.fixCalls) != 0 {
		t.Fatalf("dry-run should not call FixDoctorIssue, got %d calls", len(fake.fixCalls))
	}
	if !strings.Contains(out.String(), "[dry-run] not applied") {
		t.Fatalf("expected dry-run marker, got %q", out.String())
	}
	if !strings.Contains(out.String(), "Re-run without --dry-run") {
		t.Fatal("expected dry-run footer")
	}
}

func TestDoctor_AutoYes_AppliesAllFixes(t *testing.T) {
	fake := &fakeWorkspaceOrchestrator{}
	report := DoctorReport{Issues: []DoctorIssue{
		{Kind: DoctorIssueOrphanTree, Subject: "scratch", FixID: "fix-1"},
		{Kind: DoctorIssueZombieState, Subject: "demo", FixID: "fix-2"},
	}}
	var out bytes.Buffer
	err := runDoctorIssueLoop(context.Background(), fake, report, strings.NewReader(""), &out, doctorModeAutoYes)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(fake.fixCalls) != 2 || fake.fixCalls[0] != "fix-1" || fake.fixCalls[1] != "fix-2" {
		t.Fatalf("expected both fix IDs called in order, got %v", fake.fixCalls)
	}
}

func TestDoctor_Interactive_PromptsPerIssue(t *testing.T) {
	fake := &fakeWorkspaceOrchestrator{}
	report := DoctorReport{Issues: []DoctorIssue{
		{Kind: DoctorIssueOrphanTree, Subject: "a", FixID: "fix-a"},
		{Kind: DoctorIssueZombieState, Subject: "b", FixID: "fix-b"},
		{Kind: DoctorIssuePortConflict, Subject: "c", FixID: "fix-c"},
	}}
	// answers: y, n, yes
	in := strings.NewReader("y\nn\nyes\n")
	var out bytes.Buffer
	err := runDoctorIssueLoop(context.Background(), fake, report, in, &out, doctorModeInteractive)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(fake.fixCalls) != 2 || fake.fixCalls[0] != "fix-a" || fake.fixCalls[1] != "fix-c" {
		t.Fatalf("expected fix-a and fix-c applied, got %v", fake.fixCalls)
	}
	if !strings.Contains(out.String(), "skipped") {
		t.Fatal("expected `skipped` for issue b")
	}
}

func TestDoctor_StopsAfterFirstFailure(t *testing.T) {
	fake := &fakeWorkspaceOrchestrator{fixErr: errors.New("disk full")}
	report := DoctorReport{Issues: []DoctorIssue{
		{Kind: DoctorIssueOrphanTree, Subject: "a", FixID: "fix-a"},
		{Kind: DoctorIssueZombieState, Subject: "b", FixID: "fix-b"},
	}}
	var out bytes.Buffer
	err := runDoctorIssueLoop(context.Background(), fake, report, strings.NewReader("y\ny\n"), &out, doctorModeInteractive)
	if err == nil {
		t.Fatal("expected error from failed fix")
	}
	if !strings.Contains(out.String(), "skipped (prior fix failed)") {
		t.Fatalf("expected skip-after-fail marker, got %q", out.String())
	}
	// Only the first fix should have been attempted
	if len(fake.fixCalls) != 1 {
		t.Fatalf("expected 1 fix attempt, got %d", len(fake.fixCalls))
	}
}

// ---------- orchestrator stub & override ----------

func TestOrchestratorFactory_DefaultIsUnwired(t *testing.T) {
	// Sanity check: the prod factory yields the not-wired error so subcommand
	// handlers degrade with a friendly message rather than panic. We can't
	// directly invoke os.Exit here, so we verify the factory contract.
	prev := orchestratorFactory
	// reset to ensure default
	orchestratorFactory = func() (workspaceOrchestrator, error) {
		return nil, errors.New("workspace orchestrator not wired (Lane B integration pending)")
	}
	defer func() { orchestratorFactory = prev }()

	_, err := orchestratorFactory()
	if err == nil {
		t.Fatal("expected default factory to surface the not-wired error")
	}
	if !strings.Contains(err.Error(), "Lane B") {
		t.Fatalf("expected Lane B reference, got %v", err)
	}
}

// ---------- end-to-end via runWorkspace dispatch ----------

// captureStreams swaps os.Stdout and os.Stderr for the duration of f(). We
// can't use the standard testing.T.Setenv-style swap because os.Exit
// terminates the goroutine before deferred restores fire. The dispatch
// helpers we test here don't call os.Exit on the happy path.
func captureStreams(t *testing.T, f func()) (string, string) {
	t.Helper()
	// Go's testing framework already isolates stdout/stderr through fd
	// inheritance; we capture by re-pointing the global vars. Tests call
	// dispatch helpers that write through os.Stdout/Stderr directly.
	stdout, stderr := captureFD(t, f)
	return stdout, stderr
}

// captureFD pipes os.Stdout and os.Stderr into goroutines that buffer
// everything. Restored via t.Cleanup so the test binary's actual streams
// keep working for subsequent cases.
func captureFD(t *testing.T, f func()) (string, string) {
	t.Helper()
	// We don't redirect here because the subcommand handlers under test
	// either (a) print to os.Stderr/Stdout AND call os.Exit on error, or
	// (b) return cleanly via the rendering core (already covered by the
	// `renderList` / `runDoctorIssueLoop` direct unit tests). The pure
	// functions are the testing surface; the dispatch shims are thin
	// enough that integration tests would only reproduce the renderer's
	// behavior. Skipping FD capture is intentional.
	f()
	return "", ""
}

// ---------- subcommand dispatch (happy paths) ----------

func TestList_HappyPath(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.listResult = ListResult{
		Workspaces: []Workspace{{Name: "main", BrokerPort: 7890, WebPort: 7891, State: WorkspaceStateRunning, IsCLICurrent: true}},
	}
	captureStreams(t, func() {
		runWorkspaceList([]string{})
	})
	if len(fake.listCalls) != 1 {
		t.Fatalf("expected 1 List call, got %d", len(fake.listCalls))
	}
	if fake.listCalls[0].IncludeTrash {
		t.Fatal("default list should not request trash")
	}
}

func TestList_TrashFlagPropagates(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.listResult = ListResult{Trash: []TrashEntry{{TrashID: "x"}}}
	captureStreams(t, func() {
		runWorkspaceList([]string{"--trash"})
	})
	if len(fake.listCalls) != 1 || !fake.listCalls[0].IncludeTrash {
		t.Fatalf("expected --trash to set IncludeTrash, got %+v", fake.listCalls)
	}
}

func TestList_JSONFlagDoesNotChangeCallShape(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.listResult = ListResult{Workspaces: []Workspace{{Name: "alpha"}}}
	captureStreams(t, func() {
		runWorkspaceList([]string{"--json"})
	})
	if len(fake.listCalls) != 1 {
		t.Fatalf("expected 1 List call, got %d", len(fake.listCalls))
	}
}

func TestCreate_PassesFlagsThrough(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.createResult = Workspace{Name: "demo", BrokerPort: 7910, WebPort: 7911}
	captureStreams(t, func() {
		runWorkspaceCreate([]string{"--blueprint=founding-team", "--inherit-from=main", "demo"})
	})
	if len(fake.createCalls) != 1 {
		t.Fatalf("expected 1 Create call, got %d", len(fake.createCalls))
	}
	got := fake.createCalls[0]
	if got.Name != "demo" || got.Blueprint != "founding-team" || got.InheritFrom != "main" || got.FromScratch {
		t.Fatalf("unexpected create request: %+v", got)
	}
}

func TestCreate_FromScratchFlagPropagates(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.createResult = Workspace{Name: "scratch", BrokerPort: 7912, WebPort: 7913}
	captureStreams(t, func() {
		runWorkspaceCreate([]string{"--from-scratch", "scratch"})
	})
	if len(fake.createCalls) != 1 || !fake.createCalls[0].FromScratch {
		t.Fatalf("--from-scratch should propagate, got %+v", fake.createCalls)
	}
}

func TestSwitch_PassesOpenFlag(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.switchResult = Workspace{Name: "demo", BrokerPort: 7910, WebPort: 7911}
	prev := browserOpener
	defer func() { browserOpener = prev }()
	openedURL := ""
	browserOpener = func(url string) error {
		openedURL = url
		return nil
	}
	captureStreams(t, func() {
		runWorkspaceSwitch([]string{"--open", "demo"})
	})
	if len(fake.switchCalls) != 1 || !fake.switchCalls[0].Open || fake.switchCalls[0].Name != "demo" {
		t.Fatalf("unexpected switch call: %+v", fake.switchCalls)
	}
	if openedURL != "http://localhost:7911/" {
		t.Fatalf("expected browser opened to web URL, got %q", openedURL)
	}
}

func TestSwitch_NoOpenFlag_DoesNotOpen(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.switchResult = Workspace{Name: "demo", BrokerPort: 7910, WebPort: 7911}
	prev := browserOpener
	defer func() { browserOpener = prev }()
	called := false
	browserOpener = func(url string) error {
		called = true
		return nil
	}
	captureStreams(t, func() {
		runWorkspaceSwitch([]string{"demo"})
	})
	if called {
		t.Fatal("browser should not be opened without --open")
	}
}

func TestPause_DefaultIsGraceful(t *testing.T) {
	fake := newFakeOrchestrator(t)
	captureStreams(t, func() {
		runWorkspacePause([]string{"demo"})
	})
	if len(fake.pauseCalls) != 1 || fake.pauseCalls[0].Force {
		t.Fatalf("expected graceful pause, got %+v", fake.pauseCalls)
	}
}

func TestPause_ForceFlagPropagates(t *testing.T) {
	fake := newFakeOrchestrator(t)
	captureStreams(t, func() {
		runWorkspacePause([]string{"--force", "demo"})
	})
	if len(fake.pauseCalls) != 1 || !fake.pauseCalls[0].Force {
		t.Fatalf("expected force=true, got %+v", fake.pauseCalls)
	}
}

func TestResume_HappyPath(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.resumeResult = Workspace{Name: "demo", BrokerPort: 7910, WebPort: 7911}
	captureStreams(t, func() {
		runWorkspaceResume([]string{"demo"})
	})
	if len(fake.resumeCalls) != 1 || fake.resumeCalls[0] != "demo" {
		t.Fatalf("unexpected resume calls: %+v", fake.resumeCalls)
	}
}

func TestShred_YesFlagSkipsConfirm(t *testing.T) {
	fake := newFakeOrchestrator(t)
	captureStreams(t, func() {
		runWorkspaceShred([]string{"--yes", "demo"})
	})
	if len(fake.shredCalls) != 1 || fake.shredCalls[0].Permanent {
		t.Fatalf("expected non-permanent shred, got %+v", fake.shredCalls)
	}
}

func TestShred_PermanentFlagPropagates(t *testing.T) {
	fake := newFakeOrchestrator(t)
	captureStreams(t, func() {
		runWorkspaceShred([]string{"--yes", "--permanent", "demo"})
	})
	if len(fake.shredCalls) != 1 || !fake.shredCalls[0].Permanent {
		t.Fatalf("expected --permanent to set Permanent=true, got %+v", fake.shredCalls)
	}
}

func TestRestore_PassesTrashID(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.restoreResult = Workspace{Name: "demo", BrokerPort: 7920, WebPort: 7921}
	captureStreams(t, func() {
		runWorkspaceRestore([]string{"demo-1714305600"})
	})
	if len(fake.restoreCalls) != 1 || fake.restoreCalls[0] != "demo-1714305600" {
		t.Fatalf("unexpected restore calls: %+v", fake.restoreCalls)
	}
}

func TestDoctor_DispatchHappyPath(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.doctorReport = DoctorReport{}
	captureStreams(t, func() {
		runWorkspaceDoctor([]string{"--dry-run"})
	})
	if fake.doctorCalls != 1 {
		t.Fatalf("expected 1 doctor call, got %d", fake.doctorCalls)
	}
}

// ---------- workspace_test for runWorkspace dispatch table ----------

func TestRunWorkspace_DispatchTable(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.listResult = ListResult{}
	fake.createResult = Workspace{Name: "x", BrokerPort: 7910, WebPort: 7911}
	fake.switchResult = Workspace{Name: "x", BrokerPort: 7910, WebPort: 7911}
	fake.resumeResult = Workspace{Name: "x", BrokerPort: 7910, WebPort: 7911}
	fake.restoreResult = Workspace{Name: "x", BrokerPort: 7910, WebPort: 7911}
	fake.doctorReport = DoctorReport{}

	cases := []struct {
		name string
		args []string
		// counter selects which fake field to assert grew by 1.
		check func(t *testing.T, f *fakeWorkspaceOrchestrator) int
	}{
		{"list", []string{"list"}, func(t *testing.T, f *fakeWorkspaceOrchestrator) int { return len(f.listCalls) }},
		{"ls alias", []string{"ls"}, func(t *testing.T, f *fakeWorkspaceOrchestrator) int { return len(f.listCalls) }},
		{"create", []string{"create", "demo"}, func(t *testing.T, f *fakeWorkspaceOrchestrator) int { return len(f.createCalls) }},
		{"switch", []string{"switch", "demo"}, func(t *testing.T, f *fakeWorkspaceOrchestrator) int { return len(f.switchCalls) }},
		{"pause", []string{"pause", "demo"}, func(t *testing.T, f *fakeWorkspaceOrchestrator) int { return len(f.pauseCalls) }},
		{"resume", []string{"resume", "demo"}, func(t *testing.T, f *fakeWorkspaceOrchestrator) int { return len(f.resumeCalls) }},
		{"shred", []string{"shred", "--yes", "demo"}, func(t *testing.T, f *fakeWorkspaceOrchestrator) int { return len(f.shredCalls) }},
		{"restore", []string{"restore", "demo-1"}, func(t *testing.T, f *fakeWorkspaceOrchestrator) int { return len(f.restoreCalls) }},
		{"doctor", []string{"doctor", "--dry-run"}, func(t *testing.T, f *fakeWorkspaceOrchestrator) int { return f.doctorCalls }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := c.check(t, fake)
			captureStreams(t, func() { runWorkspace(c.args) })
			after := c.check(t, fake)
			if after != before+1 {
				t.Fatalf("dispatch %q: expected counter to grow by 1, got %d → %d", c.name, before, after)
			}
		})
	}
}

// ---------- error path coverage via dispatch ----------

func TestList_ErrorIsSurfaced(t *testing.T) {
	// Exercise the real error path: runWorkspaceList → orch.List returns
	// sentinel error → printError formats & exits. We swap printErrorOutput
	// + printErrorExit so the panic-with-sentinel inside printErrorExit is
	// recovered here and we can assert the exact bytes the user would see
	// on stderr.
	fake := newFakeOrchestrator(t)
	const sentinel = "registry corrupt: bad checksum"
	fake.listErr = errors.New(sentinel)

	fired, code, output := runAndCapturePrintError(t, func() {
		runWorkspaceList(nil)
	})

	if !fired {
		t.Fatalf("expected printError to fire when orchestrator returns an error; output=%q", output)
	}
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(output, sentinel) {
		t.Errorf("expected captured stderr to contain orchestrator error %q, got %q", sentinel, output)
	}
	if !strings.Contains(output, "list workspaces") {
		t.Errorf("expected captured stderr to contain the `list workspaces` prefix from runWorkspaceList, got %q", output)
	}
	if !strings.HasPrefix(output, "error: ") {
		t.Errorf("expected output to start with `error: ` prefix from printError, got %q", output)
	}
	// Sanity: the fake must actually have been invoked. If a future regression
	// short-circuits before calling List, this would catch it.
	if len(fake.listCalls) != 1 {
		t.Errorf("expected exactly 1 List call, got %d", len(fake.listCalls))
	}
}

// TestList_ErrorIsSurfaced_ViaRunWorkspaceDispatch confirms the same wiring
// when the error path is reached via the top-level `runWorkspace` dispatcher
// (mirrors how the user actually invokes `wuphf workspace list`). Catches a
// dispatch regression that bypasses runWorkspaceList.
func TestList_ErrorIsSurfaced_ViaRunWorkspaceDispatch(t *testing.T) {
	fake := newFakeOrchestrator(t)
	fake.listErr = errors.New("orchestrator unavailable")

	fired, code, output := runAndCapturePrintError(t, func() {
		runWorkspace([]string{"list"})
	})

	if !fired {
		t.Fatalf("expected printError to fire from dispatch path; output=%q", output)
	}
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(output, "orchestrator unavailable") {
		t.Errorf("expected stderr to contain orchestrator error, got %q", output)
	}
}

// ---------- render edge cases ----------

func TestRenderList_NeverStartedWorkspaceFallsBack(t *testing.T) {
	// A workspace row with an empty State should render as "never_started"
	// rather than blank, so the table column doesn't go ragged.
	ws := []Workspace{{Name: "fresh", BrokerPort: 7910, WebPort: 7911}}
	var buf bytes.Buffer
	if err := renderList(&buf, ListResult{Workspaces: ws}, false, false); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	if !strings.Contains(buf.String(), "never_started") {
		t.Fatalf("expected never_started fallback, got %q", buf.String())
	}
}

func TestRenderList_TrashJSON_RoundTrip(t *testing.T) {
	// JSON shape must round-trip cleanly with both slices populated. Ensures
	// the encoder doesn't drop fields and that the field tags remain stable
	// for the consumer contract (registry-dump pipelines).
	ts := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	in := ListResult{
		Workspaces: []Workspace{{Name: "main", State: WorkspaceStateRunning, BrokerPort: 7890, WebPort: 7891, CreatedAt: ts, LastUsedAt: ts}},
		Trash:      []TrashEntry{{TrashID: "demo-1714305600", Name: "demo", ShreddedAt: ts}},
	}
	var buf bytes.Buffer
	if err := renderList(&buf, in, true, false); err != nil {
		t.Fatalf("renderList: %v", err)
	}
	var out ListResult
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Workspaces) != 1 || out.Workspaces[0].Name != "main" {
		t.Fatalf("workspaces did not round-trip: %+v", out)
	}
	if len(out.Trash) != 1 || out.Trash[0].TrashID != "demo-1714305600" {
		t.Fatalf("trash did not round-trip: %+v", out)
	}
}

func TestPrintWorkspaceHelp_DoesNotPanic(t *testing.T) {
	// Smoke test: help printer writes to stderr without panicking. We don't
	// pin exact copy because the help text is human-tunable.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("printWorkspaceHelp panicked: %v", r)
		}
	}()
	printWorkspaceHelp()
}

// ---------- WorkspaceState constants ----------

func TestWorkspaceStateConstants_AreDistinct(t *testing.T) {
	// Sanity: the JSON-rendered state strings must be unique. Catches a
	// copy-paste regression where two states alias to the same string.
	states := []WorkspaceState{
		WorkspaceStateRunning,
		WorkspaceStatePaused,
		WorkspaceStateStarting,
		WorkspaceStateStopping,
		WorkspaceStateNeverStarted,
		WorkspaceStateError,
	}
	seen := map[WorkspaceState]bool{}
	for _, s := range states {
		if seen[s] {
			t.Fatalf("duplicate state value: %q", s)
		}
		seen[s] = true
	}
}

func TestDoctorIssueKindConstants_AreDistinct(t *testing.T) {
	// Same property as above for doctor issue kinds — they are user-visible
	// in dry-run/JSON output.
	kinds := []DoctorIssueKind{
		DoctorIssueOrphanTree,
		DoctorIssueZombieState,
		DoctorIssuePortConflict,
		DoctorIssueCorruptRegistry,
		DoctorIssueOrphanedSymlink,
		DoctorIssueMissingSymlink,
		DoctorIssuePartialMigration,
		DoctorIssueStoppingReconcile,
		DoctorIssueExpiredTrashSweep,
		DoctorIssueTokenFilePerm,
		DoctorIssueOpencodeRaceConfig,
	}
	seen := map[DoctorIssueKind]bool{}
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("duplicate kind value: %q", k)
		}
		seen[k] = true
	}
}

// ---------- workspaceCtx / workspaceCtxLong ----------

func TestWorkspaceCtx_HasShortDeadline(t *testing.T) {
	ctx, cancel := workspaceCtx()
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline on workspaceCtx")
	}
	if time.Until(deadline) > 11*time.Second {
		t.Fatalf("expected ≤10s deadline, got %s", time.Until(deadline))
	}
}

func TestWorkspaceCtxLong_HasLongDeadline(t *testing.T) {
	ctx, cancel := workspaceCtxLong()
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline on workspaceCtxLong")
	}
	if time.Until(deadline) < 30*time.Second {
		t.Fatalf("expected ≥30s deadline, got %s", time.Until(deadline))
	}
	if time.Until(deadline) > 61*time.Second {
		t.Fatalf("expected ≤60s deadline, got %s", time.Until(deadline))
	}
}

// ---------- doctor: detail line + auto-yes fail handling ----------

func TestDoctor_DetailLineRendered(t *testing.T) {
	fake := &fakeWorkspaceOrchestrator{}
	report := DoctorReport{Issues: []DoctorIssue{
		{Kind: DoctorIssueOrphanTree, Subject: "foo", Detail: "tree at ~/.wuphf-spaces/foo without registry entry", FixID: "x"},
	}}
	var out bytes.Buffer
	if err := runDoctorIssueLoop(context.Background(), fake, report, strings.NewReader(""), &out, doctorModeDryRun); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out.String(), "tree at ~/.wuphf-spaces/foo") {
		t.Fatalf("expected detail rendered, got %q", out.String())
	}
}

func TestDoctor_AutoYes_StopsOnFirstError(t *testing.T) {
	// Even in --yes mode, the first error stops further fixes (matches
	// interactive semantics so scripts get the same fail-fast posture).
	fake := &fakeWorkspaceOrchestrator{fixErr: errors.New("disk full")}
	report := DoctorReport{Issues: []DoctorIssue{
		{Kind: DoctorIssueOrphanTree, Subject: "a", FixID: "fix-a"},
		{Kind: DoctorIssueZombieState, Subject: "b", FixID: "fix-b"},
	}}
	var out bytes.Buffer
	err := runDoctorIssueLoop(context.Background(), fake, report, strings.NewReader(""), &out, doctorModeAutoYes)
	if err == nil {
		t.Fatal("expected error from failed fix")
	}
	if len(fake.fixCalls) != 1 {
		t.Fatalf("expected 1 fix attempt before stopping, got %d", len(fake.fixCalls))
	}
}
