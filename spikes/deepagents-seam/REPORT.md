# Spike report: Go-broker ↔ Python-deepagents seam (Option A)

> Throwaway spike. Branch `worktree-deepagents-harness-eval`. 2026-06-19.
> Question: is "keep the Go broker, run the agent inner loop in Python deepagents,
> reuse WUPHF tools over MCP" real and cheap — before we write a migration plan?

## Verdict

**Seam validated. Proceed to the migration plan.** A Python deepagents agent
talks to the real `wuphf mcp-team` MCP server, reuses every WUPHF tool unchanged,
and layers planning + subagents + virtual-FS on top. The per-call seam cost is
~2 ms. One yellow flag (server cold start) that shapes the dispatch model, not the
decision.

## Setup

- Real server: `wuphf mcp-team` (production `internal/teammcp`, stdio MCP, official
  `modelcontextprotocol/go-sdk v1.6.1`). Built from `origin/main` @ `ec467159`.
- Round-trip baseline: `echo-server` — a 30-line Go MCP server using the *same SDK
  + StdioTransport*, so it isolates seam cost from broker + LLM cost.
- Python: `deepagents 0.6.11`, `langchain-mcp-adapters`, `langgraph`, `mcp`, CPython 3.12.
- No model API key. Deterministic fake chat models drive the loop, so every number
  is **seam cost, not vendor LLM latency**.
- Repro: `uv venv .venv && uv pip install -r <pyproject> && .venv/bin/python seam_probe.py`

## Results

| Layer | What it proves | Result |
|---|---|---|
| **L1 — MCP interop** | Python deepagents stack connects to real `teammcp` | ✅ **76 tools** discovered; `tools/list` **24.7 ms**; `initialize` **6.26 s** (cold start, see flag) |
| **L1b — round-trip** | Per-call seam cost (same-SDK Go server, no broker) | ✅ **median 1.88 ms**, min 0.79, p95 12.0 (30 calls) |
| **L2 — deepagents build** | deepagents wraps teammcp tools + adds its own | ✅ **84 bound = 76 teammcp + 8 deepagents builtins** (`write_todos`, `task`, `ls/read_file/write_file/edit_file/glob/grep`) |
| **L3 — HITL** | `interrupt_on` pauses before a tool call | ✅ interrupted before `echo`; maps to the broker approval gate |

`results.json` holds the raw output.

## What this means

1. **The keystone holds.** deepagents reuses all 76 live WUPHF tools by connecting to
   `teammcp` as an MCP client — **zero tool rewrite** — and injects exactly the deep-agent
   behaviors the native Go loop lacks: `write_todos` (planning), `task` (ephemeral
   subagents), and a virtual filesystem. That is the entire thesis of Option A, demonstrated.
2. **The seam is not a latency problem.** ~2 ms median per tool call over Python↔Go stdio
   MCP. Dispatch overhead is noise next to model inference and tool work.
3. **HITL transfers.** deepagents `interrupt_on` produces a LangGraph interrupt that the Go
   broker can surface through its existing approval gate — no new approval UX needed.

## Flags (inform the plan, do not block it)

- 🟡 **`wuphf mcp-team` cold start ≈ 6.3 s.** Process spawn + teammcp init (likely a broker
  reach with a timeout). Implication for the dispatch model: the Python executor should hold
  **one long-lived `teammcp` session per task** (or a warm pool), never cold-spawn per call.
  Worth a 30-min characterization of where the 6 s goes before locking dispatch.
- ⚪ **76 vs 91 tools.** `teammcp` registers a mode-dependent subset; 91 is the all-branches
  upper bound, 76 is this config (markdown backend, no slug/channel). Expected, not a bug.

## NOT proven here (carry into the plan)

- **Real LLM loop quality** — no API key; we proved wiring, not that deepagents-on-our-models
  matches Claude Code/Codex on real tasks. This is the capability bet from the assessment.
- **Live tool *execution*** — we proved discovery + binding + transport; executing a mutating
  teammcp tool needs a running broker + token (the broker is the host, by design).
- **End-to-end streaming** through a real `provider/deepagents` dispatch RPC (events back to
  the broker UI). Standard work, but unmeasured here.
- **Deployment** — bundling a Python runtime with the Go binary / Wails desktop sidecar.
