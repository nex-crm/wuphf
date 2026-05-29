# Issue Execution Loop — Spec

**Status:** Draft, in active build (2026-05-26)
**Owner:** Najmuzzaman
**Branch:** `mvp/v3-architecture-shape`

## North star

Any work the human asks for becomes an Issue. The Issue carries the work
from "human request" through "owner agent ships it" with a human gate at
the start (approve to begin) and a human surface at the end (review what
landed). Important transitions surface in chat as Issue cards. Anything
that needs the human's input parks in the Inbox.

## Decisions locked

| ID | Decision | Choice | Why |
|----|----------|--------|-----|
| A1 | Owner assignment timing | CEO sets `owner` on `team_task action=create` in one call | One tool call, simpler model, already supported by broker. |
| B1 | Spinning up a new agent | CEO calls `team_member action=create` FIRST, then `team_task` with the new slug as owner | Explicit creation; no hidden side effects. |
| C1 | "Needs human input" surface | Owner agent calls `human_interview` (existing path) → lands in Inbox | Reuse working path, no new lifecycle state needed. |

## End-to-end flow

```
Human posts work
  → CEO creates Issue (drafting) + assigns owner
       (CEO prefers existing fit; only creates new agent when no fit exists)
  → System emits Issue card in chat (📋 amber, "Review & Approve →")
  → Human opens Issue
       ↓ can comment, edit spec, tag agents
       ↓ click Approve & Start  → Issue → running, owner WAKES
       ↓ click Close issue      → Issue → cancelled, owner NOT spun up
  → Owner agent runs the work
       ↓ blocked / needs human  → human_interview (Inbox card) + Issue comment
       ↓ status update          → Issue comment (CEO sees, can intervene)
       ↓ done                    → Issue → in review or done + chat card
  → CEO is woken on owner Issue comments
       ↓ can respond directly OR escalate to human via Inbox
  → Every important lifecycle change → chat card linked to Issue
```

## Current bugs found while mapping

| # | Bug | Status |
|---|-----|--------|
| C1 | Drafting + Approve transitions Issue directly to **approved** (terminal/done) instead of **running**. Owner never starts. | ❌ TODO Slice 1 |
| C2 | Same approve path writes a "decision" entry to wiki with zero context. | ❌ TODO Slice 1 (drop the write) |
| C3 | CEO assigns itself as owner by default — never picks an existing specialist agent. | ❌ TODO Slice 2 |
| C4 | Drafting → Running transition doesn't post a chat message tagging owner; owner agent has nothing to wake on. | ❌ TODO Slice 2 |
| C5 | No chat card emitted on important lifecycle transitions (only on `issue_created`). | ❌ TODO Slice 2 |
| C6 | Owner agent has no prompt contract for {blocked, needs_input, done}; will improvise. | ❌ TODO Slice 3 |
| C7 | `human_interview` calls from owner don't link to the parent Issue, so the Inbox card has no breadcrumb. | ❌ TODO Slice 3 |
| C8 | Lifecycle states are 10+ (drafting/intake/ready/running/changes_requested/blocked_on_pr_merge/review/decision/approved/rejected/unknown) — not Linear-shaped. | ❌ TODO Slice 4 |
| C9 | Approving an Issue ≠ writing a Decision. Decisions are a distinct concept with proper shape (problem, options, choice, rationale, decided_by, decided_at). | ❌ TODO Slice 5 (after Slice 1 strips the bad write) |
| C10 | Issue comment textarea has no @-mention autocomplete; humans can't easily tag specific agents. | ❌ TODO Slice 6 |

## Shipped (verified live)

- RULE ZERO + WAIT FOR APPROVAL in agent prompts
- ACTIVE ISSUES catalog visible to all agents
- AVAILABLE SKILLS catalog (no slug hallucination)
- Broker auto-resolves Issue on `team_action_execute` boundary (drafting Issues block)
- Issue card (`issue_created` system message) renders in chat with amber "Review & Approve →" CTA when drafting
- Decision Packet auto-seeded on Issue create (no more "decision packet not yet available" 404)
- Issues default to `task_type="issue"` (legacy follow_up/research/feature/launch/bugfix get overridden)
- Tasks created via team_task land in drafting (not in_progress)
- POST /tasks/{id}/comment emits channel message tagging [ceo, ...@mentions] — wakes agents
- Close Issue button on IssueDocument (two-step with reason)

## Slice plan (execute in this order)

### Slice 1 — Fix the approve gate (30–60 min)

Goal: clicking "Approve & Start" on a drafting Issue transitions to
**running** (not approved), without writing a wiki decision.

Changes:
- `broker_decision_packet.go` `lifecycleStateForDecisionAction` →
  needs the CURRENT lifecycle state to disambiguate "approve the draft
  to start work" (drafting→running) vs "approve completed work"
  (review→approved). Pass `currentState` in; route accordingly.
- Same file `recordTaskDecisionInternal` → only call
  `writeWikiPromotionLocked` + `broadcastDecisionLocked` when the
  transition target is `LifecycleStateApproved` (terminal). The
  drafting→running transition is not a "decision" — it's a start.
- Suppress the misleading wiki write entirely for drafting→running.
- Drop the "decision" chat broadcast for drafting→running (we have
  the new issue-card system message for that).

Acceptance:
- Approve drafting Issue → lifecycle = running, owner = whatever CEO set
- Wiki gets no new entry
- Channel gets the lifecycle card emitted in Slice 2 (not a "decision"
  broadcast)

### Slice 2 — Owner assignment + wake + lifecycle chat cards (2–3 hr)

Goal: CEO picks owner deliberately, owner wakes on approve, important
transitions emit chat cards.

Changes:
- Prompt: `prompt_builder.go`
  - New `renderAvailableAgentsBlock` section: list `@slug — role/expertise`
    for every team member. CEO reads this before assigning owner.
  - Tighten ISSUE_JUDGMENT: "When creating an Issue, ALWAYS assign an
    owner. Prefer an existing agent whose expertise matches. Only call
    `team_member action=create` to spin up a new agent if NO existing
    agent fits."
  - Update RULE ZERO to mention owner-assignment requirement.
- Broker: `broker_decision_packet.go` + `broker_tasks_notifications.go`
  - On any drafting→running transition, post a channel message tagging
    the owner: "Issue task-X approved — @owner starting work."
  - Reuse `postIssueLifecycleCardLocked` (NEW) for the structured chat
    card. Same `system` author, `kind="issue_lifecycle"`, payload with
    {task_id, from_state, to_state, title, owner, channel}.
- FE: `IssueLifecycleCard.tsx` (NEW)
  - Same pattern as `IssueCreatedCard`.
  - Renders different copy per transition: "Approved & started",
    "Done", "Closed", "Needs your input", etc.
  - All clickable → open Issue detail.
  - Wire into `MessageBubble.tsx` dispatch.

Acceptance:
- Fresh test: ask CEO to do bookkeeping work → CEO creates Issue with
  `owner: bookkeeper` (existing agent)
- Approve & Start → chat shows green "Approved & started — @bookkeeper
  on it" card → bookkeeper agent wakes and begins work
- Close → chat shows neutral "Closed" card

### Slice 3 — Owner reports {blocked, needs_input, done} (2–3 hr)

Goal: owner agent has a clear contract for reporting back, and the
right info surfaces on the right surface.

Changes:
- Prompt: every agent (not just CEO) gets a `== OWNERSHIP CONTRACT ==`
  block when they own an Issue. Spells out:
  - Use `team_task action=comment` for status updates (CEO + reviewers
    see)
  - Use `human_interview` when blocked on human input — pass `issue_id`
    so it links back to the Issue in Inbox
  - Use `team_task action=submit_for_review` when work is ready for
    human/CEO review
  - Use `team_task action=complete` when done with no review needed
- Broker: `broker_requests_interviews.go`
  - The POST /requests handler already accepts `issue_id` (shipped
    earlier). Verify it persists on every `human_interview` call from
    the MCP layer too — `internal/teammcp/server_human_interview.go`
    needs to pass `IssueID` when the agent owns an Issue.
- Broker: `broker_inbox_handler.go`
  - When the human comments on an Issue, current behavior wakes CEO +
    @mentions. Keep that, AND wake the owner too (always tag owner so
    they see clarifications even when no one @s them).
- FE: Inbox cards for `human_interview` requests should show "Issue:
  task-X" breadcrumb when `issue_id` is set.

Acceptance:
- Owner agent runs, hits a blocker, calls human_interview → Inbox card
  appears with "Issue: task-X — [open]" link
- Owner agent finishes → calls team_task action=complete → Issue moves
  to in_review (or done depending on review_state) → chat card emitted

### Slice 4 — Linear-style lifecycle (~4 hr)

Goal: collapse the 10+ states to Linear's standard set.

Mapping:
| Old state | New state |
|-----------|-----------|
| drafting | **Backlog** (initial — needs human approval) |
| intake | **Todo** (approved, awaiting pickup) |
| ready | **Todo** |
| running | **In Progress** |
| changes_requested | **In Progress** (with reviewer feedback flag) |
| blocked_on_pr_merge | **In Progress** (with blocked flag) |
| review | **In Review** |
| decision | **In Review** |
| approved | **Done** |
| rejected | **Cancelled** |
| unknown | **Backlog** (fallback) |

Changes:
- `broker_lifecycle_transition.go` LifecycleState constants → 6 new
  states: Backlog, Todo, InProgress, InReview, Done, Cancelled.
- Migration shim: read old states, emit new on every load/save.
- `derivedFieldsFor` table → 6 rows.
- `lifecycleToColumn` (FE) → 6 columns.
- `IssuesList.tsx` COLUMN_ORDER + COLUMN_LABEL + COLUMN_HINT → new
  names.
- All call sites that reference old state names get re-mapped.

Risk: lots of touch points. Keep the old constants as aliases for one
release so existing tests pass.

### Slice 5 — Real Decisions (~half-day)

Goal: introduce a first-class Decision concept, properly shaped, with
context written to wiki.

This is its own sub-spec — sketch it in `docs/specs/decisions.md` first.
Out of scope for the execution loop.

### Slice 6 — Issue comment @-mention autocomplete (~1 hr)

Goal: typing `@` in the Issue comment textarea opens the same
autocomplete the chat Composer uses.

Changes:
- Replace plain `<textarea>` in `CommentsTimeline` with a wrapper that
  uses the existing `Autocomplete` component + `applyAutocomplete` +
  `currentTrigger` helpers from `messages/Autocomplete.tsx`.
- Keyboard nav parity with `Composer.tsx`.
- When picked, slug gets inserted as `@slug`. Comment broadcast (already
  shipped) parses these and wakes the right agents.

## Out of scope (for now)

- Issue editing the spec inline (today: comment-only). Spec edits are
  Slice 7+.
- Multi-owner Issues (today: single owner). Sub-tasks (parent_issue_id)
  is the right answer here — separate slice already on the roadmap.
- Real-time owner agent status pill on the Issue card. Today: poll on
  open. Real-time via SSE is post-MVP.
- Reviewer roles (today: every member is a reviewer). Per-Issue reviewer
  assignment is Phase 2.

## Open questions

- Should "Close issue" run a reject (terminal) or a cancel (soft)?
  Today: reject. Linear's "Cancelled" maps cleanly to this.
- When the owner is the same as the creator (CEO assigned itself),
  should the chat card still show "@ceo starting work" or suppress it
  as redundant? Decision: show it — even self-assignment is a real
  state change worth surfacing.
- When auto-resolve creates an Issue on team_action_execute (the
  fallback when agent forgets), who's the owner? Today: defaults to the
  calling agent. Keep that — they triggered the work.
