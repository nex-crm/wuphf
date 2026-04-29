# launcher.go decomposition — plan

Status: in progress. C1–C4 landed (combined PR); C5a / C5 / C6 still ahead. See git log for exact landing commits.

Source of truth: `internal/team/launcher.go` (was 4998 lines, 161 funcs at plan time; -1340 lines through C4).

## Decisions, up front

- Stay inside package `internal/team`. Do **not** spin up child packages (`team/dispatch`, `team/scheduler`, etc.). The struct surface is already entangled (`*Launcher` pointer, `*Broker`, unexported types like `notificationTarget`, `officeMember`, `teamTask`, `headlessCodexTurn`, `paneDispatchTurn`, `officeActionLog`). Promoting them to a sub-package means exporting half the package's domain types — a mechanical change with a huge diff and no real isolation gain. Splits become **new files** + **new unexported types** in the same package, with `*Launcher` keeping a pointer/field to each.
- Each new type owns its own state and its own mutex. `Launcher` keeps the field, but the mutex moves into the new type. No more "the launcher has 3 mutexes".
- Test seams use the existing `atomic.Pointer[fn]` override pattern (`launcherSendNotificationToPaneOverride` in launcher.go:3142, swap helper in `test_support.go:184`). It already works under `-race`, supports nested cleanup, and ships in production with zero overhead. Replicate that exact shape for new seams; do not invent a fresh injection style.
- Time-based loops get a `clock` interface plus a `signal` channel. No `time.Sleep` in tests — the user's hard rule. Production uses `realClock`; tests use `manualClock` with a `Tick()` method that releases pending sleepers and exposes a `Sent <-chan struct{}` for "I observed the work" signalling.
- All extractions are **PRs against `main`**, one cluster per PR, in the order below. Each PR must (a) leave `go test ./internal/team/... -race` green, (b) ship at least one new test exercising a path that was 0% before, and (c) not touch any caller outside `internal/team`.

---

## 1. Cluster proposal

Numbering matches the cut order in §2.

### C1 — `prompt_builder.go` (new type `promptBuilder`)
**Funcs moved:** `buildPrompt`, `markdownKnowledgeToolBlock`, `markdownKnowledgeMemoryBlock`, `teamVoiceForSlug`, `headlessSandboxNote`. Helpers: `agentConfigFromMember` if it stays referenced only here; otherwise leave it.

**New surface:**
```go
type promptBuilder struct {
    pack             *agent.PackDefinition
    bp               *operations.Blueprint
    isOneOnOne       func() bool
    isFocusMode      func() bool
    packName         func() string
    leadSlug         func() string
    members          func() []officeMember
    policies         func() []officePolicy
    memoryBackend    config.MemoryBackend
    nexDisabled      bool
}
func (p *promptBuilder) Build(slug string) string
```

**Launcher keeps:** a `prompt promptBuilder` field built once per `Launch()`/`reconfigureVisibleAgents()`. `buildPrompt(slug)` becomes a one-liner: `return l.prompt.Build(slug)`.

**Why first:** zero side effects, zero goroutines, zero broker writes. It's a 300-line `strings.Builder`. The only reason it's at 49% coverage is that nobody wrote table tests against it. Trivial to test by snapshot; trivial to ship.

---

### C2 — `office_targets.go` (new type `officeTargeter`)
**Funcs moved:** `agentPaneSlugs`, `officeAgentOrder`, `visibleOfficeMembers`, `overflowOfficeMembers`, `paneEligibleOfficeMembers`, `overflowWindowName`, `resolvePaneTargetForSlug`, `agentPaneTargets`, `agentNotificationTargets`, `shouldUseHeadlessDispatchForSlug`, `shouldUseHeadlessDispatchForTarget`, `skipPaneForSlug`, `isChannelDM`, `officeMembersSnapshot`, `officeMemberBySlug`, `officeLeadSlug`, `officeLeadSlugFrom`, `activeSessionMembers`, `getAgentName`. Plus the runtime predicates that drive headless routing: `memberEffectiveProviderKind`, `memberUsesHeadlessOneShotRuntime`, `normalizeProviderKind`, `usesPaneRuntime`, `requiresClaudeSessionReset`.

**New surface:**
```go
type officeTargeter struct {
    sessionName       string
    paneBackedAgents  *bool                  // pointer back to launcher field
    failedPaneSlugs   map[string]string      // shared map; targeter reads, dispatcher writes
    broker            brokerSnapshotter      // narrow interface, see §3
    isOneOnOne        func() bool
    oneOnOneAgent     func() string
    provider          func() string          // l.provider as a getter
}
func (o *officeTargeter) PaneTargets() map[string]notificationTarget
func (o *officeTargeter) NotificationTargets() map[string]notificationTarget
func (o *officeTargeter) LeadSlug() string
func (o *officeTargeter) MemberBySlug(slug string) officeMember
func (o *officeTargeter) ShouldUseHeadlessFor(slug string) bool
```

**Launcher keeps:** field `targets *officeTargeter`. Every `l.officeLeadSlug()` etc. becomes `l.targets.LeadSlug()`.

**Why second:** pure data-shape logic over `Broker.OfficeMembers()`/`SessionModeState()`. No goroutines, no tmux, no processes. The two side-effects in this cluster (`failedPaneSlugs` mutation, `paneBackedAgents` mutation) move with their writers — `failedPaneSlugs` is written by `recordPaneSpawnFailure` in cluster C5; `paneBackedAgents` is written by `trySpawnWebAgentPanes` in C5. The targeter is read-only over both.

---

### C3 — `notification_context.go` (new type `notificationContextBuilder`)
**Funcs moved:** `buildNotificationContext`, `ultimateThreadRoot`, `threadMessageIDs`, `buildTaskNotificationContext`, `relevantTaskForTarget`, `responseInstructionForTarget`, `buildMessageWorkPacket`, `buildTaskExecutionPacket`, `extractTaskFileTargets`, `truncate`. Plus `taskNotificationContent` and `humanizeNotificationType`.

**New surface:**
```go
type notificationContextBuilder struct {
    broker          brokerSnapshotter        // ChannelMessages, ChannelTasks, AllTasks, EnabledMembers
    targeter        *officeTargeter          // for LeadSlug, MemberBySlug, isChannelDM
    isOneOnOne      func() bool
}
func (b *notificationContextBuilder) MessageWorkPacket(msg channelMessage, slug string) string
func (b *notificationContextBuilder) TaskExecutionPacket(slug string, action officeActionLog, task teamTask, content string) string
func (b *notificationContextBuilder) TaskNotificationContent(action officeActionLog, task teamTask) string
```

**Launcher keeps:** field `notify *notificationContextBuilder`. `sendChannelUpdate` and `sendTaskUpdate` (which stay on the launcher because they decide pane vs headless dispatch) call into it.

**Why third:** large surface of pure-string logic operating on broker reads. Read-only, deterministic, and exactly the cluster where coverage will jump the most. The 0%-coverage entries here are 0% only because tests can't easily set up a `Broker` with the needed message graph — extracting against a `brokerSnapshotter` interface lets us inject a stub map of messages and tasks and exercise threading edge cases (missing root, cycles, deep reply chains, blocked tasks, review-state lead summaries) without spinning up a real Broker.

---

### C4 — `scheduler.go` (new type `watchdogScheduler`)
**Funcs moved:** `watchdogSchedulerLoop`, `processDueSchedulerJobs`, `processDueTaskJob`, `processDueRequestJob`, `processDueWorkflowJob`, `nextWorkflowRun`, `recordWatchdogLedger`, `updateSchedulerJob`. Plus `shouldBackfillTaskOwner` (already a free func).

**New surface:**
```go
type watchdogScheduler struct {
    broker        schedulerBroker      // narrow: DueSchedulerJobs, FindTask/Request, ResumeTask,
                                        //         CreateWatchdogAlert, RecordSignals, RecordAction, etc.
    targeter      *officeTargeter
    notify        *notificationContextBuilder
    deliverTask   func(officeActionLog, teamTask)   // back-edge to launcher.deliverTaskNotification
    clock         clock                              // see §3
    pollEvery     time.Duration                      // formerly hardcoded 20s
    initialDelay  time.Duration                      // formerly hardcoded 15s
    stopCh        chan struct{}
    done          sync.WaitGroup
    onTickDone    chan struct{}                      // closed-and-replaced per tick; nil in prod
}
func (w *watchdogScheduler) Start(ctx context.Context)
func (w *watchdogScheduler) Stop()
func (w *watchdogScheduler) processOnce()
```

**Launcher keeps:** field `scheduler *watchdogScheduler`. `Launch()` constructs and `Start()`s it; `Kill()` calls `Stop()`. The `go l.watchdogSchedulerLoop()` line goes away.

**Why fourth (mid-risk):** spawns a goroutine, but the work it does each tick is a deterministic batch. Easy to test by calling `processOnce()` directly without ever calling `Start()`, plus the loop itself can be tested with the manual clock + `onTickDone` signal (see §3). This is the highest-value mid-risk cluster — the four `processDue*Job` paths are currently 0%-covered and are exactly where silent scheduler bugs live (the `task.Blocked` rate-limit retry path on line 1542 is one I'd flag for a regression test on the way out).

---

### C5 — `pane_lifecycle.go` (new type `paneLifecycle`)
**Funcs moved:** `spawnVisibleAgents`, `spawnOverflowAgents`, `detectDeadPanesAfterSpawn`, `recordPaneSpawnFailure`, `trySpawnWebAgentPanes`, `paneFallbackMessages`, `reportPaneFallback`, `listTeamPanes`, `clearAgentPanes`, `clearOverflowAgentWindows`, `parseAgentPaneIndices`, `shouldPrimeClaudePane`, `primeVisibleAgents`, `watchChannelPaneLoop`, `channelPaneStatus`, `channelPaneNeedsRespawn`, `isNoSessionError`, `captureDeadChannelPane`, `channelStderrLogPath`, `channelPaneSnapshotPath`, `capturePaneTargetContent`, `capturePaneContent`, `respawnPanesAfterReseed`, `HasLiveTmuxSession`, `isMissingTmuxSession`, `reconfigureVisibleAgents`. Helper: `shellQuote`.

**New surface:**
```go
type paneLifecycle struct {
    sessionName     string
    socket          string
    cwd             string
    runner          tmuxRunner       // interface wrapping exec.CommandContext("tmux", ...)
    targeter        *officeTargeter
    promptBuilder   *promptBuilder
    notifyOverride  *atomic.Pointer[launcherSendNotificationToPaneFn]  // existing seam, reused
    failedPaneSlugs map[string]string                                   // owned here, read by targeter
    paneBackedFlag  *bool                                               // back-pointer to launcher field
    onPaneSpawn     func()                                              // signal hook for tests; nil in prod
}
```

**Launcher keeps:** field `panes *paneLifecycle`. `Launch()` calls `panes.SpawnVisibleAgents()` only if `usesPaneRuntime()`.

**Why fifth (high-risk):** every method here either shells out to `tmux` or writes to the filesystem. The cluster is internally cohesive but we can't fake `tmux` from the outside without a runner interface. Wrap it; that's the only way to test pane spawn fallback without a real terminal session. Order matters: it must come **after** the targeter cluster (C2) lands, because `paneLifecycle` reads the targeter to decide which slugs to spawn.

---

### C6 — `pane_dispatch.go` (new type `paneDispatcher`)
**Funcs moved:** `queuePaneNotification`, `runPaneDispatchQueue`, `launcherSendNotificationToPane`, `sendNotificationToPane`. Plus the package-level `paneDispatchMinGap`, `paneDispatchCoalesceWindow` vars (move alongside but keep package-level so test helpers can swap them in process; see §3 for the alternative).

**New surface:**
```go
type paneDispatcher struct {
    runner       tmuxRunner       // shared with paneLifecycle
    sendOverride *atomic.Pointer[launcherSendNotificationToPaneFn]  // existing seam
    clock        clock
    minGap       time.Duration
    coalesce     time.Duration

    mu           sync.Mutex
    queues       map[string][]paneDispatchTurn
    workers      map[string]bool
    lastSentAt   map[string]time.Time

    workerWg     sync.WaitGroup     // new: lets tests await all workers without Sleep
    onSent       chan struct{}      // optional signal: closed-and-replaced per send; nil in prod
}
func (p *paneDispatcher) Enqueue(slug, paneTarget, notification string)
func (p *paneDispatcher) Stop()                  // closes a stop channel + WaitGroup drain
```

**Launcher keeps:** field `paneDispatch *paneDispatcher`. `paneDispatchMu`/`paneDispatchQueues`/`paneDispatchWorkers`/`paneDispatchLastSentAt` come off the Launcher struct.

**Why sixth (high-risk):** spawns a goroutine *per slug*, has subtle coalesce semantics, and is the place where the 60s/3s timing gates live. It's where the user has historically lost messages from concurrency bugs. We need the `WaitGroup` and the `Stop()` to make it deterministically testable. Existing `pane_dispatch_queue_test.go` is good — it uses the override and works without sleeps, so the public surface is already test-shaped, we're just giving it an owner type instead of leaving the maps on the launcher.

---

### C7 — `headless_workers.go` (extracted methods, type `headlessWorkerPool`)
**Funcs moved:** Everything currently namespaced under `headless_codex.go` already lives outside `launcher.go` for the queue logic itself. What moves *out of launcher.go* is the spawn/teardown the launcher owns: `headlessMu`/`headlessCtx`/`headlessCancel`/`headlessWorkers`/`headlessActive`/`headlessQueues`/`headlessDeferredLead`/`headlessStopCh`/`headlessWorkerWg` plus the spawn helper currently called from `Launch()`. Keep the existing `headless_codex.go` file structure; just move the launcher-side fields into a `headlessWorkerPool` type and add an interface seam for `enqueueHeadlessCodexTurn`.

**New surface:**
```go
type headlessWorkerPool struct {
    mu         sync.Mutex
    ctx        context.Context
    cancel     context.CancelFunc
    workers    map[string]bool
    active     map[string]*headlessCodexActiveTurn
    queues     map[string][]headlessCodexTurn
    deferred   *headlessCodexTurn
    stopCh     chan struct{}
    wg         sync.WaitGroup
}
```

**Launcher keeps:** field `headless *headlessWorkerPool`. The package-internal call sites that already say `l.headlessQueues` etc. become `l.headless.queues` (still within the package; not exported). This is a mechanical move — the goal is to **stop the Launcher struct from owning a third sub-mutex**, not to redesign the worker semantics. Leave the goroutine-leak fix from PR #320 (per memory) untouched.

**Why seventh (high-risk):** these fields are touched from `Launch()`, `Kill()`, the resume path, and `headless_codex.go`. Mechanical move with broad blast radius. Schedule it last among the goroutine clusters specifically because nothing else depends on the move — once the other clusters are extracted and their tests are green, this one is only at risk of regressing itself, not them.

---

### C8 — `bootstrap.go` (lifecycle stays on Launcher; this file just hosts what's left)
After C1–C7, `launcher.go` keeps: `Launcher` struct + getters/setters, `NewLauncher`, `Preflight`, `Launch`, `Attach`, `Kill`, `ResetSession`, `ReconfigureSession`, the four notification-loop wiring funcs (`notifyAgentsLoop`, `notifyTaskActionsLoop`, `notifyOfficeChangesLoop`, `pollOneRelayEventsLoop`), and the small office-change delivery helpers (`deliverOfficeChangeNotification`, `officeChangeTaskNotifications`, `respawnPanesAfterReseed`, `safeDeliverMessage`, `recoverPanicTo`).

Web-mode code (`PreflightWeb`, `LaunchWeb`, `maybeOfferNex`, `waitForWebReady`, `openBrowser`, `stdinIsTTY`) gets split into `launcher_web.go`. It's pure split-by-build-target hygiene; no new type. Cheap, do it as part of C1.

Broker-side housekeeping (`killStaleBroker`, `ResetBrokerState`, `ClearPersistedBrokerState`, `officePIDFilePath`, `writeOfficePIDFile`, `clearOfficePIDFile`, `killPersistedOfficeProcess`, `resetBrokerState`, `brokerBaseURL`) moves to `broker_lifecycle.go`. No type — these are package funcs already. Free move; bundle with C5.

End state: `launcher.go` is ~600–800 lines and is exactly the orchestration glue.

---

## 2. Cut order — justification

| # | Cluster | Risk | Why this slot |
|---|---|---|---|
| C1 | `promptBuilder` | low | Pure func over data. No goroutines, no mutexes, no broker writes. Snapshot tests cover it. Ship as the proof-of-pattern PR. |
| C2 | `officeTargeter` | low | Read-only over broker snapshot funcs. No goroutines. Reveals the smallest necessary `brokerSnapshotter` interface, which then unblocks C3 and C4. |
| C3 | `notificationContextBuilder` | low-mid | Large but pure. Depends on C2's targeter. Coverage payoff is the highest of any cluster — every threading branch is testable with a stub. |
| C4 | `watchdogScheduler` | mid | First goroutine extraction. Depends on C2 + C3. The scheduler interface is small (`processOnce`) so it ships with a real test that walks each `processDue*Job` branch via direct call, plus one loop test using the manual clock. |
| C5 | `paneLifecycle` + broker housekeeping | high | Touches `tmux` and the filesystem; needs `tmuxRunner` interface. Must land **before** C6 because C6 borrows the same `tmuxRunner`. |
| C6 | `paneDispatcher` | high | Per-slug goroutines + timing gates + the only message-loss path in the system. Lands after C5 so it can share the runner. The existing `pane_dispatch_queue_test.go` is the regression net. |
| C7 | `headlessWorkerPool` | high | Pure mechanical struct move. Last because the blast radius hits `Launch()` and the resume path; we want every other cluster green and freezeable before we churn this one. |
| C8 | residual `launcher.go` cleanup | trivial | What's left is glue and is the natural endpoint, not a separate PR. |

Hard rule: **no two clusters in the same PR**. Each PR is independently revertible.

---

## 3. Test seams (higher-risk clusters)

### Existing seam to reuse, not reinvent
`launcherSendNotificationToPaneOverride atomic.Pointer[launcherSendNotificationToPaneFn]` (launcher.go:3142) plus the `setLauncherSendNotificationToPaneForTest` helper (test_support.go:184). It already works under `-race` and supports `t.Cleanup` nesting. Pattern:

```go
var fooOverride atomic.Pointer[fooFn]

func setFooForTest(t *testing.T, fn fooFn) {
    t.Helper()
    prior := fooOverride.Load()
    fooOverride.Store(&fn)
    t.Cleanup(func() { fooOverride.Store(prior) })
}
```

Use this exact shape for every new injection point. Don't introduce a context-injected DI container, don't add an interface field on `Launcher` that production must wire — both add ceremony.

### `tmuxRunner` interface (C5, C6)
```go
type tmuxRunner interface {
    Run(args ...string) error
    Output(args ...string) ([]byte, error)
    Combined(args ...string) ([]byte, error)
}
```
Production `realTmuxRunner` shells out via `exec.CommandContext` with the existing `-L tmuxSocketName` prefix baked in. Test `fakeTmuxRunner` records every call into a slice; tests assert on the recorded `args` (exact `send-keys`/`new-window` calls) and inject canned outputs for `list-panes`/`capture-pane`. Wire it via the `atomic.Pointer` seam:

```go
var tmuxRunnerOverride atomic.Pointer[tmuxRunner]
func newTmuxRunner() tmuxRunner {
    if p := tmuxRunnerOverride.Load(); p != nil { return *p }
    return realTmuxRunner{}
}
```

### `clock` interface (C4, C6)
```go
type clock interface {
    Now() time.Time
    After(d time.Duration) <-chan time.Time
    Sleep(d time.Duration)
}
```
Production `realClock` delegates to `time`. Test `manualClock`:
- `Now()` returns the stored time.
- `Advance(d)` adds to the stored time **and** releases any sleepers whose deadline has now passed.
- `Sleep(d)` blocks on a channel registered in a heap keyed by deadline.

This kills the two `time.Sleep` calls inside `runPaneDispatchQueue` (lines 3095, 3104) and the `15s`/`20s` calls in `watchdogSchedulerLoop` (1500, 1503) for tests, while production behaviour is byte-identical.

### Signal channels (the "deterministic signal" the user demands)
Two patterns, both already idiomatic in this codebase:

1. **WaitGroup drain.** `paneDispatcher.Stop()` closes a `stopCh` and `wg.Wait()`s. Tests wanting "queue is fully drained" call `Stop()`. Mirrors the headless worker pool's existing PR #320 pattern.

2. **Per-event signal channel.** Add an unexported field `onSent chan struct{}` to `paneDispatcher`; production leaves it nil; tests set it via a `setOnSentForTest(t, ch)` helper. After each successful pane send, the dispatcher does `if c := p.onSent; c != nil { c <- struct{}{} }`. The test reads from `c` to know "exactly one notification has flushed", with no sleep. Same pattern works for `watchdogScheduler.onTickDone`.

   **Important:** unbuffered. Buffered swallows the signal under back-to-back sends and reintroduces races. If a test wants to count, give it a `chan int` with `len(workers)` capacity and read N times.

### Coverage of the time-window logic without real time
Once the clock interface exists, the coalesce-window test becomes:
```go
clk := newManualClock()
disp := newPaneDispatcher(... clk ...)
sent := make(chan string, 4)
setLauncherSendNotificationToPaneForTest(t, func(_ *Launcher, _, n string) { sent <- n })

disp.Enqueue("eng", "tgt", "first")
<-sent                                    // first send fires immediately
disp.Enqueue("eng", "tgt", "second")     // arrives mid-window
disp.Enqueue("eng", "tgt", "third")      // also mid-window — must coalesce
clk.Advance(paneDispatchCoalesceWindow + time.Millisecond)
got := <-sent
require.Equal(t, "second\n\n---\n\nthird", got)
```
That's the exact regression we'd want a permanent test for, and it has zero `time.Sleep` calls.

---

## 4. Coverage gate — recommendation

**Pick (b): per-file floor on extracted files (≥ 85%), launcher.go untracked during the migration.**

Reasoning:
- (a) per-package floor that ratchets up: the package is currently 62.9%. Extracting C1 (promptBuilder, ~300 lines, currently lightly tested) to 90% will move package coverage by maybe 1–2 points. A ratcheting threshold either has to ratchet by 0.5 points (annoying, easy to game) or it has to wait several PRs for a meaningful jump (defeats the point of per-PR enforcement).
- (b) per-file floor: we know exactly which files are new (`prompt_builder.go`, `office_targets.go`, etc.). CI can run `go test -coverpkg=./internal/team -coverprofile=cov.out ./internal/team/...`, then a tiny script computes per-file coverage from the profile and fails if any new file is < 85%. Old `launcher.go` is exempt while it shrinks. **This is the only option that gates the new code without holding the new code hostage to old code's debt.** 85% (not 90%) so we don't waste cycles testing trivial getters.
- (c) baseline file: works but doesn't enforce that *new* code is well-tested, only that nothing regresses. We want the opposite emphasis right now — push new files high, let launcher.go's number drift up naturally as funcs leave it.

**Implementation:** add `scripts/check-file-coverage.sh` that takes `--min 85 --files internal/team/prompt_builder.go,internal/team/office_targets.go,...`. CI step runs after the existing `bash scripts/test-go.sh`. Add files to the list as PRs land. Six lines of `awk` over `go tool cover -func`.

Once the migration finishes, swap to (a) at the package level set to whatever we hit (likely 78–82%), and retire the per-file script. Two-phase, not permanent ceremony.

---

## 5. Traps

These are the things that will bite if you don't actively defend against them.

1. **`failedPaneSlugs` is shared between targeter and pane-lifecycle.** `recordPaneSpawnFailure` writes it (in C5); `agentPaneTargets`/`skipPaneForSlug` read it (in C2). Currently it's an unsynchronised `map[string]string` — the only reason no race fires is that pane spawn is sequential during `Launch()`. **Fix:** make it `sync.Map` *or* move it onto the `paneLifecycle` type and have the targeter call `panes.IsFailed(slug)`. Don't leave it dangling on the Launcher struct, and don't share a bare map across two new types — that's the kind of split that *introduces* a race the original god-object accidentally avoided.

2. **`paneBackedAgents bool` is the load-bearing flag for the entire dispatch decision.** It's flipped to `true` deep inside `trySpawnWebAgentPanes`; every targeter method short-circuits when it's false (launcher.go:1897). If you move the flag onto `paneLifecycle` but keep the targeter reading the old launcher field, every notification will route through the headless path silently. **Fix:** the targeter holds a `*bool` or, better, a `func() bool` getter that points at the lifecycle's field. Single source of truth.

3. **`launcherSendNotificationToPaneOverride` is global, not per-Launcher.** It's a package-level `atomic.Pointer`. The pane-dispatch tests work because they're sequential and use `t.Cleanup`. If C6 is restructured so the dispatcher holds its own override pointer instead, the existing tests in `pane_dispatch_queue_test.go` and `resume_test.go` (line 706) break. **Fix:** keep the override package-global. The new `paneDispatcher` reads the same `atomic.Pointer` the existing free function reads. Don't move the seam; only move the work.

4. **`NewLauncher` does I/O before returning.** It loads config, reads the manifest, may delete `defaultBrokerStatePath()`, and synthesises `loadRunningSessionMode`. The new sub-types should be constructed **lazily inside `Launch()`**, not in `NewLauncher`, because (a) `Preflight()` runs first and can fail before any sub-type is needed, and (b) tests construct `&Launcher{}` directly and rely on every sub-type being nil-safe. Pattern: `if l.targets == nil { l.targets = newOfficeTargeter(...) }` at the top of `Launch()` and at the top of any test-friendly entry point.

5. **`paneDispatchMinGap` and `paneDispatchCoalesceWindow` are package vars, not consts, specifically so tests can swap them.** Don't promote them to fields on `paneDispatcher` without preserving a swap path — there are tests in the wild that mutate them (grep before moving). The cleanest answer is: keep them as package vars, but have the `paneDispatcher` constructor read them once into instance fields. Tests that want a different value either swap the package var before construction (existing pattern) or pass a configured dispatcher (new pattern). Both work.

6. **`Launch()` has an implicit ordering invariant**: broker start → tmux session create → headless context create → goroutine fan-out. The goroutines (`notifyAgentsLoop`, `notifyTaskActionsLoop`, `notifyOfficeChangesLoop`, `pollNexNotificationsLoop`, `watchdogSchedulerLoop`) are started *unconditionally* at the bottom of `Launch()` except `notifyTaskActionsLoop` which gates on `!isOneOnOne()`. When the scheduler moves to its own type (C4), the `Start(ctx)` call MUST stay inside `Launch()`'s gated block — don't move it into `NewLauncher` or a constructor side effect. Same for the headless worker pool's `ctx, cancel = context.WithCancel(...)` — that line is what makes `Kill()`'s cancel actually cancel anything.

7. **`primeVisibleAgents` and `respawnPanesAfterReseed` straddle C3 and C5.** They read message context (C3 territory) to decide what to type into a pane (C5 territory). Put them on `paneLifecycle` and have it depend on `notificationContextBuilder`, not the other way around. If you put them on the context builder, you're back to one type that knows about tmux.

8. **`atomic.Pointer` overrides leak across parallel tests when keyed globally.** All the existing `*Override` vars are package-global. If anyone runs `t.Parallel()` on tests that set them, they corrupt each other. Today this is fine because the override-using tests don't `t.Parallel()`. If you add new overrides for C5/C6, **document that they must not be combined with `t.Parallel()`**, or build them as per-`*Launcher` fields from day one. Pick one and stick with it; mixing is the worst outcome.

9. **`extractTaskFileTargets` (line 2132) is invoked from broker code paths outside this file.** Confirm with `grep -rn extractTaskFileTargets internal/team` before yanking it from launcher.go. If it has external callers in the package, leave it free-function in a `text_helpers.go` file rather than absorbing it into the context builder type.

10. **Coverage profile aggregation across files:** when you split launcher.go, `go tool cover` will report each new file separately, but funcs that *moved* keep their old function names. Don't be surprised if `launcher.go`'s reported coverage *drops* between extractions — the tested funcs leave for the new file, the untested ones stay behind. That's the desired transient state, not a regression. The per-file gate in §4 makes this visible without flagging it as a failure.

---

## What I'm explicitly not proposing

- **No new public packages.** Every type stays unexported in `internal/team`. The package surface (what `cmd/wuphf` imports) does not change.
- **No interface for `*Broker`.** I named `brokerSnapshotter` and `schedulerBroker` above as **narrow consumer-side interfaces** declared on the consumer side (Go convention). The `*Broker` type itself stays concrete and unchanged. This avoids a 50-method interface that nobody needs.
- **No rename of `Launcher`.** It stays the orchestrator. We're slimming it, not retiring it.
- **No compatibility shims.** `l.officeLeadSlug()` becomes `l.targets.LeadSlug()` everywhere in the package. No deprecation period. The package is internal — there are no external callers to break.

---

## Definition of done for the whole migration

- `launcher.go` is < 1000 lines.
- Every new file has its own `_test.go` with > 85% coverage measured per-file.
- Package coverage for `internal/team` is ≥ 75% (up from 62.9%).
- `go test -race ./internal/team/... -count=10` is green (the `-count=10` is the cheap insurance against the per-slug goroutine races C6 reshapes).
- Zero `time.Sleep` calls in any test we wrote during the migration.
- `Launcher` struct has at most one mutex on it (the rest moved into sub-types).
