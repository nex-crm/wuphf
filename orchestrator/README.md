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

Caveat (FakeHarness only): without `claude-agent-sdk` installed the service falls
back to `FakeHarness`, whose interrupted step projects `running`, so a wired task
re-dispatches each tick. Install the SDK + set a key to get the real
`ClaudeAgentHarness` (P1b-iv), which returns a real gate state and resolves this.

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
