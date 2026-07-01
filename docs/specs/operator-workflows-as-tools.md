# Workflows as Tools (chat-first, AI-authored tools)

**Status:** spike, slice 1 (FE-first, mock). Branch `operator/workflows-as-tools`.

## The reframe
Drop the built **app UI**. The operator surface is a **chat with Nex**. When the
operator teaches Nex a workflow, Nex does not assemble an app — it **writes a
custom Tool** (a scripted function tailored to that workflow) and stores it in the
**Tools** library ("Workflows" and "Tools" are the same thing). In chat, the
agent **calls those Tools** to run the operator's workflow on demand.

So the unit of work is a **Tool**: AI-authored, workflow-specific, callable by the
agent. The chat is the surface; the Tools library is what Nex has built for you.

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

## Slice 1 — the clickable shape (mock)
A chat-first **Assistant** surface + a **Tools** rail.
- Chat: describe a workflow → a short "Nex is writing a tool…" activity → a
  message with a **Tool card** (name, purpose, inputs, a peek at the script); the
  Tool lands in the Tools rail.
- Run: "run it / call <tool>" → the chat renders a **tool-call** block (the agent
  invoking the Tool with args) → a mock result.
- Tools rail: the library of authored tools; selecting one shows its script,
  inputs, and call history.
- All mock: `authorToolFromDescription(desc)` derives a plausible Tool; `callTool`
  returns a canned result. No backend.

## Later slices (after the shape is validated)
2. Real tool authoring — Nex writes actual callable code for the workflow.
3. Real execution — the agent calls the Tool (sandboxed), against real
   integrations / the browser-step engine, with the existing send-gate.
4. Persistence + versioning of Tools; edit-a-tool in chat.
