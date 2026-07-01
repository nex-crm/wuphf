# Workflows as Tools (chat-first, AI-authored tools)

**Status:** spike, slice 1 (FE-first, mock). Branch `operator/workflows-as-tools`.

## The reframe (revised 2026-07-01 — keep the app UI)
**Keep the app and its UI.** The one change: when the operator teaches a workflow,
Nex **writes a custom Tool** (a scripted function tailored to that workflow) that
the app's chat can call. These tools are surfaced in a **new Tools tab** on the
app — additive; the **Workflow tab is untouched**. The app's chat has context of
the tools it has access to and **calls them when needed**.

So the unit is a **Tool**: AI-authored, workflow-specific, callable by the app's
agent. "Workflows" and "Tools" are the same idea; the Tools tab lists them with an
explanation of what each does.

A Tool: `{ id, name (callable, camelCase), purpose, inputs[], script, createdFrom
(the workflow the operator described), calls[] (invocation history) }`.

## ICP tutorial examples (Maya, RevOps) — the spec, test against all three
1. **Score + route a lead.** Maya: "When a new lead comes in, score its fit and
   route hot ones to the right AE." → Nex writes `scoreAndRouteLead(lead)` →
   appears in Tools. Later: "run it on the Acme lead" → the agent **calls**
   `scoreAndRouteLead({lead:"Acme"})` → returns "Fit 82 → routed to Priya (AE)".
2. **Weekly pipeline summary.** Maya: "Every Monday summarize last week's
   pipeline." → Nex writes `weeklyPipelineSummary()` → Tools. "run it" → the agent
   calls it → returns a glanceable summary.
3. **Draft a follow-up.** Maya: "Draft a follow-up email for a stalled deal." →
   Nex writes `draftFollowup(deal)` → Tools. "draft one for the Globex deal" → the
   agent calls `draftFollowup({deal:"Globex"})` → returns the draft.

## Where tools come from: a create_tool tool on the chat agent (not a UI)
Tools are authored by the **chat agent's own `create_tool` tool**, not a
build-a-tool UI. The operator teaches a workflow in the app's chat; the agent
calls `create_tool(name, title, purpose, inputs, code)` to make it callable and
register it, so a later turn can call it. Implemented on **pi-mono** (the only
operator backend — the Python/deepagents harness is removed): `agent/src/tools.ts`
(`authorTool` + `buildTool`) behind `POST /tools/build` (`agent/src/service.ts`),
with the `Tool`/`ToolInput` wire shapes in `agent/src/wire.ts` mirroring the FE.
This is the only way tools are made.

These are **agent tools**: they take input params and return agent-shaped output,
so only the app's chat calls them — a human never runs one by hand.

## Slice 1 — the Tools tab (DONE)
A new **Tools tab** on the app detail (`AppToolsTab`), added to both the real
(`OperatorAppDetail`) and mock (`InternalToolDetail`) tab models — after Workflow,
before Data. The Workflow tab and everything else are unchanged. The tab only
**shows** tools; it has NO build-a-tool UI and NO Run button (agent-only).
- **Lists the app's tools** in PLAIN LANGUAGE for a non-technical operator: a
  readable title, what it does, a friendly "Needs: …", and "the chat calls this".
  The code (signature + script) is behind a **"View code"** toggle — nothing
  technical shown by default. (`web/src/operator/tools/mockTools.ts`,
  `web/src/operator/surfaces/AppToolsTab.tsx`, shared state in `toolsContext.tsx`.)

## Slice 2+3 — chat → create_tool, wired to pi-mono (DONE)
The app's Ask-AI chat (`AppToolsChat`) POSTs the taught workflow to the pi-mono
agent's `/tools/build` (vite proxy `/agent` → :8820, `WUPHF_AGENT_PORT`); the
agent's `create_tool` authors the tool; the chat renders the `create_tool(...)`
call and the tool lands in the Tools tab. Falls back to the local FE mock when the
agent is unreachable (`web/src/operator/tools/toolAgentClient.ts`). Authoring is
deterministic S0 (keyword → shape, shared with the FE mock) so it runs key-free;
the pi-model authoring path mirrors `buildAgent.ts`'s staging.

## Slice 4 — model authoring (DONE)
`create_tool` can now WRITE the tool's code: `authorToolWithModel`
(`agent/src/tools.ts`) makes one structured pi-ai `complete` call against
`TOOL_SCHEMA_PROMPT` (mirrors `buildAgent.ts`: `extractJson`, abort + 45s timeout,
`opts.complete` test override) and validates/coerces the result. Opt-in via
`TOOL_AUTHOR_MODEL=1` on the service — the deterministic stub stays the default
and the fallback on ANY model failure, so `/tools/build` never blocks on an
unreachable model. The response carries `authored_by: "model" | "stub"`.

## Slice 5 — sandboxed execution, called from the chat (DONE)
The chat CALLS tools via `POST /tools/call` → `runTool`
(`agent/src/toolRuntime.ts`): the tool's code runs in a **Worker isolate**
(`agent/src/toolSandboxWorker.ts`) that is HARD-KILLED at the deadline
(`worker.terminate()` — a synchronous infinite loop dies on time, not just the
waiting). Capabilities stay HOST-side: every capability call is an RPC from the
worker back to the host, so tool code never holds the broker token, a model
key, or the capability implementations (`import`/`eval` are also rejected by a
code scan and dangerous globals are shadowed). The capability runtime
(`agent/src/capabilities.ts`) composes REAL seams from the host env — pi-ai
`nex.ai.*` via `TOOL_RUNTIME_MODEL=1`, broker `integrations.call` +
`nex.browser` via `WUPHF_BROKER_URL`/`WUPHF_BROKER_TOKEN` — and deterministic
simulations otherwise, with every capability call recorded as an action trace.
**Send-gate:** gated capabilities (`crm.assign`, `nex.send`) default-deny —
the run halts `needs_approval` with a human-readable gate detail; `approved:
true` (the human's answer) executes. FE: saying "run the weekly summary" in the
app's chat invokes the matching tool (name/title mention or run/call/use +
title-word overlap), renders the call + action trace + result, pauses on an
inline Approve / Not now card for gated calls, and logs completed calls so the
Tools tab shows a read-only "Last run". Still no Run button anywhere.

## Slice 6 — real capability seams (DONE)
The Worker isolate + host-side capability composition above (real
integrations/browser via the broker, real `nex.ai.*` via a model, simulated
fallbacks) shipped as slice 6; realness is a deployment property of the host
env, not of the tool's code.

## Later slices
7. Persistence + versioning of Tools; edit-a-tool in chat; restore app-edit-via-AI
   alongside tool-teaching in the Ask-AI dock.
