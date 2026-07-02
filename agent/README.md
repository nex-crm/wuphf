# wuphf-agent (pi-mono build agent)

The BUILD half of the operator harness, on the **pi-mono** stack (`@mariozechner/pi-ai`):
plain-language description → a deterministic `WorkflowSpec`. **Key-free, multi-provider.**

This is the engine — the operator backend is fully pi-mono (see
`docs/specs/operator-harness-clean-start.md` §Engine decision; the Python/deepagents
`harness/` fallback has been removed). It replaces driving `claude -p`/`codex` via
stdout: pi-ai is an embeddable TypeScript SDK over one abstraction for **subscription
OAuth** (Claude Pro/Max, ChatGPT/Codex, Copilot — `/login`, no API key), **BYOK** (env
keys), and **local/open-weight** (Ollama → e.g. Hermes). Same `WorkflowSpec` contract
as the FE (`web/src/operator/mock/data.ts`), behind the `/build/stream` shape.

## Layout

```text
src/wire.ts        WorkflowSpec/Step + Build/Run request + RunResult; the build prompt; extractJson; validateSpec
src/model.ts       model resolution: Ollama (key-free default) | subscription | BYOK
src/buildAgent.ts  buildWorkflow(message) -> WorkflowSpec via pi-ai `complete`; streamWorkflow()
src/executor.ts    deterministic run; a step with an `api` is REPLAYED as a real HTTP call (auth resolved from a named ref); a gated step halts for the approval card (CQ1); a failed call halts with error
src/sniff.ts       browsersniff: HAR -> ApiCall/WorkflowStep; strips secrets, auth->named ref, classifies stable-key vs rotating-session (A3/A4)
src/providers.ts   inference-path detection for the FE Settings surface (subscription / BYOK / local)
src/service.ts     Bun.serve HTTP/SSE: /health, /providers, POST /build/stream (SSE), POST /run,
                   /tools/build + /tools/call, and the persistence routes (below)
src/store.ts       file-backed per-agent store: tools, routines, sessions, artifacts
                   (WUPHF_AGENT_DATA_DIR, default .wuphf-agent-data/; atomic-ish writes)
src/routineRunner.ts  run a routine = match-or-author a tool, run it approved:false,
                   append the transcript, save the md run artifact
src/scheduler.ts   interval tick over due routines (ROUTINE_SCHEDULER=1 to enable;
                   ROUTINE_TICK_MS, default 30s); schedule labels, not cron
src/runContext.ts  per-run AbortSignal (AsyncLocalStorage) so a settled tool run
                   aborts in-flight real capability calls
src/run.ts         CLI runner (compile a description live)
scripts/smoke.sh   live smoke (boots the service; /build/stream key-free against Ollama)
src/*.test.ts      pure + service tests (offline; the service test stubs the engine)
```

## Run

```bash
bun install
bun test            # pure-logic tests (no model)
bun run typecheck
# live, key-free, against a local model:
ollama pull qwen2.5-coder:1.5b   # or any local model; bigger = better specs
bun run build:run "When a demo request comes in, score it; if over $5k route to the sales Slack channel, else nurture"
```

## Auth (production)

The operator runs pi `/login` once (Claude Pro/Max, ChatGPT/Codex, or Copilot) — no API
key. pi-ai's OAuth is **Node-only** (not browser), so the agent runs in the Electron main
process / a Node service, never the renderer. `HARNESS_PROVIDER` + `HARNESS_MODEL` select
the model; default is local Ollama so it always runs out of the box.

Note: Anthropic now bills third-party subscription use as per-token "extra usage" — BYOK or
ChatGPT/Codex/Copilot subscriptions are the cost-neutral paths (badlogic/pi-mono#3372).

## Service

```bash
bun run serve     # http://127.0.0.1:8820  (PORT to override)
bun run smoke     # boots it + exercises /health /providers /run /build/stream (live, key-free)
```

The FE points its build/run calls here (same `WorkflowSpec` contract as the mock). This
is the only operator backend — the Python `harness/` (deepagents) has been removed.

### Persistence + routines routes (slice 2)

Per-agent state persists as one JSON file per agent id (`src/store.ts`). Wire
shapes live in `src/wire.ts` (`StoredTool`, `Routine`, `SessionMeta`,
`SessionMessage`, `StoredArtifact`).

```text
POST  /tools/build             `app` set -> persist the authored tool under that agent
                               (re-authoring a same-named tool bumps `version`)
GET   /tools?agent=<id>        { tools: StoredTool[] }
GET   /routines?agent=<id>     { routines: Routine[] }
POST  /routines                { agent, name, prompt, schedule } -> { routine } (+ its chat session)
PATCH /routines/<id>           { agent, enabled?, prompt?, publish? } -> { routine }
                               (prompt edit -> draft; publish -> vN+1, draft cleared)
POST  /routines/<id>/run       { agent } -> { routine, session } — run NOW, approved:false
GET   /sessions?agent=<id>     { sessions: SessionMeta[] }
GET   /sessions/<id>?agent=    { session, messages }
POST  /sessions                { agent, title? } -> { session } (manual; default "Chat <n>")
POST  /sessions/<id>/message   { agent, from, body } -> { ok: true } (append-only)
GET   /artifacts?agent=<id>    { artifacts: StoredArtifact[] }
```

A routine run always executes with `approved: false` (send-gate, default deny):
a gated capability records `needs_approval` into the transcript and artifact —
scheduled runs never auto-send. The scheduler is opt-in (`ROUTINE_SCHEDULER=1`;
tick every `ROUTINE_TICK_MS`, default 30s) and interprets human schedule labels
("Every 30 minutes", "Every hour", "Weekdays 8:00", "Every day 18:00", "Every
Monday 9:00") — time-of-day labels fire on the first tick at/after the time,
once per matching day; this is deliberately not cron (see `src/scheduler.ts`).

## Status

`buildWorkflow` + the full service (`/build/stream` SSE, `/run` executor, `/providers`)
are verified live, key-free, against Ollama (`scripts/smoke.sh` → `SMOKE OK`). The narrow
BUILD task uses one structured `pi-ai` call; the full agentic tool-loop (gbrain /
browsersniff via `pi-agent-core`) lands at the discovery slice. Next: run a *subscription*
model head-to-head on spec quality (operator `/login`), then the real deterministic
executor (API-first replay).
