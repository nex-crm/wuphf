# Routines: workflows are scheduled prompts in the chat

**Status:** slice 1 (FE-first, mock). Branch `operator/agent-routines`.
**Founder direction (2026-07-01):** "Let workflows be what Claude Routines is —
it just runs a prompt in the chat on a schedule. Which means we should allow
multiple chat sessions in our agent. Disable and Publish new version flows will
only belong to each workflow, not to the whole agent."

## The model
- A **routine** = `{ id, name, prompt, schedule, enabled, version, lastRun }`.
  It is nothing more than a prompt the agent runs in its own chat on a schedule.
  The chat already knows the agent's tools (create_tool / tool calls), so a
  routine's run IS a chat turn: the prompt goes in, the agent calls its tools,
  the outcome lands as messages (and artifacts).
- **Multiple chat sessions per agent.** Each routine run opens (or continues)
  its own session; the operator can browse every session and start new manual
  ones. The Ask Agent dock gains a session list.
- **Per-routine lifecycle.** `Disable` (pause the schedule) and `Publish new
  version` (freeze the current prompt as vN+1) belong to EACH routine — the
  agent itself no longer has Disable/Publish in its header.

## Slices
1. **FE mock (this slice).** The Workflow tab becomes **Routines**: a list of
   routines (name, human schedule, enabled toggle, vN, last run, "open its
   chat"), a per-routine Publish-new-version action, and a "New routine"
   composer (prompt + schedule). The Ask Agent dock becomes session-aware:
   a sessions rail (routine sessions + manual sessions + New chat); each
   session holds its own transcript. Agent-header Disable / Publish buttons are
   removed. Mock state lives in a per-agent `routinesContext` +
   `sessionsContext` seam.
2. Backend: routines persisted per agent; the scheduler runs a routine by
   posting its prompt into a fresh chat session against the pi-mono agent
   (/tools/build + /tools/call flow); session transcripts persisted.
3. Runs produce artifacts (a routine's md/pdf/html output lands in Artifacts).
