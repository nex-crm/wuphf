# Operator Harness — Clean Start (deepagents-native)

**Status:** plan, pre-scaffold. **Decision date:** 2026-06-27 (founder picked the
clean deepagents-native rewrite). **Supersedes:** most of
`deepagents-migration-plan.md` (the LangGraph multi-agent stack) — salvage the seam only.
**Companion to:** `operator-mlp-plan.md`, `operator-mlp-build-queue.md`,
`operator-mlp-prototype-brief.md`.

---

## 1. What we are building

The operator-MLP product: **a non-technical operator builds a deterministic internal
tool (UI + Workflow) by talking to an AI.** The spine is unchanged:

> **Agentic freedom to FIGURE OUT the workflow; determinism to EXECUTE it.**

The backend is **one fast deepagents-native agent harness** that turns a conversation
(or a captured screen session) into a **compiled, deterministic workflow spec**, plus a
**deterministic executor** that runs that spec. No multi-agent office, no Go broker, no
channels/lifecycle/coordination.

## 2. Why a clean start

The Go broker existed to coordinate a multi-agent office. The product no longer has an
office — it has **one operator and one build agent**. Keeping the broker (office,
channels, 13-state lifecycle, sub-task coordination kernel, re-hydrate) would be carrying
a coordination engine for a problem we no longer have. The 2026-06-20 LangGraph migration
(#1117–#1133) was building coordination *on* that broker; it is moot. We keep the parts
that were always about **one agent reaching tools and streaming to a UI**.

## 3. Architecture (three layers)

```
┌─ Operator FE (React, salvaged from web/src/operator) ──────────────┐
│  Chats · Internal Tools (UI·Workflow·Data) · Knowledge · Integrations · Settings │
│  talks over a thin HTTP + SSE API (no broker)                       │
└───────────────────────────────┬────────────────────────────────────┘
                                 │  HTTP/SSE  (the salvaged FastAPI + SSE seam)
┌───────────────────────────────▼────────────────────────────────────┐
│  Harness API (Python / FastAPI)                                     │
│   POST /build/stream   chat → agent assembles a workflow (SSE)      │
│   POST /run            execute a compiled workflow on test/real data │
│   GET/POST /knowledge  gbrain (search/query/put_page/find_experts)  │
│   /integrations /settings /providers   (BYOK + provider detection)  │
└───────────────────────────────┬────────────────────────────────────┘
                  ┌──────────────┴───────────────┐
        ┌─────────▼─────────┐          ┌──────────▼───────────┐
        │ BUILD (agentic)   │          │ EXECUTE (deterministic) │
        │ deepagents agent: │  compile │ compiled WorkflowSpec:  │
        │ plan + tools →    │ ───────► │ API-first → UI-replay → │
        │ a WorkflowSpec    │          │ bounded CUA-heal        │
        └─────────┬─────────┘          └─────────────────────────┘
                  │ tools over MCP (the salvaged seam)
        gbrain · browsersniff · browser-capture (chromedp) · Composio
```

- **BUILD is agentic** (deepagents): a single deep agent that plans, drives discovery
  (capture/sniff/gbrain), asks the operator one sharp clarifying question, and emits a
  **WorkflowSpec**. It does NOT execute the live workflow.
- **EXECUTE is deterministic**: the compiled `WorkflowSpec` runs through the workflow
  engine (API-first replay → UI replay → bounded CUA-heal). No tool-call chain at run time.

### Why deepagents (LangChain) for BUILD
deepagents gives a prebuilt deep-agent loop on LangGraph: a planning tool, sub-agents, a
virtual filesystem, and steerable tool use — the right shape for "figure out the
workflow." We use it ONLY for build/discovery; the deterministic executor is ours.

## 3a. Engine decision (2026-06-27, after research)

The BUILD engine is **pi-mono** (`@mariozechner/pi-ai` + `pi-agent-core`), a model-agnostic
embeddable TypeScript agent SDK (the stack OpenClaw is built on). Why it won over the
alternatives we evaluated:

| Option | Key-free? | Multi-provider | In our stack | Verdict |
|---|---|---|---|---|
| **pi-mono (pi-ai)** | ✅ subscription OAuth (`/login`) + BYOK + **Ollama/open-weight** | ✅ (Anthropic, OpenAI/Codex, Google, Ollama, 2000+) | ✅ TS, embeds in Electron/Node | **chosen** |
| Claude Code / Codex headless (`claude -p`) | ✅ (reuses CLI login) | ~ (those two) | shell-out + stdout parse | good fallback; built in `harness/` |
| deepagents (LangChain) | ❌ BYOK only | ✅ | Python | demoted to BYOK option |
| OpenClaw (the product) | ✅ | ✅ | heavy; security-flagged (arXiv 2603.11619) | too much; we take pi-mono underneath |
| Hermes (Nous) | — it's an open *model*, not a harness | — | via Ollama under pi-ai | a model choice, not the engine |

pi-mono gives the key-free benefit of the CLIs **without** shelling out, plus an
open-weight escape hatch (Ollama → Hermes) and BYOK, behind one abstraction in our TS/
Electron stack. **Verified live, key-free** against local Ollama (`agent/` package). Caveat:
pi-ai keeps its own OAuth store (one-time operator `/login`; it does not reuse Claude Code's
token), and Anthropic now meters third-party subscription use as per-token extra usage
(badlogic/pi-mono#3372) — so ChatGPT/Codex/Copilot subscriptions or BYOK are the cost-neutral
paths. The engine sits behind the `WorkflowSpec` contract, so it stays swappable.

`agent/` (TS) is the build engine, and the operator backend is **fully pi-mono**: the
Python `harness/` fallback (CLI / deepagents / stub) has been REMOVED (founder decision,
2026-07-01 — "we stay fully pi mono"). Sections below that describe the Python/FastAPI
harness are historical context for the clean-start decision, not a living backend.

## 4. Salvage / delete

**Salvage (carry into the clean start):**
- The **MCP seam** — Python reaches tools over MCP (proven by the `deepagents-seam` spike;
  gbrain speaks `search`/`query`/`put_page`/`find_experts`).
- The **FastAPI service pattern + SSE streaming** (`/run/stream` → `start`/`turn`/`result`
  events; Go/TS consumers) — becomes the FE↔harness API.
- **Claude Agent SDK harness wiring** incl. the `allowed_tools` fix (wired MCP tools must
  be allowed or the call is silently denied) and **env-NAME→value MCP config** (secrets
  cross as names, resolved harness-side).
- **Provider detection + config + BYOK** (multi-provider inference).
- The **operator FE** (`web/src/operator/**` + `operator-shell.css`) and its design system.
- **browsersniff** (HAR→`spec.APISpec`), the **workflow engine** (`internal/workflow`),
  **detection** (repointed to operator activity), **Electron shell** + build/install.
- The **hands-free-completion principle**: a task finishes without a human ack; the
  machine **verification check** is the proof-of-done. (The Go implementation is dropped
  with the broker; the principle carries into the executor's done-gate.)

**Delete / leave behind:**
- Go **broker**, office, channels, 13-state lifecycle, coordination/sub-task kernel,
  multi-agent dispatch, notifier loops, re-hydrate.
- The #1117–#1133 LangGraph coordination stack (goal/coordinate/decompose/re-hydrate
  kernel) — moot without the broker. (Stays on its draft branches as history.)

## 5. Repo strategy (THE open decision — confirm before scaffolding)

The 2026-06-20 plan stripped wuphf in place (build-queue Slice 3). The 2026-06-27 decision
added "new repo authorized if cleaner." Two real options:

**Option A — in-repo clean rebuild (RECOMMENDED).** Add the deepagents harness as a new
top-level package in wuphf, keep the operator FE where it is, and **aggressively delete**
the broker/office (build-queue Slice 3). wuphf *becomes* the operator product by
subtraction.
- Pro: every salvageable piece (FE, provider config, Electron shell, workflow engine,
  browsersniff, CI, release, npm, secretlint, screenshot harness) already lives here — no
  migration, no re-tooling, history preserved. Matches the existing build queue.
- Con: the deleted broker tempts "keep it just in case"; the repo carries Go + Python.

**Option B — brand-new repo.** Fresh repo with FE + deepagents harness + the seam, nothing
else.
- Pro: a literal clean slate; smallest surface; no broker temptation.
- Con: migrate the FE + Electron + provider config + workflow engine + browsersniff +
  re-stand-up CI/release/build — weeks of plumbing before the first product slice; history
  lost.

**Recommendation: Option A.** The clean-start value is in *deleting the broker and writing
a new harness*, not in a new git remote. Subtraction-in-place gets us to a working
deepagents product fastest and keeps the large, real salvage in place. Reserve Option B
only if the founder wants a hard public break.

## 6. Slice plan (FE-first; BE rewritten under this lens)

Slice 1 (operator FE shell, mock) is **done, awaiting founder review**. Then:

- **S0 — Clean-start scaffold.** New `harness/` (Python: FastAPI app + deepagents agent
  stub + provider config, lifting the seam: SSE, MCP env-name config, SDK wiring). FE
  `/#/operator` points its build/run calls at the harness behind a feature flag; mock
  fixtures stay the default until the endpoint is real. No broker dependency.
- **S1 — Strip the office cruft** (build-queue Slice 3). Delete broker/office/lifecycle;
  `web` + harness build green; LOC down hard.
- **S2 — Chat-to-build (the spine).** Swap the FE's keyword `planWorkflow` compiler for the
  real deepagents build agent over `/build/stream`; it assembles a `WorkflowSpec` live and
  asks one clarifying question. (CQ1 approval card on any build-time external mutation.)
- **S3 — Deterministic executor.** Compile `WorkflowSpec` → engine run on fixtures →
  digest; UI/Workflow/Data tabs bound to real run records. (CQ1 exec-time mutation gate.)
- **S4 — gbrain over MCP + Knowledge** (tests A1 parallel-run, A2 unreachable-degrade).
- **S5 — Browser capture + browsersniff** (the discovery half of build).
- **S6 — The magical call** (demo gate; tests A3 HAR secret-strip, A4 auth-classify).
- **S7 — Proactive improvement** (suggested-change card → re-freeze, version+1).

Each slice ships its mapped critical test: **A1/A2** (S4), **CQ1** (S2/S3), **A3/A4** (S6).

## 7. Wire contract (FE ↔ harness)

Reuse the SSE shape already built and tested: `POST /build/stream` emits `start` →
`step` (one event per `WorkflowStep` as the agent assembles the spec) → `spec` (the
compiled `WorkflowSpec` + the one clarifying question), with an `error` event if the
build fails mid-stream. `extra="forbid"` + a
`schema_version` handshake (both carried from the dead stack's hardening) so a FE/harness
drift fails loud. Secrets cross as env-var **names** only.

## 8. Open questions

1. **Repo strategy** (§5) — confirm Option A vs B before scaffolding. *(blocking)*
2. deepagents version pin + whether we wrap it or fork the loop (decide at S2).
3. gbrain packaging (bundled PGLite+ollama subprocess) — already decided in the operator
   plan; re-confirm at S4.
