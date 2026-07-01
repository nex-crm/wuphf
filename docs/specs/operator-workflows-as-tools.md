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

## Slice 1 — the clickable shape (mock) — DONE
A new **Tools tab** on the app detail (`AppToolsTab`), added to both the real
(`OperatorAppDetail`) and mock (`InternalToolDetail`) tab models — after Workflow,
before Data. The Workflow tab and everything else are unchanged.
- **Lists the app's tools** (seeded, as if built from taught workflows): each with
  its signature, an explanation of what it does, "Taught from" (the workflow), a
  collapsible script, a call count, and its last call result.
- **Call**: each tool has a Call button that logs a mock invocation (stands in for
  the app's chat calling the tool).
- **Teach a tool**: a composer at the bottom — describe a workflow → Nex writes a
  tool → it appears in the list.
- All mock: `authorToolFromDescription(desc)` derives a plausible Tool (keyword →
  shape for the three ICP examples, else a synthesized stub); `callTool` returns a
  canned result. No backend. (`web/src/operator/tools/mockTools.ts`,
  `web/src/operator/surfaces/AppToolsTab.tsx`.)

## Later slices (after the shape is validated)
2. **Author in the app's own chat.** Move tool-building from the tab composer into
   the app's Ask-AI chat (`AppBuilderChat`): teaching a workflow there writes a
   tool into the app's Tools; the chat gains tool-context and renders tool-calls.
3. **Real tool authoring** — Nex writes actual callable code for the workflow.
4. **Real execution** — the app's chat calls the Tool (sandboxed), against real
   integrations / the browser-step engine, with the existing send-gate.
5. Persistence + versioning of Tools; edit-a-tool in chat.
