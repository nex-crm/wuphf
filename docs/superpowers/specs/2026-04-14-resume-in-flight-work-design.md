# Resume In-Flight Work on Startup

**Date:** 2026-04-14
**Status:** Approved

## Problem

When WUPHF shuts down (graceful or crash) with work in flight, that work is lost on restart:

1. **Orphaned tasks** — Tasks with status `in_progress`, `review`, or `pending` have owners, but the owning agent process is gone. On restart, agents get fresh sessions with no awareness of their assigned work. Tasks sit in limbo.

2. **Unanswered conversations** — A human message was routed to an agent, the agent was processing it, and WUPHF died before the reply came back. On restart, the message is never re-delivered. The human gets silence.

The broker state file (`~/.wuphf/team/broker-state.json`) correctly persists all task and message state across restarts. The gap is that no mechanism re-pushes that state to agents after they boot.

## Solution

A single startup resume sweep that runs once after agents are ready. It scans broker state for two categories of interrupted work, builds a combined resume work packet per agent, and delivers it through the existing notification infrastructure.

## Design

### Resume Detection

Two scanners run sequentially in `resumeInFlightWork()`:

**Task scanner:**
- Iterate `broker.tasks`
- A task needs resuming if:
  - Status is non-terminal (`in_progress`, `review`, `pending`, `blocked` — anything except `done`, `completed`, `canceled`, `cancelled`)
  - It has an owner (agent slug)
  - The owner exists in the current pack's agent list
- Group results by owner

**Conversation scanner:**
- Iterate `broker.messages` in reverse (most recent first)
- A message needs re-delivery if:
  - It's from `"you"`, `"human"`, or `"nex"` (human-originated). Agent-to-agent messages are intentionally excluded — those are coordination artifacts, not work requests. If an agent delegated to another agent, the task system tracks that, not the message scanner.
  - It was posted within the last broker session (use broker's saved timestamp as lower bound, falling back to last 50 messages if no timestamp is available)
  - No reply from any agent exists after it in the same thread (check messages with matching `ReplyTo` or thread chain)
  - It was tagged to specific agents, or would route to agents via existing `notificationTargetsForMessage` logic
- Group results by target agent
- Skip agents not in the current pack

### Work Packet Construction

One resume packet per agent, batching all their interrupted work:

```
[Session resumed — picking up where you left off]

Active tasks:
- #task-1 "Implement auth flow" (in_progress, stage: implement)
  Working directory: "/path/to/worktree"
- #task-2 "Review dashboard PR" (review, stage: review)

Unanswered messages:
- @human in #general (msg-abc): "Can you check the API rate limits?"

Resume instructions:
Continue your most urgent work. For tasks, pick up where you left off.
For unanswered messages, read the context and respond.
Use team_broadcast with my_slug "<slug>" and the appropriate channel/reply_to_id.
```

If an agent has only tasks or only messages, omit the empty section. If an agent has nothing to resume, skip them entirely (no notification sent).

### Startup Integration

**Tmux path (`Launch()`):**
- Append resume logic to the end of `primeVisibleAgents()`
- `primeVisibleAgents` already waits for agents to be ready (handles Claude startup interactivity), making it the natural gate
- After the existing "replay latest message" logic, call `l.resumeInFlightWork()`
- Note: `primeVisibleAgents` runs as a goroutine concurrent with `notifyAgentsLoop`. If a new human message arrives during the prime window, both the resume sweep and the notify loop could target the same agent. The existing debounce cooldowns (`agentNotifyCooldown`) mitigate this — and in tmux, `/clear` before each send means the later delivery simply replaces the earlier one.

**Headless path (`launchHeadlessCodex()`):**
- Call `l.resumeInFlightWork()` **synchronously before** starting `notifyAgentsLoop` and other background loops
- This avoids a race where the notify loop delivers a fresh message while the resume sweep is still building its packet, which in the headless queue model could produce confusing double-context
- The broker is already started and state is loaded, so the resume scan can run immediately
- If no work needs resuming, this is a fast no-op (single pass over tasks + messages)

**Deduplication:**
- Tmux: the existing `/clear` sent before each pane notification means a duplicate delivery just replaces the previous one
- Headless: synchronous resume before notify loop eliminates the race entirely
- The existing debounce cooldowns (`agentNotifyCooldown` / `agentNotifyCooldownAgent`) provide an additional safety net
- No new deduplication state is needed

### Edge Cases

| Scenario | Behavior |
|---|---|
| Stale tasks (WUPHF down for days) | Resume nudge still fires. Agent or CEO decides to close stale tasks. No auto-expiry. |
| Task `in_progress` with no owner | Skipped by task scanner. CEO sees it in normal task context and can reassign. |
| Owner not in current pack | Skipped. Only agents in the active pack receive resume packets. |
| Multiple rapid restarts | Second resume delivery replaces first via `/clear`. No harm. |
| 1:1 mode | Works normally. Filters to the single active agent's tasks and messages. |
| Empty state (no in-flight work) | `resumeInFlightWork` is a no-op. No notifications sent. |
| Explicit `--pack` flag on restart | Broker state is deleted (line 162 of `launcher.go`). Nothing to resume. Clean slate. |

## Files to Modify

| File | Change |
|---|---|
| `internal/team/launcher.go` | Add `resumeInFlightWork()`, `buildResumePacket()`, `findUnansweredMessages()`. Call from end of `primeVisibleAgents()`. |
| `internal/team/headless_codex.go` | Call `l.resumeInFlightWork()` synchronously before notification loops in `launchHeadlessCodex()`. |
| `internal/team/broker.go` | Add `InFlightTasks() []teamTask` and `RecentHumanMessages(limit int) []channelMessage` exported accessors for the resume scanner. |
| `internal/team/launcher_test.go` | Test resume detection: tasks with various statuses, unanswered message detection, pack membership filtering, empty state no-op. |

## Relationship with SessionRecovery

The existing `SessionRecovery` struct in `session_memory.go` builds focus/next-steps/highlights from tasks and requests. It serves a different purpose: it's **pulled** by the channel view to show the human "what changed while you were gone." The resume packet is **pushed** to agents to wake them up and give them work. They share some input data (tasks, messages) but are intentionally separate systems — one is human-facing UI, the other is agent-facing notification.

## Out of Scope

- Mid-tool execution state recovery (tool calls are ephemeral)
- Auto-expiring stale tasks on restart
- Persistent delivery acknowledgment tracking
- Agent heartbeat infrastructure
