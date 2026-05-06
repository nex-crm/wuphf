# PR 8 Design Pass — Memory Workflow Gate (TODO #19)

## Status
- Track: notebook-wiki-promise series (PR 8 of 8)
- Type: Design pass, not implementation
- Author: Najmuzzaman Mohammad
- Date: 2026-05-06
- Supersedes: original "5-line filter removal" framing in `~/.gstack/projects/nex-crm-wuphf/najmuzzaman-main-design-20260505-131620-notebook-wiki-promise.md` (Build Order row for PR 8)

---

## Problem

The memory workflow gate in `internal/team/memory_workflow.go` was designed to enforce that every task passing through it has its lookup, capture, and promote steps satisfied before the task is considered complete. In practice the gate fires for fewer than 5% of tasks because `memoryWorkflowRequirementForTask` (lines 193–228) returns `Required: false` for everything except tasks with a `process_research`-family `TaskType` or a `research` type plus explicit prior-context keywords. Every other task gets `Status = not_required` and the gate is effectively dead for the vast majority of broker activity.

Premise #3 of the notebook-wiki-promise design commits to killing this narrow filter so the gate applies universally. The original plan was a 5-line filter removal at lines 212–227, auto-satisfied by the PR 1 `AutoNotebookWriter` writing a `capture`-step artifact inline. Codex's outside-voice review (2026-05-05, decisions OV3A/OV3D) rejected this approach on two grounds: (a) the gate has three distinct steps — lookup, capture, promote — each with its own satisfaction path; satisfying only capture does not satisfy the gate; (b) calling `RecordTaskMemoryCapture` inline from any code path that already holds `b.mu` deadlocks because `RecordTaskMemoryCapture` itself acquires `b.mu` at line 52 of `broker_tasks_memory_workflow.go`. PR 8 is therefore a fresh design pass, not a one-liner.

---

## Premise #3 (closure)

Premise #3 in the notebook-wiki-promise design states: "The narrow `process_research` filter on the memory workflow gate is killed in this design — gate applies to all tasks." The simple "filter removal" framing in the original PR 8 row treated this as deleting the `isProcessResearchTaskType` guard at lines 212–213 of `memory_workflow.go` and toggling `Required: true` globally, then calling `RecordTaskMemoryCapture` from the `AutoNotebookWriter` drain goroutine to auto-satisfy the capture step.

Codex's finding OV3D showed the inline call path is a deadlock: `RecordTaskMemoryCapture` → `b.recordTaskMemoryArtifact` (line 46 of `broker_tasks_memory_workflow.go`) acquires `b.mu.Lock()` unconditionally. Meanwhile, `PostMessage` (broker_messages.go:393) holds `b.mu` for its entire body, including the `emitTaskTransitionAutoNotebook` call at line 656 of `broker.go`, which calls `b.autoNotebookWriter.Handle(...)`. `Handle` enqueues onto a buffered chan and returns immediately — no lock is held across the channel send. But if the design had placed a `RecordTaskMemoryCapture` call inside `AutoNotebookWriter.process`, that goroutine would try to acquire `b.mu` while `PostMessage` (or any other `b.mu`-holding path) was still running, creating a deadlock against the same non-reentrant mutex.

"De-narrowed" means three concrete changes, not one: (1) `memoryWorkflowRequirementForTask` returns `Required: true` for all non-nil tasks with a real owner, not just research-typed ones; (2) a lookup hook runs pre-tool-use to satisfy `MemoryWorkflowStepLookup` for every gated task; (3) a capture hook runs post-tool-use to satisfy `MemoryWorkflowStepCapture`, dispatched asynchronously through the `AutoNotebookWriter` pattern so it never re-enters `b.mu`. Promote remains satisfied by the existing promotion review path (PRs 3–7).

---

## Goal of PR 8

Make the memory workflow gate semantically honest: every task with an owner gets `Required: true`, and all three steps — lookup, capture, promote — are auto-satisfied by deterministic broker-side mechanics, never by prompting the LLM to remember to call a tool. The gate becomes a real invariant, not a narrow research-only heuristic that fires 5% of the time.

---

## Current code anatomy

**Gate trigger and filter:**
- `internal/team/memory_workflow.go:193–228` — `memoryWorkflowRequirementForTask`: the function that decides whether a task needs the full workflow. The narrow `isProcessResearchTaskType` guard lives at line 213; the `researchTaskNeedsPriorContext` heuristic at line 219. These two cases are the only paths that return `Required: true` today.
- `internal/team/memory_workflow.go:239–246` — `isProcessResearchTaskType`: the four-value switch that defines "process research" task types.
- `internal/team/memory_workflow.go:263–414` — `syncTaskMemoryWorkflow`, `refreshMemoryWorkflowStatus`, `refreshMemoryWorkflowStepStatus`: status propagation helpers called by the reconciler.

**Gate satisfaction API (all acquire `b.mu`):**
- `internal/team/broker_tasks_memory_workflow.go:14–36` — `Broker.RecordTaskMemoryLookup`: acquires `b.mu`, finds the task, calls `recordMemoryWorkflowLookup`, calls `saveLocked`.
- `internal/team/broker_tasks_memory_workflow.go:38–76` — `Broker.RecordTaskMemoryCapture` / `Broker.RecordTaskMemoryPromotion` / `Broker.recordTaskMemoryArtifact`: all acquire `b.mu` unconditionally.
- `internal/team/broker_tasks_memory_workflow.go:78–179` — `Broker.handleTaskMemoryWorkflow`: the HTTP handler used by MCP tools that call the gate from agent-side.

**Async write pattern (PR 1, the template for PR 8):**
- `internal/team/auto_notebook_writer.go` — `AutoNotebookWriter`: buffered-chan async writer. `Handle` is a non-blocking enqueue; `process` runs in a dedicated goroutine with no lock acquisition on `b.mu`. This is the correct model for any gate-satisfaction call that cannot hold `b.mu`.
- `internal/team/broker_messages.go:439–452` — `PostMessage`: calls `b.autoNotebookWriter.Handle(...)` while holding `b.mu`, safely, because `Handle` never re-enters `b.mu`.
- `internal/team/broker.go:625–668` — `emitTaskTransitionAutoNotebook` and `pendingTaskTransition`: the OV2A canonical seam. Called after `saveLocked` from mutation paths.

**Reconciler (background repair loop):**
- `internal/team/memory_workflow_reconciler.go:230–243` — `Broker.startMemoryWorkflowReconcilerLoop`: ticks every `memoryWorkflowReconcileInterval` (10 minutes), calls `ReconcileMemoryWorkflows`.
- `internal/team/memory_workflow_reconciler.go:245–283` — `Broker.ReconcileMemoryWorkflows`: snapshot-under-lock, reconcile-without-lock, write-back-under-lock pattern. Safe against the deadlock because reconcile work runs outside `b.mu`.

**Notebook search (the lookup backend):**
- `internal/team/notebook_worker.go:224–280` — `WikiWorker.NotebookSearch`: literal substring search scoped to a single agent's shelf (`agents/{slug}/notebook/`). Has its own mutex inside `WikiWorker`, independent of `b.mu`.
- `internal/team/broker_notebook.go:439–490` — `Broker.handleNotebookSearch`: HTTP handler. Calls `wikiWorker.NotebookSearch` outside `b.mu`.
- `internal/team/broker_streams.go:324–330` — `Broker.WikiIndex`: accessor for the `*WikiIndex`. Returns under `b.mu` but the index itself has its own internal mutex.

---

## Deadlock hazard

The deadlock scenario, step by step:

1. An HTTP request (agent turn) enters `Broker.PostMessage` at `broker_messages.go:393`.
2. `PostMessage` calls `b.mu.Lock()` at line 393 and holds it for the function's entire body via `defer b.mu.Unlock()`.
3. Inside that critical section, `PostMessage` calls `b.autoNotebookWriter.Handle(...)` at line 443. `Handle` does a non-blocking channel send and returns immediately — no additional lock acquired. Safe.
4. Now suppose the original PR 8 design had placed a `b.RecordTaskMemoryCapture(...)` call inside `AutoNotebookWriter.process`. The drain goroutine calls `RecordTaskMemoryCapture` → `recordTaskMemoryArtifact` at `broker_tasks_memory_workflow.go:52` → `b.mu.Lock()`.
5. Goroutine 1 (PostMessage) holds `b.mu`. Goroutine 2 (drain) waits to acquire `b.mu`. Goroutine 1 is waiting for nothing — but `b.mu` is a `sync.Mutex`, which is not reentrant. If any subsequent operation in Goroutine 2 needs the lock that Goroutine 1 holds without a path to release it, both goroutines park forever.
6. In practice, a second path can also trigger this: `ReconcileMemoryWorkflows` (reconciler goroutine) acquires `b.mu` at line 246 of `memory_workflow_reconciler.go`. If the drain goroutine tries to acquire `b.mu` while the reconciler holds it, same deadlock.

The deferred-emit pattern (`AutoNotebookWriter.Handle` + dedicated drain goroutine) avoids this entirely: the drain goroutine never calls any `b.mu`-acquiring method. Its only broker interaction is through the `autoNotebookWriterClient` interface (`NotebookWrite`), which goes through `WikiWorker` and its own independent queue, never touching `b.mu`.

The correct PR 8 pattern mirrors this exactly: gate-satisfaction calls that need `b.mu` must come from outside the drain goroutine — either (a) from a separate async writer that makes the call only after acquiring `b.mu` itself, or (b) from the HTTP handler for the relevant MCP tool (already the pattern for lookup and capture in `handleTaskMemoryWorkflow`), or (c) via the reconciler loop, which already takes the snapshot-reconcile-write-back pattern safely.

---

## Proposed design

### High-level shape

The gate has three responsibilities, not one: lookup, capture, and promote. Each is a separate hook point with a separate satisfaction path. They share one invariant: no gate-satisfaction call may be made from inside a goroutine that holds or is called from a path holding `b.mu`, unless that call is also under `b.mu` and is itself a `saveLocked`-gated write (the existing broker mutation pattern).

The gate's `Required` field is widened: every task with a non-empty `Owner` satisfies the new `memoryWorkflowRequirementForTask` predicate. Auto-satisfaction is the default for all three steps so the gate never blocks task completion in normal operation.

### Lookup

**Trigger:** every MCP `notebook_search` call from an agent that has an active gated task.

**Mechanism:** `Broker.handleNotebookSearch` (`broker_notebook.go:443`) is the HTTP entry point for agent-side `notebook_search` tool calls. After the search completes, if the request carries a non-empty `X-WUPHF-Task-ID` header (or a `task_id` body field), the handler calls `b.RecordTaskMemoryLookup(taskID, actor, query, citations)`. This call is already correct: `handleNotebookSearch` does not hold `b.mu` when it runs (it is an HTTP handler that acquires the lock only briefly to fetch the wiki worker, then releases it before doing search work). `RecordTaskMemoryLookup` acquires `b.mu` internally, which is safe at this point.

**Output:** `MemoryWorkflow.Citations` is populated; `refreshMemoryWorkflowStepStatus` marks `wf.Lookup.Status = satisfied` on the next reconcile pass.

**Lock discipline:** read-only notebook scan runs outside `b.mu`. `RecordTaskMemoryLookup` acquires `b.mu` for the write. No nested lock.

**What changes:** add `task_id` field to the `NotebookSearchRequest` type; thread it through `handleNotebookSearch` to the `RecordTaskMemoryLookup` call site. No new types or goroutines.

**Invariant:** if an agent never calls `notebook_search` on a task, the lookup step stays `pending`. That is acceptable: the reconciler can auto-satisfy lookup for tasks where the auto-notebook writer has already written a matching entry (see Capture below), by treating the existence of a notebook entry for the task as implicit evidence a lookup occurred.

### Capture

**Trigger:** every `AutoNotebookWriter.process` call that successfully writes a notebook entry (`written` counter increments) AND whose event carries a non-empty `TaskID`.

**Mechanism:** `AutoNotebookWriter.process` (`auto_notebook_writer.go:303`) already writes the entry. After a successful `NotebookWrite` call, the writer enqueues a lightweight signal onto a new `captureSignalCh chan autoNotebookCaptureSignal` (buffered, cap 64, same pattern as the writer's own queue). A separate goroutine — `captureSignalDrainer` — consumes from `captureSignalCh` and calls `b.RecordTaskMemoryCapture(taskID, actor, artifact)` where `artifact` carries the notebook path and commit SHA. This goroutine acquires `b.mu` inside `RecordTaskMemoryCapture` normally; it does not hold `b.mu` entering the call.

**Why a second goroutine instead of calling directly from `process`?** `process` runs in the `AutoNotebookWriter` drain goroutine, which must stay fast and must never block on `b.mu` (it could be called while `PostMessage` holds the lock). The `captureSignalDrainer` goroutine is the isolation boundary: `process` does a non-blocking send to `captureSignalCh` (drop-on-full, same as the event queue), and `captureSignalDrainer` does the blocking `b.mu`-acquiring work at its own pace.

**Contract:** `autoNotebookCaptureSignal` struct carries: `TaskID string`, `Actor string`, `NotebookPath string`, `CommitSHA string`, `Timestamp string`. Populated by `process` from the `autoNotebookEvent.TaskID` and the SHA returned by `NotebookWrite`.

**Lock discipline:** `process` holds `AutoNotebookWriter.mu` (dedupe bucket lock) but never `b.mu`. The channel send is non-blocking. `captureSignalDrainer` acquires `b.mu` inside `RecordTaskMemoryCapture`, no outer lock held at that point.

**Lifecycle:** `captureSignalDrainer` goroutine starts in `AutoNotebookWriter.Start`, exits on `stopCh`. Shares the existing `done` chan via a `sync.WaitGroup` added to `AutoNotebookWriter.Stop`.

**What changes:** `autoNotebookEvent` gains a `TaskID` field (already present in the struct at line 70 of `auto_notebook_writer.go`, already threaded through `emitTaskTransitionAutoNotebook`). `AutoNotebookWriter` gains `captureSignalCh` field, `captureSignalDrainer` goroutine, and a reference to a `memoryCaptureSink` interface:

```
type memoryCaptureSink interface {
    RecordTaskMemoryCapture(taskID, actor string, artifact MemoryWorkflowArtifact) (teamTask, bool, bool, error)
}
```

`Broker` satisfies `memoryCaptureSink` via its existing `RecordTaskMemoryCapture` method. Tests substitute a fake.

### Promote

**Trigger:** cumulative demand score crosses threshold (PR 3 demand pipeline) OR agent explicitly calls `notebook_promote` MCP tool (existing PR 3+7 path).

**Mechanism:** no change in PR 8. The existing `handleNotebookPromote` → `ReviewLog` → promotion state path already calls `RecordTaskMemoryPromotion` when a promotion completes. The reconciler's `repairPromotionsFromCapture` (`memory_workflow_reconciler.go:158`) also auto-populates promotion artifacts from matching captures found in `ReviewLog`. PR 8 does not add a new promote auto-satisfaction path.

**What changes:** none. Promote is already wired; the only missing piece was that no captures were arriving to trigger it. Once Capture above works, the reconciler's existing logic surfaces them as promotion candidates.

### Anti-pattern: inline call from b.mu

The rejected pattern, preserved for reference:

```
// WRONG — deadlocks when PostMessage holds b.mu
func (w *AutoNotebookWriter) process(ctx context.Context, evt autoNotebookEvent) {
    // ... writes notebook entry ...
    if evt.TaskID != "" {
        // b is the broker; b.mu is not reentrant
        b.RecordTaskMemoryCapture(evt.TaskID, ...)  // DEADLOCK: RecordTaskMemoryCapture acquires b.mu
    }
}
```

`PostMessage` (broker_messages.go:393) holds `b.mu` via `defer b.mu.Unlock()` for the entire function body, which includes calling `b.autoNotebookWriter.Handle(...)`. `Handle` enqueues the event. Later, `AutoNotebookWriter.process` runs in its own goroutine, no lock held. If `process` calls `RecordTaskMemoryCapture`, that function acquires `b.mu` at `broker_tasks_memory_workflow.go:52`. If `PostMessage` is still in its critical section on another goroutine (or any other `b.mu`-holding path is active), `process` blocks waiting for the lock. Since `PostMessage` will eventually release and `process` will eventually acquire, this is not a textbook deadlock in the single-path case. However: if the broker is under high concurrency with many agents posting simultaneously, `process` can be starved; more critically, if the reconciler loop (`memory_workflow_reconciler.go:246`) or any other path calls `RecordTaskMemoryCapture` while also holding something `process` is waiting for, a real cycle forms. The deferred-channel pattern eliminates the ambiguity entirely by ensuring `process` never acquires `b.mu`.

---

## What changes vs PRs 1–7

**Already shipped (PRs 1–7):**
- PR 1: `AutoNotebookWriter` drains broker events into per-agent notebook entries. `emitTaskTransitionAutoNotebook` is the task-transition hook. `PostMessage` has the `Handle` call. `autoNotebookEvent.TaskID` is already populated for task-transition events.
- PR 2: human "remember" intent classifier → `team_wiki_write` direct path. No memory workflow gate interaction.
- PRs 3–6: demand pipeline, promotion ranking, channel intent, hourly sweep. These feed the promote step via `ReviewLog` and `notebook_promote`.
- PR 7: prompt-side reinforcement — agents are instructed to call `notebook_search` with a task_id. Prompt matches the system contract PR 8 makes real.

**PR 8 adds:**

1. **`memoryWorkflowRequirementForTask` widened** (`memory_workflow.go:193–228`): remove the `isProcessResearchTaskType` / `researchTaskNeedsPriorContext` guards. Any task with a non-empty `Owner` field returns `Required: true, Steps: [lookup, capture, promote]`. Tasks with no owner continue to return `Required: false` (no shelf to land on).

2. **Lookup hook in `handleNotebookSearch`** (`broker_notebook.go:443`): accept optional `task_id` in the request. After a successful search, if `task_id` is present and non-empty, call `b.RecordTaskMemoryLookup(taskID, actor, query, citations)`. This is a pure additive change to the HTTP handler; no new types, no goroutines.

3. **`memoryCaptureSink` interface + `captureSignalCh` channel in `AutoNotebookWriter`** (`auto_notebook_writer.go`): new field `captureSink memoryCaptureSink`, new buffered chan `captureSignalCh chan autoNotebookCaptureSignal`, new goroutine started in `Start`. `process` sends to `captureSignalCh` after a successful `NotebookWrite` when `evt.TaskID != ""`. The drainer goroutine calls `captureSink.RecordTaskMemoryCapture`.

4. **`NewAutoNotebookWriter` signature change**: accepts optional `captureSink memoryCaptureSink` (nil-safe; nil disables capture signaling). Broker passes itself. Tests pass a fake or nil.

Nothing in PRs 1–7 is removed or changed except the additive `task_id` field in the notebook search request.

---

## Failure modes

1. **`captureSignalCh` saturates under burst.** If the broker is processing many task transitions and `captureSignalDrainer` cannot keep up (e.g., `saveLocked` is slow), signals drop. Mitigation: the counter pattern from `AutoNotebookWriter` (add a `captureSaturated atomic.Int64` counter); the task's capture step stays `pending` and the reconciler's 10-minute loop re-derives it from the notebook path on disk via `repairCaptureArtifacts`. No data loss: the notebook entry was already written; the reconciler is the safety net.

2. **Widening `Required: true` surfaces pending gates on old tasks.** Tasks created before PR 8 that have already completed will suddenly show `MemoryWorkflow.Status = pending` after the `memoryWorkflowRequirementForTask` change is deployed. Mitigation: the reconciler's `syncTaskMemoryWorkflow` call already runs on every reconcile tick; it will populate the gate for old tasks. For tasks that are already `done` in the task list, add a guard: `memoryWorkflowRequirementForTask` returns `Required: false` if `task.Status == "done"` and `task.MemoryWorkflow == nil` (i.e., the gate was never initialized, meaning the task predates PR 8). New completed tasks get the gate initialized at creation time and auto-satisfied before completion.

3. **Agent never calls `notebook_search` on a short task.** The lookup step stays pending. Mitigation: the reconciler auto-satisfies lookup for any task whose `MemoryWorkflow.Captures` is non-empty by treating "capture happened" as evidence that a search occurred (implicit lookup). Specifically: `repairTaskWorkflow` can be extended to call `recordMemoryWorkflowLookup` with a synthetic `ContextCitation` pointing at the capture artifact's path when `wf.Captures` is non-empty and `wf.Citations` is empty.

4. **`task_id` absent from `notebook_search` call.** The MCP tool does not require it. Agents that do not pass it get no lookup step satisfaction from the HTTP path. Mitigation: the PR 7 prompt instructions already include the `task_id` guidance; the reconciler implicit-lookup path (failure mode 3 above) covers the rest. This is belt-and-suspenders: lookup satisfaction via two paths.

5. **Reconciler write-back clobbers a concurrent capture.** `ReconcileMemoryWorkflows` does a snapshot-outside-lock, reconcile, then `reconciledTaskNewer` check before write-back. If a `RecordTaskMemoryCapture` landed between snapshot and write-back, `reconciledTaskNewer` compares `UpdatedAt` timestamps. The capture write is more recent; write-back is skipped for that task. Correct behavior already in the existing reconciler logic.

---

## Test plan

All new tests live in `internal/team/` following the existing table-driven Go test conventions. Each of the three gate steps must be independently testable.

**Lookup hook (`broker_notebook_test.go` or a new `broker_notebook_memory_workflow_test.go`):**
- `TestHandleNotebookSearch_WithTaskID_RecordsLookup`: POST `/notebook/search` with a `task_id` field; assert `task.MemoryWorkflow.Lookup.Status == "satisfied"` and `Citations` is non-empty.
- `TestHandleNotebookSearch_WithoutTaskID_NoLookupRecord`: same path, no `task_id`; assert gate is untouched.
- `TestHandleNotebookSearch_TaskIDNotFound_NoError`: non-existent `task_id`; assert handler returns 200 (search still works), gate unaffected.

**Capture signal path (`auto_notebook_writer_test.go`):**
- `TestAutoNotebookWriter_CaptureSignal_SentOnSuccessfulWrite`: construct `AutoNotebookWriter` with a fake `memoryCaptureSink`; emit an event with `TaskID` set; wait on `WaitForCondition` checking `Written == 1`; assert `fake.RecordTaskMemoryCaptureCalled == 1`.
- `TestAutoNotebookWriter_CaptureSignal_NotSentOnEmptyTaskID`: emit event with `TaskID == ""`; assert `fake.RecordTaskMemoryCaptureCalled == 0`.
- `TestAutoNotebookWriter_CaptureSignal_DropOnSaturation`: fill `captureSignalCh`; emit one more event; assert `captureSaturated.Load() == 1`, no panic, `Written` still increments.

**`memoryWorkflowRequirementForTask` widening (`memory_workflow_test.go`):**
- `TestMemoryWorkflowRequirementForTask_NonResearchWithOwner_Required`: task with `Owner = "ceo"`, `TaskType = "general"`; assert `Required: true`.
- `TestMemoryWorkflowRequirementForTask_NoOwner_NotRequired`: task with `Owner = ""`; assert `Required: false`.
- `TestMemoryWorkflowRequirementForTask_DoneTaskNoGate_NotRequired`: task with `Status = "done"`, `MemoryWorkflow == nil`; assert `Required: false` (backward compat guard).

**Promote (regression, no new tests):** existing `broker_review_test.go` and `memory_workflow_reconciler_test.go` suite must pass unchanged. The promote path is untouched.

**Integration test (`broker_notebook_test.go` or a new integration file):**
- `TestMemoryWorkflowGate_EndToEnd`: create a task with owner; emit a task-transition event via `emitTaskTransitionAutoNotebook`; POST `notebook_search` with the task ID; wait for capture signal to drain; call `ReconcileMemoryWorkflows`; assert `task.MemoryWorkflow.Status == "satisfied"` (lookup + capture both auto-satisfied; promote auto-satisfied via implicit path or a `notebook_promote` call).

---

## Out of scope (deferred)

- **LLM-gated capture classifier.** PR 8 uses the notebook write itself as the capture signal; there is no LLM call to judge whether a tool result is "worth capturing." A future pass can add a classifier that gates the `captureSignalCh` send on a lightweight LLM quality check.
- **Per-tool whitelist for capture.** Every successful notebook write triggers a capture signal regardless of which MCP tool caused the agent's turn. Start with this wide coverage; tune based on production signal-to-noise (the same metric TODO #18 will surface).
- **Cross-agent lookup.** The lookup hook in `handleNotebookSearch` covers searches within the calling agent's own shelf. Cross-agent lookup (agent A searching agent B's shelf) is what PR 5's channel intent classifier handles on the demand side. PR 8's lookup hook applies to any `notebook_search` call regardless of whose shelf is searched — the `task_id` threading is agnostic to scope. Cross-agent lookup satisfaction therefore falls out for free as long as the calling agent passes `task_id`.
- **`MemoryWorkflowStatusSatisfied` as a task-completion gate.** Today, a task's `done` status does not block on `MemoryWorkflow.Status`. PR 8 does not change this. A future enforcement pass can add a pre-completion check that refuses a `done` transition if `MemoryWorkflow.Status != satisfied` (with an override escape hatch via `MemoryWorkflowOverride`).

---

## Open questions for eng review

1. **Implicit lookup via capture — correct or too loose?** Failure mode 3 proposes auto-satisfying lookup when captures are present. This is pragmatic but conflates "the agent wrote a notebook entry" with "the agent searched before writing." Is there a cleaner invariant — e.g., require that at least one `notebook_search` call happened with this `task_id` before allowing implicit satisfaction, and surface a separate `lookup_skipped` state?

2. **`captureSignalCh` capacity.** The event queue uses 256 (decision 6A, based on "minutes of headroom under burst"). The capture channel is downstream of writes, not messages, so its burst rate is lower. 64 is proposed. Is there a principled way to derive this from the event queue depth and write throughput, or should it match 256 for symmetry?

3. **Backward compatibility guard for old `done` tasks.** The proposed guard — `Required: false` when `task.Status == "done"` and `task.MemoryWorkflow == nil` — prevents surfacing pending gates on pre-PR-8 completed tasks. But it also means tasks that were completed without the gate and are later re-opened (status reset to `in_progress`) will get a gate initialized from scratch. Is that the right behavior, or should re-opened tasks be exempt from the gate for one reconcile cycle?

4. **Reconciler interval vs. gate latency.** The reconciler ticks every 10 minutes (`memoryWorkflowReconcileInterval`). After PR 8, the capture signal drainer satisfies the capture step in near-realtime, but the reconciler is the path that refreshes `MemoryWorkflow.Status` to `satisfied`. Should PR 8 add a targeted `ReconcileMemoryWorkflows` call immediately after a capture signal is processed, or is the 10-minute lag acceptable given that the gate is not enforced as a task-completion blocker?

5. **`task_id` in notebook search: header or body?** The current `handleNotebookSearch` handler reads search parameters from query string (`q=`, `slug=`). Adding `task_id` as a query param is the smallest change. The MCP tool contract would need to be updated to pass it. Is there a reason to prefer a request body (POST instead of GET) to keep the task correlation private, or is the current GET-with-query-param model acceptable for what is effectively observability metadata?

---

## Estimated effort

| Area | Files | Delta LOC | Notes |
|---|---|---|---|
| `memoryWorkflowRequirementForTask` widening | `memory_workflow.go` | ~10 lines removed, ~5 added | Remove `isProcessResearchTaskType` guard; add `owner` check; add `done`+`nil` backward compat guard |
| Lookup hook in `handleNotebookSearch` | `broker_notebook.go` | ~20 lines | Add `task_id` field read, conditional `RecordTaskMemoryLookup` call |
| `memoryCaptureSink` interface + `captureSignalCh` | `auto_notebook_writer.go` | ~60 lines | New interface, new field, new struct, `captureSignalDrainer` goroutine, counter |
| `NewAutoNotebookWriter` signature | `auto_notebook_writer.go`, `broker_wiki_lifecycle.go` | ~10 lines | Optional sink arg; pass `b` from lifecycle |
| Tests | `broker_notebook_test.go` (or new file), `auto_notebook_writer_test.go`, `memory_workflow_test.go` | ~200 lines | 10 test functions across 3 files |
| **Total** | 4–5 files | ~300 lines net | Well within the 400-line typical / 800-line max file budget |

Sweat: low. Every pattern is a direct extension of PR 1's `AutoNotebookWriter` model. No new architectural primitives. The hardest part is the backward-compat guard for existing `done` tasks — get that wrong and the reconciler floods old completed tasks with spurious `pending` gates.
