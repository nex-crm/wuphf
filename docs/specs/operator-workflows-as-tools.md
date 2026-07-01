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
register it, so a later turn can call it. Implemented in the harness (the PyBase
chat agent): `harness/src/harness/tools.py` (`ToolStore` + `make_create_tool`),
wired into the deep agent's tool list next to `submit_workflow`
(`build_agent.py`), with the `Tool`/`ToolInput` wire shape in `wire.py` mirroring
the FE. This is the only way tools are made.

## Slice 1 — the clickable shape (mock) — DONE
A new **Tools tab** on the app detail (`AppToolsTab`), added to both the real
(`OperatorAppDetail`) and mock (`InternalToolDetail`) tab models — after Workflow,
before Data. The Workflow tab and everything else are unchanged. The tab only
**shows** tools; it has NO build-a-tool UI (that lives in the chat agent).
- **Lists the app's tools** in PLAIN LANGUAGE for a non-technical operator: a
  readable title, what it does, a friendly "Needs: …", a Run button, and the last
  run result. The code (signature + script) is tucked behind a **"View code"**
  toggle — nothing technical shown by default.
- **Run**: each tool has a Run button that logs a mock invocation (stands in for
  the app's chat calling the tool).
- All mock on the FE: `seedToolsForApp` lists the app's built tools; `callTool`
  returns a canned result. No backend. (`web/src/operator/tools/mockTools.ts`,
  `web/src/operator/surfaces/AppToolsTab.tsx`.)

## Later slices (after the shape is validated)
2. **Wire the chat → create_tool end to end.** The app's Ask-AI chat calls the
   harness `create_tool`; the new tool appears in the Tools tab; the chat renders
   the tool-call. (The tool + store exist; wiring the FE chat to the harness is
   next.)
3. **Real tool authoring** — the agent writes actual callable code (fills `code`).
4. **Real execution** — the chat calls the Tool (sandboxed), against real
   integrations / the browser-step engine, with the existing send-gate.
5. Persistence + versioning of Tools; edit-a-tool in chat.
