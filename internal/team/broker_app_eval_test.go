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
	html := "<!doctype html><html><body>" + strings.Repeat("x", bundleBytes) + "</body></html>"
	manifest := CustomApp{
		ID: id, Slug: "probe", Name: "Probe", Entry: "index.html",
		Version: version, Status: status, EditChannel: channel,
		CreatedBy: "app-builder", CreatedAt: "2026-06-22T00:00:00Z",
		UpdatedAt: "2026-06-22T00:00:00Z",
		// A published app always carries the content hash of its committed bytes;
		// seed it so the finalization proof in deterministicAppGaps sees a real
		// build. Tests that exercise the unfinalized case clear it explicitly.
		ContentHash: customAppContentHash(html),
	}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, "app.json"), raw, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
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

// setManifestContentHash rewrites a seeded app's recorded ContentHash on disk so
// a test can simulate the legacy (empty) or corrupt (mismatched) cases.
func setManifestContentHash(t *testing.T, id, hash string) {
	t.Helper()
	manifestPath := filepath.Join(CustomAppsRootDir(), id, "app.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m CustomApp
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m.ContentHash = hash
	out, _ := json.Marshal(m)
	if err := os.WriteFile(manifestPath, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Regression (legacy false-reopen fix): an app published before the ContentHash
// field existed carries an empty recorded hash but real bytes — it IS finalized
// and must NOT be reopened by the finalization check; it reaches the judge.
func TestAppAcceptanceLegacyEmptyHashReachesJudge(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	var judgeCalled bool
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		judgeCalled = true
		return `{"meets":true,"summary":"meets the brief","gaps":[]}`, nil
	})
	b := newTestBroker(t)
	const ch = "task-app-15"
	const id = "app_00000000000000ee"
	seedAcceptanceApp(t, id, ch, 5000, "ready", 1)
	setManifestContentHash(t, id, "") // legacy: real bytes, no recorded hash
	b.tasks = append(b.tasks, teamTask{
		ID: "OFFICE-15", Owner: appBuilderSlug, Channel: ch,
		Title: "Build app: Budget Monitor", Details: "Build a SaaS budget monitor.",
		status: "done",
	})

	b.evaluateAppAcceptanceForTask("OFFICE-15")

	if !judgeCalled {
		t.Error("a legacy app with real bytes but no recorded hash must reach the judge, not be reopened")
	}
	if task := b.taskByIDLocked("OFFICE-15"); task == nil || task.status != "done" {
		t.Fatalf("a legacy finalized app that meets the brief must stay done; got %+v", task)
	}
	if n := countMessagesOfKind(b, ch, appAcceptanceFailKind); n != 0 {
		t.Fatalf("no fail notice expected for a legacy finalized app, got %d", n)
	}
}

// Regression (integrity proof): a published bundle whose bytes disagree with the
// recorded build hash is a corrupt/partial publish — reopened deterministically,
// never sent to the judge.
func TestAppAcceptanceReopensCorruptBundle(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	var judgeCalled bool
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		judgeCalled = true
		return `{"meets":true,"summary":"looks fine","gaps":[]}`, nil
	})
	b := newTestBroker(t)
	const ch = "task-app-16"
	const id = "app_00000000000000ef"
	seedAcceptanceApp(t, id, ch, 5000, "ready", 1)
	setManifestContentHash(t, id, "deadbeefdeadbeef") // does not match the bundle
	b.tasks = append(b.tasks, teamTask{
		ID: "OFFICE-16", Owner: appBuilderSlug, Channel: ch,
		Title: "Build app: Budget Monitor", Details: "Build a SaaS budget monitor.",
		status: "done",
	})

	b.evaluateAppAcceptanceForTask("OFFICE-16")

	if judgeCalled {
		t.Error("a corrupt bundle must be caught deterministically, not sent to the judge")
	}
	if task := b.taskByIDLocked("OFFICE-16"); task == nil || task.status != "in_progress" {
		t.Fatalf("a corrupt-bundle build must be reopened; got %+v", task)
	}
	if countMessagesOfKind(b, ch, appAcceptanceFailKind) != 1 {
		t.Fatalf("want 1 acceptance-fail notice, got %d", countMessagesOfKind(b, ch, appAcceptanceFailKind))
	}
}

// Regression (stalled-build backstop): an App Builder build task that stalled
// in_progress without finalizing is graded by the watchdog sweep — once per stall
// episode (dedupe), only for App Builder tasks — and an unfinalized one reopens.
func TestSweepStalledAppBuildsReopensUnfinalized(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	var judgeCalled bool
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		judgeCalled = true
		return `{"meets":true,"summary":"","gaps":[]}`, nil
	})
	b := newTestBroker(t)
	const ch = "task-app-20"
	const id = "app_000000000000aa20"
	seedAcceptanceApp(t, id, ch, 5000, "ready", 1)
	// Make it an unfinalized build: App.tsx still the scaffold.
	scaffold := filepath.Join(CustomAppsRootDir(), id, "src", "App.tsx")
	if err := os.WriteFile(scaffold, []byte("// "+appScaffoldSentinel+"\nexport default function App(){return null}"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.tasks = append(b.tasks,
		teamTask{ID: "OFFICE-20", Owner: appBuilderSlug, Channel: ch,
			Title: "Build app: X", Details: "Build a real tool.",
			status: "in_progress", StalledSince: "2026-06-26T00:00:00Z"},
		// A non-App-Builder stalled task must be ignored by the sweep.
		teamTask{ID: "OFFICE-21", Owner: "ceo", Channel: "task-other",
			status: "in_progress", StalledSince: "2026-06-26T00:00:00Z"},
	)

	due := b.sweepStalledAppBuildsLocked()
	if len(due) != 1 || due[0] != "OFFICE-20" {
		t.Fatalf("sweep should return only the stalled App Builder build, got %v", due)
	}
	if again := b.sweepStalledAppBuildsLocked(); len(again) != 0 {
		t.Fatalf("same stall episode must not re-fire, got %v", again)
	}

	// Grading the stalled unfinalized build reopens it deterministically.
	b.evaluateAppAcceptanceForTask("OFFICE-20")
	if judgeCalled {
		t.Error("a stalled scaffold build must be caught deterministically, not sent to the judge")
	}
	if countMessagesOfKind(b, ch, appAcceptanceFailKind) != 1 {
		t.Fatalf("want 1 acceptance-fail notice, got %d", countMessagesOfKind(b, ch, appAcceptanceFailKind))
	}

	// A NEW stall episode (different StalledSince) is acted on again.
	b.taskByIDLocked("OFFICE-20").StalledSince = "2026-06-26T01:00:00Z"
	b.taskByIDLocked("OFFICE-20").status = "in_progress"
	if due2 := b.sweepStalledAppBuildsLocked(); len(due2) != 1 {
		t.Fatalf("a new stall episode must re-fire, got %v", due2)
	}
}

// Regression (no silent pass): an App Builder build task that reached done with
// NO app bound to its channel (register_app never called) is the strongest
// non-delivery and must be reopened + logged, not silently accepted.
func TestAppAcceptanceReopensWhenNoAppRegistered(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	var judgeCalled bool
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		judgeCalled = true
		return `{"meets":true,"summary":"","gaps":[]}`, nil
	})
	b := newTestBroker(t)
	const ch = "task-app-17"
	// No seedAcceptanceApp: nothing is bound to this channel.
	b.tasks = append(b.tasks, teamTask{
		ID: "OFFICE-17", Owner: appBuilderSlug, Channel: ch,
		Title: "Build app: Nothing Shipped", Details: "Build a tool.",
		status: "done",
	})

	b.evaluateAppAcceptanceForTask("OFFICE-17")

	if judgeCalled {
		t.Error("no app bound → must be caught deterministically, not sent to the judge")
	}
	if task := b.taskByIDLocked("OFFICE-17"); task == nil || task.status != "in_progress" {
		t.Fatalf("a build that registered no app must be reopened; got %+v", task)
	}
	if countMessagesOfKind(b, ch, appAcceptanceFailKind) != 1 {
		t.Fatalf("want 1 acceptance-fail notice, got %d", countMessagesOfKind(b, ch, appAcceptanceFailKind))
	}
}
