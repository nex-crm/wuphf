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

## Later slices
4. **Real tool authoring** — the agent writes actual callable code (fills `code`)
   via pi-ai, same staging as the build agent's model path.
5. **Real execution** — the chat calls the Tool (sandboxed), against real
   integrations / the browser-step engine, with the existing send-gate.
6. Persistence + versioning of Tools; edit-a-tool in chat; restore app-edit-via-AI
   alongside tool-teaching in the Ask-AI dock.
