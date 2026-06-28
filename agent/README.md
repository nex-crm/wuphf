# wuphf-agent (pi-mono build agent)

The BUILD half of the operator harness, on the **pi-mono** stack (`@mariozechner/pi-ai`):
plain-language description → a deterministic `WorkflowSpec`. **Key-free, multi-provider.**

This is the chosen engine (see `docs/specs/operator-harness-clean-start.md` §Engine
decision). It replaces driving `claude -p`/`codex` via stdout: pi-ai is an embeddable
TypeScript SDK over one abstraction for **subscription OAuth** (Claude Pro/Max, ChatGPT/
Codex, Copilot — `/login`, no API key), **BYOK** (env keys), and **local/open-weight**
(Ollama → e.g. Hermes). Same `WorkflowSpec` contract as the FE (`web/src/operator/mock/
data.ts`) and the Python harness, so it drops in behind the same `/build/stream` shape.

## Layout

```text
src/wire.ts        WorkflowSpec/Step + Build/Run request + RunResult; the build prompt; extractJson; validateSpec
src/model.ts       model resolution: Ollama (key-free default) | subscription | BYOK
src/buildAgent.ts  buildWorkflow(message) -> WorkflowSpec via pi-ai `complete`; streamWorkflow()
src/executor.ts    deterministic run; a step with an `api` is REPLAYED as a real HTTP call (auth resolved from a named ref); a gated step halts for the approval card (CQ1); a failed call halts with error
src/sniff.ts       browsersniff: HAR -> ApiCall/WorkflowStep; strips secrets, auth->named ref, classifies stable-key vs rotating-session (A3/A4)
src/providers.ts   inference-path detection for the FE Settings surface (subscription / BYOK / local)
src/service.ts     Bun.serve HTTP/SSE: /health, /providers, POST /build/stream (SSE), POST /run
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
replaces the Python `harness/` as the operator backend; the Python build agents remain a
fallback until removed.

## Status

`buildWorkflow` + the full service (`/build/stream` SSE, `/run` executor, `/providers`)
are verified live, key-free, against Ollama (`scripts/smoke.sh` → `SMOKE OK`). The narrow
BUILD task uses one structured `pi-ai` call; the full agentic tool-loop (gbrain /
browsersniff via `pi-agent-core`) lands at the discovery slice. Next: run a *subscription*
model head-to-head on spec quality (operator `/login`), then the real deterministic
executor (API-first replay).
