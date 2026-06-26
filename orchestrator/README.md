# wuphf-orchestrator

LangGraph orchestrator that owns WUPHF's task lifecycle + coordination on top of
Claude Code/Codex. The Go broker stays the durable host; this orchestrator
re-hydrates run-state from the Go record each step (the P4 spike decision). It is
the production home of the logic the two spikes validated.

See `docs/specs/deepagents-migration-plan.md` for the architecture and phase plan.

## Layout

```
src/orchestrator/
  lifecycle.py   real lifecycle model (13 states, forward/migration maps, migrate_record)
                 + P1 orchestration policy (route / outcome+decision transitions)
  runstate.py    TaskRun schema; from_broker_record (re-hydrate) + to_projection (-> Go)
  coordination.py multi-task dependency/sequencing kernel (P2): TaskGraph re-hydrate,
                 plan()/ready_to_dispatch (parallel batch), topological_layers, detect_cycle,
                 coordinate() -> the per-child action plan POST /coordinate returns (P2-ii)
  harness.py     Harness protocol; FakeHarness (tests/key-free) + ClaudeAgentHarness (P1b-iv)
  graph.py       the LangGraph graph: route -> dispatch_turn -> (human_gate | continue)
  wire.py        Go<->Python contract (DispatchRequest / ResumeRequest / StepResult)
  service.py     FastAPI: POST /run, POST /resume, GET /health
tests/           lifecycle round-trip/lossiness/adversarial, graph HITL, service, wire,
                 harness (classify_outcome / env resolution / degrade-safe)
```

## Run

```bash
uv venv .venv
uv pip install langgraph langgraph-checkpoint-sqlite fastapi sse-starlette uvicorn pydantic pytest httpx
.venv/bin/pytest -q
.venv/bin/uvicorn orchestrator.service:app --app-dir src   # local service
```

## What lands in this increment (P1 foundation)

- The real lifecycle model + migration, faithful to `broker_lifecycle_transition.go`.
- The orchestration graph: re-hydrate -> dispatch a turn to the inner harness ->
  transition, with human gates via LangGraph `interrupt()`.
- The Go<->Python wire contract (secrets cross as env-var *names* only).
- The FastAPI service surface the Go dispatch client will target.
- Runs green with **no model key** (FakeHarness drives the loop).

## Live agent verification (real Claude turns)

These prove the inner-harness and service paths against a **real** Claude agent
(via the Claude Agent SDK driving Claude Code). Install the SDK first and run with
`PYTHONPATH=src`:

```bash
uv pip install --python .venv/bin/python claude-agent-sdk   # the `claude` extra
PYTHONPATH=src .venv/bin/python scripts/live_harness_check.py    # 1: real turn -> classify (plan_ready)
PYTHONPATH=src .venv/bin/python scripts/live_decompose_check.py  # 2: real team_task create calls -> DECOMPOSED + child specs
PYTHONPATH=src .venv/bin/python scripts/live_service_check.py    # 3: full /run service path -> review gate + decompose
```

`scripts/stub_team_task_mcp.py` is a tiny stdio MCP `team_task` stand-in so the
checks need no broker. The service check surfaced a real bug: the harness must
**allow its wired MCP tools** (`allowed_tools=[mcp__<server>]`) — without it the
agent's `team_task` calls appear in the transcript (so classification still fires)
but are permission-DENIED, so the broker never sees the create/submit side effect.
Fixed in `ClaudeAgentHarness._allowed_mcp_tools`.

## Live cross-language smoke

`bash scripts/smoke.sh` boots the sidecar and POSTs the exact wire shapes the Go
`DispatchClient` sends, asserting the `StepResult`/projection contract (happy path
+ fail-loud `unknown`). Run it from `orchestrator/` after building the venv.

## Running the full broker → orchestrator path (E2E)

1. Boot the sidecar: `.venv/bin/uvicorn orchestrator.service:app --app-dir src --port 8770`.
2. Point the broker at it: `export WUPHF_ORCHESTRATOR_URL=http://127.0.0.1:8770`
   before launching `wuphf` — `NewLauncher` wires the dispatch client automatically
   when this is set (nil otherwise, so default installs are unchanged).
3. Create ONE task with `orchestrator: "langgraph"` (the new-task composer field /
   `team_task` plan input). When it becomes executable, the broker routes it to
   `POST /run` instead of a headless CLI turn and writes the projection back.

Gate behavior (both harnesses): a turn that submits for review or readies a plan
interrupts at a human gate and projects the GATE state (`review` / `decision`),
which is non-executable — so the broker surfaces the gate and does NOT re-dispatch.
The human resolves it through the broker's existing approval path; the next
dispatch re-hydrates the moved-forward record. Without `claude-agent-sdk` the
service degrades to `FakeHarness` (logged as a warning) so it stays runnable
key-free; install the SDK + set a key for real `ClaudeAgentHarness` turns.

## Deferred (next increments — explicit, not hidden)

- ~~**P1b Go side**~~ — **done** (PRs: dispatch client, broker routing, E2E loop).
- ~~**P1b-iv:** `ClaudeAgentHarness.run_turn`~~ — **done.** Drives Claude Code via the
  Claude Agent SDK (lazy/optional), wired to teammcp; outcome is classified by a
  pure `classify_outcome` keyed on the agent's real `team_task` actions. Live runs
  need `pip install claude-agent-sdk` + a key; without the SDK the service degrades
  to `FakeHarness`.
- **SSE streaming** of incremental events (the service returns the terminal StepResult today).
- **CI wiring** (pytest + golden wire fixtures) scoped to `orchestrator/**`.

## P1 policy deviations (revisited in P2/P3)

- `route()` treats `drafting/intake/ready/queued/blocked` as IDLE — activation and
  unblock stay the broker's job in P1.
- Plan approval and work approval are one `human_gate` distinguished by `gate_kind`
  (`plan` approve -> running, `review` approve -> approved). Faithful enough for P1;
  the full transition set (dependencies, reviewer resolution) is P2/P3.
