# Routines: workflows are scheduled prompts in the chat

**Status:** slice 1 (FE mock) done; slice 2 (backend persistence + scheduler)
done — see `agent/README.md` §Persistence + routines routes. Branch
`operator/agent-routines`.
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
2. **Backend (DONE — reworked to NO custom engine, founder direction
   2026-07-01: "don't do custom; use what pi agent already provides").**
   A routine IS a broker scheduler job (`internal/team/scheduler*.go` — the
   previous product avatar's proven engine). The broker owns:
   - **cron** (`internal/calendar/cron.go`: 5-field + daily/hourly/Nh) and
     enable/disable;
   - **versioning**: every prompt edit is a scheduler REVISION (monotonic
     version, author, change note, restorable) — "Publish new version" is
     `PATCH /scheduler/{slug} {payload, change_note}`;
   - **run history**: the per-slug 20-ring (status, timestamps, trigger
     attribution, events, errors) + the 50-ring activity log.
   On each fire the watchdog detects a custom-app owner
   (`scheduler_operator_routines.go`) and POSTs `WUPHF_AGENT_URL`
   `/routines/run {agent, slug, name, prompt}`; the agent service runs the
   prompt through the tools flow (match-or-author, then run with
   `approved: false` — a gated run records needs_approval, never auto-sends)
   and persists the transcript into the routine's **pi session**
   (`agent/src/sessions.ts`: pi `SessionManager` JSONL trees — resume and
   branching come from pi, not custom code). "Run now" =
   `POST /scheduler/{slug}/run` (the broker backdates next_run; the watchdog
   fires within a tick). The agent service stores NO routine definitions and
   runs NO scheduler; its custom session store was deleted with the rework.
3. **Artifacts (md DONE).** Every routine run saves its outcome as an `md`
   artifact (`<kebab-name>-run-<n>.md`) served by `GET /artifacts`. pdf/html
   outputs are still to come.
