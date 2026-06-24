package team

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── Pure policy ──────────────────────────────────────────────────────────────

func TestDecideAppAcceptance(t *testing.T) {
	pass := appAcceptanceDecision{Meets: true, Summary: "good", Gaps: nil}
	fail := appAcceptanceDecision{Meets: false, Gaps: []string{"missing export"}}

	// Judge says meets + no deterministic gap → PASS.
	if a, _ := decideAppAcceptance(pass, nil, 0); a != appAcceptanceActionPass {
		t.Fatalf("meets+clean want PASS, got %v", a)
	}
	// A deterministic gap overrides a meets=true verdict.
	if a, gaps := decideAppAcceptance(pass, []string{"did not publish"}, 0); a != appAcceptanceActionReopen || len(gaps) == 0 {
		t.Fatalf("det gap must override meets=true → REOPEN with gaps, got %v %v", a, gaps)
	}
	// Fail under budget → REOPEN.
	if a, _ := decideAppAcceptance(fail, nil, 0); a != appAcceptanceActionReopen {
		t.Fatalf("fail under budget want REOPEN, got %v", a)
	}
	if a, _ := decideAppAcceptance(fail, nil, appAcceptanceMaxRetries-1); a != appAcceptanceActionReopen {
		t.Fatalf("fail at last retry want REOPEN, got %v", a)
	}
	// Fail at/over budget → HALT (flag for a human, stop looping).
	if a, _ := decideAppAcceptance(fail, nil, appAcceptanceMaxRetries); a != appAcceptanceActionHalt {
		t.Fatalf("fail over budget want HALT, got %v", a)
	}
}

func TestParseAppAcceptanceDecision(t *testing.T) {
	d, ok := parseAppAcceptanceDecision("noise ```json\n{\"meets\":false,\"summary\":\"x\",\"gaps\":[\"a\",\"b\"]}\n``` trailing")
	if !ok || d.Meets || len(d.Gaps) != 2 {
		t.Fatalf("parse fenced JSON failed: ok=%v d=%+v", ok, d)
	}
	if _, ok := parseAppAcceptanceDecision("not json at all"); ok {
		t.Fatal("garbage must not parse")
	}
}

func TestBuildAppAcceptancePromptIncludesBriefAndGaps(t *testing.T) {
	app := CustomApp{Name: "OKR Tracker", Summary: "grade OKRs"}
	system, user := buildAppAcceptancePrompt(app, "renders MantineProvider; ai() one call", "Paste OKRs and grade each KR red/amber/green", []string{"did not publish"})
	if !strings.Contains(system, "acceptance reviewer") {
		t.Error("system prompt should frame the acceptance role")
	}
	for _, want := range []string{"OKR Tracker", "Paste OKRs and grade", "renders MantineProvider", "did not publish"} {
		if !strings.Contains(user, want) {
			t.Errorf("user prompt missing %q", want)
		}
	}
}

// ── Integration: FAIL reopens, retries are bounded, PASS sticks ──────────────

// seedAcceptanceApp writes a minimal published app on disk bound to `channel`,
// so appForEditChannel + the deterministic checks resolve it.
func seedAcceptanceApp(t *testing.T, id, channel string, bundleBytes int, status string, version int) {
	t.Helper()
	dir := filepath.Join(CustomAppsRootDir(), id)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir app: %v", err)
	}
	manifest := CustomApp{
		ID: id, Slug: "probe", Name: "Probe", Entry: "index.html",
		Version: version, Status: status, EditChannel: channel,
		CreatedBy: "app-builder", CreatedAt: "2026-06-22T00:00:00Z",
		UpdatedAt: "2026-06-22T00:00:00Z",
	}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "app.json"), raw, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	html := "<!doctype html><html><body>" + strings.Repeat("x", bundleBytes) + "</body></html>"
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "App.tsx"), []byte("export default function App(){return null}"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
}

func countMessagesOfKind(b *Broker, channel, kind string) int {
	n := 0
	for i := range b.messages {
		if b.messages[i].Channel == channel && b.messages[i].Kind == kind {
			n++
		}
	}
	return n
}

func TestAppAcceptanceFailReopensThenHalts(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	withFakeAppsLLM(t, func(_ context.Context, _, prompt string) (string, error) {
		if !strings.Contains(prompt, "Export the scored leads to CSV") {
			t.Errorf("judge prompt should carry the brief, got: %.120s", prompt)
		}
		return `{"meets":false,"summary":"no export","gaps":["The CSV export button from the brief is missing."]}`, nil
	})

	b := newTestBroker(t)
	const ch = "task-app-7"
	seedAcceptanceApp(t, "app_00000000000000aa", ch, 5000, "ready", 1)
	b.tasks = append(b.tasks, teamTask{
		ID: "OFFICE-7", Owner: appBuilderSlug, Channel: ch,
		Title: "Build app: Lead Board", Details: "Build a lead board. Export the scored leads to CSV.",
		status: "done",
	})

	// First fail → REOPEN: task back to in_progress, one fail notice, App Builder woken.
	b.evaluateAppAcceptanceForTask("OFFICE-7")
	task := b.taskByIDLocked("OFFICE-7")
	if task == nil || task.status != "in_progress" {
		t.Fatalf("expected task reopened to in_progress, got %+v", task)
	}
	if got := countMessagesOfKind(b, ch, appAcceptanceFailKind); got != 1 {
		t.Fatalf("want 1 acceptance-fail notice, got %d", got)
	}
	var woke bool
	for i := range b.actions {
		if b.actions[i].Kind == taskFollowUpActionKind && b.actions[i].RelatedID == "OFFICE-7" {
			woke = true
		}
	}
	if !woke {
		t.Error("a reopen must arm a task_followup to wake the App Builder")
	}

	// Second fail → still REOPEN (budget = 2).
	task.status = "done"
	b.evaluateAppAcceptanceForTask("OFFICE-7")
	if countMessagesOfKind(b, ch, appAcceptanceFailKind) != 2 {
		t.Fatalf("second fail should reopen again, fails=%d", countMessagesOfKind(b, ch, appAcceptanceFailKind))
	}

	// Third fail → HALT (budget exhausted): no new fail notice, a halt notice, task NOT reopened.
	task = b.taskByIDLocked("OFFICE-7")
	task.status = "done"
	b.evaluateAppAcceptanceForTask("OFFICE-7")
	if countMessagesOfKind(b, ch, appAcceptanceFailKind) != 2 {
		t.Errorf("halt must not add a fail notice, fails=%d", countMessagesOfKind(b, ch, appAcceptanceFailKind))
	}
	if countMessagesOfKind(b, ch, appAcceptanceHaltKind) != 1 {
		t.Errorf("want a halt notice, got %d", countMessagesOfKind(b, ch, appAcceptanceHaltKind))
	}
	if task := b.taskByIDLocked("OFFICE-7"); task.status != "done" {
		t.Errorf("halt must leave the task as delivered for a human, got %q", task.status)
	}
}

func TestAppAcceptancePassPostsSummaryAndKeepsDone(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		return `{"meets":true,"summary":"every requirement met","gaps":[]}`, nil
	})
	b := newTestBroker(t)
	const ch = "task-app-9"
	seedAcceptanceApp(t, "app_00000000000000bb", ch, 5000, "ready", 1)
	b.tasks = append(b.tasks, teamTask{
		ID: "OFFICE-9", Owner: appBuilderSlug, Channel: ch,
		Title: "Build app: Digest", Details: "Build a standup digest grouped by agent.",
		status: "done",
	})
	_ = time.Now
	b.evaluateAppAcceptanceForTask("OFFICE-9")
	if countMessagesOfKind(b, ch, appAcceptancePassKind) != 1 {
		t.Fatalf("PASS should post one acceptance-pass notice, got %d", countMessagesOfKind(b, ch, appAcceptancePassKind))
	}
	if task := b.taskByIDLocked("OFFICE-9"); task.status != "done" {
		t.Errorf("PASS must keep the task done, got %q", task.status)
	}
}

// Regression: a judge timeout must NOT let an unpublished/stuck app stay "done".
// The bug was that a judge error returned early, BEFORE the deterministic gate,
// so a build that never published (status "building", v0 — the exact 30-minute
// stuck state observed in the live eval) slipped through as delivered.
func TestAppAcceptanceJudgeTimeoutStillReopensUnpublished(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	var judgeCalled bool
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		judgeCalled = true
		return "", context.DeadlineExceeded // simulate the timeout
	})
	b := newTestBroker(t)
	const ch = "task-app-11"
	// UNPUBLISHED app: status "building", version 0.
	seedAcceptanceApp(t, "app_00000000000000cc", ch, 5000, "building", 0)
	b.tasks = append(b.tasks, teamTask{
		ID: "OFFICE-11", Owner: appBuilderSlug, Channel: ch,
		Title: "Build app: Stuck", Details: "Build a thing.",
		status: "done",
	})

	b.evaluateAppAcceptanceForTask("OFFICE-11")

	// The deterministic gate must reopen WITHOUT ever consulting the judge.
	if judgeCalled {
		t.Error("an unpublished app must be caught by the deterministic gate without calling the judge")
	}
	if task := b.taskByIDLocked("OFFICE-11"); task == nil || task.status != "in_progress" {
		t.Fatalf("a judge timeout must not stop the deterministic gate from reopening; got %+v", task)
	}
	if countMessagesOfKind(b, ch, appAcceptanceFailKind) != 1 {
		t.Fatalf("want 1 deterministic acceptance-fail notice, got %d", countMessagesOfKind(b, ch, appAcceptanceFailKind))
	}
}

// Regression: an app that PUBLISHED (ready/v1) but is still the unmodified
// starter scaffold must be reopened deterministically — even if the judge
// (wrongly) passes it — because the agent never built the requested tool.
func TestAppAcceptanceReopensUnmodifiedScaffold(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	var judgeCalled bool
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		judgeCalled = true
		return `{"meets":true,"summary":"looks fine","gaps":[]}`, nil
	})
	b := newTestBroker(t)
	const ch = "task-app-13"
	seedAcceptanceApp(t, "app_00000000000000dd", ch, 5000, "ready", 1)
	// Overwrite App.tsx with the unmodified scaffold (carries the sentinel).
	scaffoldPath := filepath.Join(CustomAppsRootDir(), "app_00000000000000dd", "src", "App.tsx")
	if err := os.WriteFile(scaffoldPath,
		[]byte("// "+appScaffoldSentinel+"\nexport default function App(){return null}"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.tasks = append(b.tasks, teamTask{
		ID: "OFFICE-13", Owner: appBuilderSlug, Channel: ch,
		Title: "Build app: Real Thing", Details: "Build a real domain tool, not the scaffold.",
		status: "done",
	})

	b.evaluateAppAcceptanceForTask("OFFICE-13")

	if judgeCalled {
		t.Error("a scaffold-unchanged app must be caught deterministically, not sent to the judge")
	}
	if task := b.taskByIDLocked("OFFICE-13"); task == nil || task.status != "in_progress" {
		t.Fatalf("an unmodified scaffold must be reopened; got %+v", task)
	}
	if countMessagesOfKind(b, ch, appAcceptanceFailKind) != 1 {
		t.Fatalf("want 1 acceptance-fail notice, got %d", countMessagesOfKind(b, ch, appAcceptanceFailKind))
	}
}
