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

```
src/wire.ts        WorkflowSpec/Step types + the build prompt + extractJson + validateSpec
src/model.ts       model resolution: Ollama (key-free default) | subscription | BYOK
src/buildAgent.ts  buildWorkflow(message) -> WorkflowSpec via pi-ai `complete`; streamWorkflow()
src/run.ts         CLI runner (compile a description live)
src/buildAgent.test.ts  pure tests (extractJson / validateSpec)
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

## Status (S2, pi-mono engine)

`buildWorkflow` is verified live key-free against Ollama. The narrow BUILD task uses one
structured `pi-ai` call; the full agentic tool-loop (gbrain / browsersniff tools via
`pi-agent-core`) lands at the discovery slice. Next: wire `streamWorkflow` into the
service `/build/stream` and run a subscription model head-to-head on spec quality.
