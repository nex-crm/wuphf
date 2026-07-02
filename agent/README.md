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
src/store.ts       file-backed per-agent store: tools + artifacts
                   (WUPHF_AGENT_DATA_DIR, default .wuphf-agent-data/; atomic-ish writes)
src/sessions.ts    pi-backed chat sessions: transcripts persist as pi SessionManager
                   JSONL trees under <dataDir>/sessions/<agent>/ (resume/branching
                   come from pi, not custom code)
src/routineRunner.ts  run a fired routine = match-or-author a tool, run it approved:false,
                   append the transcript into the routine's pi session, save the md
                   run artifact. Definitions/cron/versioning/run history live in the
                   BROKER's scheduler registry — the broker calls POST /routines/run
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

### Persistence + routines routes

Tools + artifacts persist as one JSON file per agent id (`src/store.ts`); chat
sessions persist in pi's native session format (`src/sessions.ts`). Wire shapes
live in `src/wire.ts` (`StoredTool`, `SessionMeta`, `SessionMessage`,
`StoredArtifact`). Routine DEFINITIONS (prompt, cron, enable/disable, revision
history = versioning, per-run history) live in the BROKER's scheduler registry
— this service holds none of them.

```text
POST  /tools/build             `app` set -> persist the authored tool under that agent
                               (re-authoring a same-named tool bumps `version`)
GET   /tools?agent=<id>        { tools: StoredTool[] }
POST  /routines/run            { agent, slug, name, prompt } -> { status, digest, session_id }
                               the BROKER fires a due routine here (approved:false);
                               the outcome lands in its per-slug run ring
GET   /sessions?agent=<id>     { sessions: SessionMeta[] }
GET   /sessions/<id>?agent=    { session, messages }
POST  /sessions                { agent, title? } -> { session } (manual; default "Chat <n>")
POST  /sessions/<id>/message   { agent, from, body } -> { ok: true } (append-only)
GET   /artifacts?agent=<id>    { artifacts: StoredArtifact[] }
```

A routine run always executes with `approved: false` (send-gate, default deny):
a gated capability records `needs_approval` into the transcript and artifact —
scheduled runs never auto-send. Scheduling is the broker watchdog's job (real
cron via `internal/calendar/cron.go`; `WUPHF_AGENT_URL` points it at this
service, default :8820). pi session semantics apply to chats: a session file
flushes on its first exchange, so an empty chat does not survive a restart.

## Status

`buildWorkflow` + the full service (`/build/stream` SSE, `/run` executor, `/providers`)
are verified live, key-free, against Ollama (`scripts/smoke.sh` → `SMOKE OK`). The narrow
BUILD task uses one structured `pi-ai` call; the full agentic tool-loop (gbrain /
browsersniff via `pi-agent-core`) lands at the discovery slice. Next: run a *subscription*
model head-to-head on spec quality (operator `/login`), then the real deterministic
executor (API-first replay).
