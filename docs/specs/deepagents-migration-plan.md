# Migration plan: WUPHF orchestration → LangGraph (orchestrator-of-record)

> Branch `worktree-deepagents-harness-eval`. 2026-06-19. Base `origin/main` @ `ec467159`.
> Re-aimed after the founder's clarification: the weak, hand-rolled layer is WUPHF's
> **coordination above** Claude Code/Codex, not the inner loop. The inner CLIs are great
> and stay. Companions: [`deepagents-harness-evaluation.md`](./deepagents-harness-evaluation.md)
> (decision trail) and [`../../spikes/deepagents-seam/REPORT.md`](../../spikes/deepagents-seam/REPORT.md)
> (Python↔teammcp-over-MCP proven). This is the artifact the 4-section eng review runs against.

## 1. Scope & decision (D1–D4)

Replace WUPHF's hand-rolled coordination with a **Python LangGraph orchestrator that is the
orchestrator-of-record** (owns task lifecycle + run-state). Keep **Claude Code/Codex** as the
inner execution, invoked from graph nodes. **Go** becomes the durable-store + transport + tools
+ CLI host. deepagents is optional sugar for the CEO decompose/delegate node.

**Replace (→ LangGraph):** the agent tick/turn-loop (`internal/agent`), and the coordination
logic in `internal/team` — CEO decomposition, multi-agent sequencing, dependency ordering,
the lifecycle state machine, scheduling, escalation.

**Keep:** Claude Code/Codex (inner harness), `teammcp` (76 tools), the durable business store,
the web API/transport, integrations/membrane.

**Explicit non-goals:** ❌ rewrite the inner harness; ❌ rewrite teammcp tools; ❌ replace the
durable business store; ❌ port the *whole* Go backend to Python; ❌ big-bang cutover;
❌ run two orchestrators that co-own the same task (per-task single ownership, §8).

## 2. Why this target is lower-risk than the inner-loop framing

The inner harness is the part hardest to beat and easiest to break. We keep it untouched.
The bet shrinks from "deepagents on our models ≥ Claude Code" (unproven, probably false on day
one) to "LangGraph ≥ WUPHF's hand-rolled coordination" — a low bar you've already called. The
capability risk that dominated the earlier plan is mostly gone.

## 3. Architecture

```
   TS web ◄──► Go host:  business store · API/WS transport · teammcp tools · integrations  (KEEP)
                         ▲ task-status projection            ▲ tools (MCP, broker token)
   ┌─────────────────────┴────────────────────────────────── ┴───────────────────────────┐
   │  Python LangGraph orchestrator   (orchestrator-of-record — NEW)                       │
   │   • task lifecycle = graph state; durable via LangGraph checkpointer                  │
   │   • CEO decompose/plan node (optionally deepagents write_todos + task)                │
   │   • multi-agent sequencing · dependencies · scheduling · escalation                   │
   │   • HITL via interrupt() ─► existing approval surface ─► Command(resume=…)            │
   │             │ each work-step node invokes the GREAT inner harness:                    │
   │             ▼                                                                          │
   │     Claude Code (Claude Agent SDK) / Codex (CLI)  ── use teammcp tools over MCP        │
   └────────────────────────────────────────────────────────────────────────────────────────┘
```

**Data-ownership split (the rule that prevents dual-source-of-truth):**

| Fact | Owner | Notes |
|---|---|---|
| Orchestration / run-state (task graph, node status, pending interrupts, turn cursors) | **LangGraph checkpointer** (NEW store) | "What is running and where." |
| Business records (office members, integrations, wiki, skills, channels/messages, task *records*) | **Go store** (KEEP) | "The company." |
| Web's view of task status | **Projection** | One-way LangGraph → Go task records → web. Web renders unchanged. |

No single fact lives in two stores. Orchestration facts are LangGraph's; business facts are Go's;
the projection is one-way and derived.

## 4. Component inventory

**New**
| Component | Where | Notes |
|---|---|---|
| `orchestrator/` LangGraph app | Python | The graph: lifecycle, decomposition, sequencing, HITL. |
| Checkpointer | Python | Official `langgraph-checkpoint-sqlite`/`-postgres`; this store = run-state source of truth. |
| Node → inner-harness invokers | Python | `claude` node via **Claude Agent SDK**; `codex` node via CLI/exec. Pass teammcp MCP config + broker token. |
| Projection bridge | Go ↔ Python | Orchestrator emits task-status events → Go persists as task records for the web. |
| Run-state migration shim | both | `broker-state.json` lifecycle fields → LangGraph checkpoint (§8, prod fixtures per `TODOS.md`). |
| Supervisor | Go (`internal/runtimebin`) | Start/health/restart the Python orchestrator sidecar. |

**Reused unchanged** — Claude Code/Codex, `teammcp` + broker-token launch pattern, durable
business store, web API/WS, integrations/membrane, `prompt_builder` logic (ported or called).

**Deleted last (P5):** `internal/agent` loop + `internal/team` coordination logic.

## 5. The three seams

**5a. Run-state / checkpointer seam (highest-risk — now spiked, see
[`../../spikes/langgraph-runstate/REPORT.md`](../../spikes/langgraph-runstate/REPORT.md)).**
The spike round-tripped WUPHF's real 13-state lifecycle through a LangGraph checkpointer in
both ownership variants (13/13). It picks the **re-hydrate** variant: the **Go broker record
stays the single source of durable truth**; the LangGraph checkpoint is a *rebuildable cache*
re-hydrated from the Go record on restart. This avoids physically migrating run-state into a
new store, removes dual-source drift, and makes a lost checkpoint recoverable. It still
satisfies D4 (LangGraph decides what runs; Go holds the record). Two rules the spike nailed:
(1) **carry `lifecycle_state` directly, never re-derive from the 4-tuple** — derivation is
lossy for 3 states (`intake→ready`, `decision→review`, `changes_requested→running`) and is a
legacy-only fallback; (2) contradictory legacy tuples **fail loud to `unknown`** for operator
triage, never a silent wrong state.

**5b. Inner-execution seam.** A work-step node invokes the inner harness:
- **Claude:** the **Claude Agent SDK** (Python) drives Claude Code in-process with an MCP config
  (teammcp) + permission mode; cleaner than shelling the CLI from Python.
- **Codex:** CLI/exec with the same MCP config.
- Both get `WUPHF_BROKER_TOKEN` + `WUPHF_BROKER_ADDR` so teammcp tools authenticate to the broker
  (same trust model as today). Node streams inner output back into graph state.

**5c. Web/transport seam.** Keep the web unchanged: Go projects LangGraph run-state into the
existing task/lifecycle shapes the web already consumes, and streams events over the existing WS.
The web does not learn there's a Python orchestrator.

## 6. HITL mapping

Gated nodes call LangGraph `interrupt()`; the orchestrator surfaces it through Go's existing
approval surface (the deterministic-integrations `ExternalActionApprovalCard`). Human decision →
`Command(resume=…)`. Gate policy stays in one place (the broker's action classification), so we
don't fork approval logic.

## 7. Concurrency / scheduling mapping

Today: worktree-keyed lanes; only real `depends_on` serializes; independent tasks run in parallel.
Map onto LangGraph: dependencies → graph edges; independent tasks → concurrent branches; the
worktree-per-task isolation stays (the inner CLI node runs in the task's worktree). Verify
LangGraph's concurrency model honors the "independent ⇒ parallel, dependent ⇒ serialized" rule
with a dedicated test; do not assume it.

## 8. Migration sequencing (strangler-fig; per-task single ownership)

During transition two orchestrators coexist, but **every task is owned by exactly one** — a
per-task `orchestrator=langgraph|broker` flag, never both. That bounds the anti-pattern.

| Phase | Deliverable | Gate |
|---|---|---|
| **P0** ✅ | Seam spike (Python↔teammcp over MCP) | Done. |
| **P1a** ✅ | The `orchestrator/` Python package: real lifecycle model + LangGraph graph (re-hydrate → dispatch turn → transition, HITL via `interrupt()`) + wire contract + FastAPI service + 25 green tests, key-free. | Built — `orchestrator/`, `pytest` green. |
| **P1b-i** ✅ | Go dispatch client (`internal/provider/deepagents.go`): typed wire contract mirroring `wire.py` (snake_case, `schema_version`, env-var-name-only MCP), `DispatchClient.Run`/`Resume`/`Health` against the FastAPI service, projection decode with fail-loud `IsUnknown`, `KindDeepagents` binding kind. Standalone client (NOT a `StreamFn` Entry — the orchestrator owns the whole task, and `StreamFn(msgs,tools)` carries no record to re-hydrate from). Unit-tested against an httptest stand-in for `service.py`. | Built — `go test ./internal/provider/` green. |
| **P1b-ii** ✅ (wiring) | Broker wiring (`internal/team`): per-task `Orchestrator` field (`""`=broker, `"langgraph"`=orchestrator; wire key `orchestrator`, omitempty so existing state is byte-identical); `sendTaskUpdate` routes orchestrator-owned tasks to `DispatchClient.Run` instead of the headless CLI turn; projection write-back via `TransitionLifecycle` (fail-loud on `unknown`/non-canonical); re-hydrate covers the human gate (no separate Resume on this path). Strictly additive — `l.orchestrator` is nil unless `WUPHF_ORCHESTRATOR_URL` is set. Unit-tested with a fake dispatcher (no live sidecar). | Built — `go test ./internal/team/` green. |
| **P1b-iii** ✅ | Close the E2E loop: per-task `orchestrator` flag plumbed through the create paths (`TaskPlanInput`/`plannedTaskInput`, validated to `\|langgraph`); cmd wiring confirmed (`NewLauncher` auto-wires the client when `WUPHF_ORCHESTRATOR_URL` is set); CI-safe Go integration test driving the REAL `DispatchClient` over real HTTP through a real broker task → projection write-back → web wire shape; live cross-language smoke (`orchestrator/scripts/smoke.sh`) against the real Python sidecar. | Done — Go integration test green; live smoke `SMOKE OK` (happy path + fail-loud unknown). **Known stub gap:** on `FakeHarness` an interrupted step projects `running`, so a wired task re-dispatches; P2's real harness resolves it. |
| **P1b-iv** ✅ | Real inner harness (the §5b seam): `ClaudeAgentHarness` drives Claude Code via the Claude Agent SDK (lazy/optional), wired to teammcp with env resolved from names on the orchestrator side. Turn outcome is a **pure `classify_outcome`** keyed on the agent's real `team_task` actions (`complete`/`done`→completed, `submit_for_review`→review, `block`→blocked, planning-without-terminal→plan_ready, else continue). `build_harness` degrades to `FakeHarness` when the SDK is absent (now logged as a warning), so the service stays key-free. | Built — `pytest` green; classifier/env/prompt/degrade all unit-tested without the SDK or a key. Live agent run needs `pip install claude-agent-sdk` + a key. |
| **P2-i** ✅ | Coordination kernel (`orchestrator/coordination.py`): the dependency/sequencing rule the broker owns today, rebuilt as a pure re-hydratable model. `TaskGraph.from_broker_records`; `dependency_resolved` faithful to the broker (only terminal **status** `done/archived` releases — a REJECTED upstream stays blocking); `coordination_action`/`plan` (per-task IDLE/AWAIT/BLOCK/START/DISPATCH/UNKNOWN); `ready_to_dispatch` (the parallel batch); `topological_layers` (independent→parallel, dependent→serial, plan §7); `detect_cycle` (fail loud on a deadlocked decomposition). | Built — `pytest` green; semantics/parallelism/rejection/missing-dep/cycles all unit-tested. |
| **P2-0** ✅ | Self-review hardening (triangulated 5-lens review of the P0–P2-i stack). **Corrects the P1b-iii "stub gap":** the gap was in the graph, not the harness — `_human_gate` interrupted *before* writing the lifecycle state, so every gated turn projected the pre-gate executable state and re-dispatched forever. Fix: split `enter_gate`→`human_gate` so the gate state (`review`/`decision`) is committed before the interrupt suspends; the live smoke now asserts a gated `/run` projects `review`, not `running`. Also: `derived_fields`/`to_projection` made total over `UNKNOWN` (was a reachable `KeyError` from coordination); `schema_version` validated server-side + pydantic `extra="forbid"` (was decorative); `CHANGES_REQUESTED`+`CONTINUE` preserves state; `coordination_action(BLOCKED)`→IDLE; degrade warning. Go side: per-task single-flight + `recoverPanicTo` + shutdown-aware context on the dispatch goroutine; refuse projecting a terminal task backward; stop logging interrupt agent-text. | Built — `pytest` green; live smoke `SMOKE OK` (gate-state + schema-400 + full unknown shape); Go suite green. Deferred (documented): orchestrator-service auth header; `MemorySaver`→sqlite checkpointer. |
| **P2-ii-a** ✅ | Wire the kernel into a goal-level step. New wire shape `CoordinateRequest{goal_id, children[]}` → `CoordinationPlan{actions{task_id→action}, ready[], cycle?}` (extra="forbid" + schema_version, the P2-0 discipline). Orchestrator `POST /coordinate` = pure `coordinate(graph)` over the kernel (cycle → every child BLOCKed). Broker rides the existing dispatch loop (founder pick): a goal (top-level task with children) becoming executable calls `coordinateGoalViaOrchestrator` — enumerates children, sends `depends_on = DependsOn ∪ BlockedOn` (matches the broker's unblock cascade), applies START (`TransitionLifecycle`→running, the notify loop dispatches next tick) / DISPATCH (single-task `/run` now) / BLOCK·IDLE·AWAIT (leave) / UNKNOWN·cycle (fail loud). Per-goal single-flight, panic-recover, shutdown ctx (reuses the P2-0 guards). | Built — `pytest` green; live smoke `SMOKE OK` (coordinate plan); Go provider+team suites green under `-race`, incl. a real-client `/coordinate` E2E through the broker. |
| **P2-ii-b** ✅ | Decompose-turn classification (orchestrator-side, pure). New `TurnOutcome.DECOMPOSED`: `classify_outcome` recognizes a turn that called `team_task action=create` (ranked below an explicit terminal action, above PLAN_READY); `TurnTranscript.decomposed_children()` extracts the ordered child specs (title + `depends_on`) from the create-call inputs (`ChildSpec`). DECOMPOSED is non-gated and lands the goal in RUNNING, so the next tick routes it to the P2-ii-a coordinate path (the children carry their own review gates; the broker already gated plan approval before sub-task creation). No Go change — the broker creates the children as a side effect of the MCP calls and routes goals-with-children to coordinate. | Built — `pytest` green (89); classify priority table + spec extraction + non-gated transition all unit-tested. |
| **P2-ii-c** ✅ | Close the runtime loop (Go-only). Two findings drove it: lifecycle transitions append NO action (only `MutateTask`/unblock/create do), so a goal coordinates once and then stalls; and nothing marks a goal done when its children finish. Fix: (1) **re-tick** — `notifyTaskActionsLoop` re-coordinates a child's parent goal (`retickOrchestratorGoalParent`, guarded to an executable orchestrator goal) on every child action, riding the existing action-driven dispatch loop (no timer, no transition-chokepoint hook). Per-goal single-flight collapses the bursts. (2) **goal completion** — `applyCoordinationPlan` transitions the goal to REVIEW once every child is terminal (computed from current broker state), surfacing it for a final human sign-off; a REVIEW goal is non-executable so it stops re-coordinating. The realistic child→child progression already rides the broker's unblock cascade + per-task dispatch (owned children land/unblock to RUNNING). | Built — `go vet`/`golangci-lint` clean; team suite green under `-race` (re-tick guard table, re-tick fires coordinate, goal completes only when all children terminal). **Gate met in the broker mechanics; end-to-end demonstration needs a live agent run (next).** |
| **P2-ii-d** ✅ | Live agent run (the P2-ii gate, demonstrated) + the bug it caught. Ran real Claude turns through `ClaudeAgentHarness` (SDK driving Claude Code, no key needed — uses Claude Code auth): (1) a planning turn classifies `plan_ready`; (2) real `team_task action=create` calls flow through the production transcript collection → `DECOMPOSED` + extracted child specs; (3) the full `/run` service path → a submit turn hits the review gate (`review`/`interrupted`) and a decompose turn creates two subtasks. **Bug caught:** the harness wired its MCP servers but never **allowed** their tools, so the agent's `team_task` calls were permission-DENIED — classification still fired (the call is in the transcript) but the broker never saw the create/submit side effect; the whole path was inert in production. Fix: `ClaudeAgentHarness._allowed_mcp_tools` grants `mcp__<server>` for each wired server. | Live checks PASS (`scripts/live_*`); `pytest` green (SDK-absent degrade tests skip when SDK present, run in CI; +SDK-present complement). |
| **P3** | HITL interrupts + approval gates via LangGraph. | A task that hits an approval gate pauses and resumes correctly. |
| **P4** | Run-state ownership (re-hydrate, §5a — spiked ✅): Go record stays authoritative, LangGraph rehydrates on restart; carry `lifecycle_state`; prod-fixture tests (`TODOS.md` item 0). | Existing in-flight tasks resume under LangGraph without state loss. |
| **P5** | Expand to all task-types; retire `internal/agent` loop + `internal/team` coordination logic. | Parity sustained across task-types; old coordination deleted. |
| **P6** | Deployment/bundling (Python sidecar + supervisor) incl. Windows. | `wuphf` ships with a working bundled orchestrator on macOS/Linux/Windows. |

Each phase ships behind the per-task flag with E2E verification (global rule: incremental, no big-bang).

## 9. Deployment / bundling

The orchestrator is Python, so a runtime ships with the product (same tax as the earlier plan):
self-contained Python (uv/PEX) bundled beside `wuphf`, supervised by Go (`internal/runtimebin`);
Wails desktop spawns it as a sidecar (compromises "in-process, no sidecar" for the orchestrator,
not the store); container for hosted; **Windows is the highest-risk platform — schedule early in P6.**

## 10. Test strategy

- **Python:** LangGraph graph unit tests (decomposition, ordering, HITL) with a fake inner-harness
  node (no API key, spike pattern); checkpointer migration tests against **production `broker-state.json`
  fixtures** (`TODOS.md` item 0); node→Claude-SDK/Codex integration tests.
- **Go:** projection-bridge tests (LangGraph status → web shapes); supervisor lifecycle tests.
- **Cross-language:** golden fixtures for the projection + run-state shapes; oracle both directions.
- **E2E:** per task-type, a real run through LangGraph vs the broker path, compared; concurrency
  rule (§7) verified explicitly.

## 11. Risks & open questions

| Risk | Mitigation |
|---|---|
| **Run-state migration** — the in-flight-task data move. | **De-risked (spike):** re-hydrate keeps Go authoritative (no data move); carry `lifecycle_state` (lossless 13/13); unmapped tuples fail loud to `unknown`. Remaining: real prod fixtures (`TODOS.md` #0). |
| **Two orchestrators in transition** | Per-task single-ownership flag; never co-owned. |
| **Web churn** | One-way projection keeps the web API shapes unchanged. |
| **Concurrency semantics differ** | Explicit §7 test; don't assume LangGraph matches the lane rule. |
| **Deployment / Windows** | P6 early; PEX/uv; container for hosted. |
| **Capability** | Largely retired — the inner harness (Claude Code/Codex) is unchanged. |
| **Security** | Broker token flows to the Python orchestrator + inner CLIs; same trust as today's CLI; only env-var names on the wire. |

Open: does the CEO node use **deepagents** (write_todos + task) or a plain LangGraph supervisor?
Decide at P2 by prototype; not load-bearing for the architecture.

## 12. First concrete PR (P1)

A `orchestrator/` LangGraph app running one single-agent task-type end-to-end behind a per-task
flag: one Claude work-step node via the Claude Agent SDK (teammcp MCP + broker token), the official
SQLite checkpointer, and a projection bridge that writes task status into the Go store so the existing
web renders it. Reuses the spike's `seam_probe.py` MCP wiring. No change to the inner harness, the
web, or teammcp.
