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
  harness.py     Harness protocol; FakeHarness (tests/key-free) + ClaudeAgentHarness (P2)
  graph.py       the LangGraph graph: route -> dispatch_turn -> (human_gate | continue)
  wire.py        Go<->Python contract (DispatchRequest / ResumeRequest / StepResult)
  service.py     FastAPI: POST /run, POST /resume, GET /health
tests/           25 tests: lifecycle round-trip/lossiness/adversarial, graph HITL, service, wire
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

## Deferred (next increments — explicit, not hidden)

- **P1b Go side:** a `provider/deepagents`-style dispatch client + the broker-state
  projection write-back + the per-task `orchestrator=langgraph|broker` flag.
- **P2:** `ClaudeAgentHarness.run_turn` — drive Claude Code via the Claude Agent SDK,
  wired to teammcp + broker token; classify the real turn outcome.
- **SSE streaming** of incremental events (the service returns the terminal StepResult today).
- **CI wiring** (pytest + golden wire fixtures) scoped to `orchestrator/**`.

## P1 policy deviations (revisited in P2/P3)

- `route()` treats `drafting/intake/ready/queued/blocked` as IDLE — activation and
  unblock stay the broker's job in P1.
- Plan approval and work approval are one `human_gate` distinguished by `gate_kind`
  (`plan` approve -> running, `review` approve -> approved). Faithful enough for P1;
  the full transition set (dependencies, reviewer resolution) is P2/P3.
