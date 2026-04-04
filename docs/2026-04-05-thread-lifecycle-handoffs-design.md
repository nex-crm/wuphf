# Thread Lifecycle & Structured Handoffs

## Problem

WUPHF threads never close. Once an agent posts in a thread, they stay subscribed indefinitely. There is no "done" signal, no summary surfaced to the human, and no structured way to transfer work context between agents.

This creates three failures:
1. **No closure** — the human never gets a clean "here's what was done" output
2. **Stale subscriptions** — agents keep getting woken up for concluded conversations
3. **Context-free transitions** — when work moves between agents, the receiving agent gets a task owner change but no briefing

## Key Definitions

**Thread identity**: A thread is identified by its **root message ID** — the first message in a reply chain (where `ReplyTo == ""`). All messages where `msg.ID == rootID || msg.ReplyTo == rootID` belong to that thread. There is no separate thread table; thread identity is derived from the message graph.

**Thread-task relationship**: Threads and tasks are independent objects. Tasks have an optional `ThreadID` field linking them to a thread, but:
- Concluding a thread does NOT auto-complete associated tasks
- Completing a task does NOT auto-conclude its thread
- A thread may have zero, one, or many tasks
- A task may have no thread

These are separate lifecycle concerns. Threads track conversation state; tasks track work state.

## Design

### Thread Lifecycle

Threads gain a `state` field: `open` (default) → `concluded`. Concluded threads suppress auto-notifications but allow explicit tagged messages through.

**Concluding a thread** requires a structured summary:
- **Discussed**: What topics were covered
- **Decided**: What decisions were made
- **Done**: What concrete work was completed (files, deployments, outputs)
- **Open items**: Anything remaining or handed off

**Authorization**: The broker validates that `my_slug` appears in the thread's message history before allowing conclusion. Only agents who participated can conclude.

**Reopening**: Only the CEO/lead agent or the human can reopen a concluded thread (broker validates the caller's slug is the lead). Reopening flips it back to `open` and re-subscribes participants.

**Idempotency**: One thread, one conclusion. If a thread is already concluded, subsequent `team_conclude` calls return the existing conclusion. To replace it, reopen then re-conclude.

**Conclusion summaries are dual-stored**:
1. As a structured `threadConclusion` object (for querying, TUI rendering)
2. As a `[CONCLUSION]` channel message in the thread (for free TUI rendering via existing message list, like existing `[STATUS]` messages)

### Notification Rules for Concluded Threads

- **Thread-participant auto-notifications**: Suppressed (the main win)
- **Explicitly tagged messages** (`@agent` in a concluded thread): Allowed through — the sender intentionally wants attention
- **CEO/lead messages** in a concluded thread: Allowed through (CEO always broadcasts)
- **Task notifications** for tasks linked to a concluded thread: Still delivered (task lifecycle is independent)

### Structured Handoffs

When an agent finishes their piece and another agent needs to pick up, they use `team_handoff` instead of reassigning the task. A handoff packages:

- **What I did** — concrete outputs, decisions made, files changed
- **What you need to do** — the specific remaining work for the receiving agent
- **Context you'll need** — relevant details, gotchas, dependencies
- **Target agent** — who receives the handoff

The handoff:
- Transfers task ownership automatically
- Injects the full handoff context into the receiving agent's notification (not just "task reassigned")
- Is visible in the channel to all agents (transparent, not a DM)
- Gets stored on the task for future reference

**Validation rules**:
- Caller (`my_slug`) must be the current task owner
- Target agent (`to_agent`) must be an enabled member of the task's channel
- Task must exist and be in `in_progress` or `open` status

**Pending handoff detection** for `team_poll`: A handoff is "pending" for the `to_agent` when the task's current owner matches `to_agent` and the task status is `in_progress`. Once the agent acks the task or completes it, the handoff is no longer pending.

The key principle: **the sending agent is responsible for packaging context before letting go.**

### Full Flow Example

1. Human: "build a landing page"
2. CEO uses `team_plan` → creates tasks with dependencies
3. Designer picks up first task, acks, does the work
4. Designer uses `team_handoff` to FE: "Figma spec attached, 3 sections, mobile-first, use these hex values"
5. FE gets notified with the full handoff context, builds it
6. FE uses `team_conclude`: "Landing page shipped. 3 sections, responsive, deployed to /landing. Open item: copy needs CMO review."
7. Human sees conclusion summary in TUI — clean, forwardable output
8. Thread stops auto-notifying. CMO can be tagged if needed for copy review.

## Data Model Changes

### broker.go

**Thread conclusion storage:**
```go
type conclusionSummary struct {
    Discussed string `json:"discussed"`
    Decided   string `json:"decided"`
    Done      string `json:"done"`
    OpenItems string `json:"open_items,omitempty"`
}

type threadConclusion struct {
    ThreadID    string            `json:"thread_id"`    // Root message ID
    Channel     string            `json:"channel"`
    Summary     conclusionSummary `json:"summary"`
    ConcludedBy string            `json:"concluded_by"`
    ConcludedAt string            `json:"concluded_at"` // RFC3339
}
```

Add `conclusions []threadConclusion` to `Broker` struct and `brokerState`. Wire into `loadState()` and `saveLocked()` (explicit field copy, matching existing pattern).

**Handoff storage on tasks:**
```go
type taskHandoff struct {
    FromAgent string `json:"from_agent"`
    ToAgent   string `json:"to_agent"`
    WhatIDid  string `json:"what_i_did"`
    WhatToDo  string `json:"what_to_do"`
    Context   string `json:"context"`
    CreatedAt string `json:"created_at"` // RFC3339
}
```

Add `Handoffs []taskHandoff` to `teamTask`. (Persisted automatically via existing `Tasks` field in `brokerState`.)

**New endpoints:**

`POST /conclude` — Conclude a thread with summary.
```json
{
    "channel": "general",
    "thread_id": "msg-42",
    "discussed": "Landing page layout options",
    "decided": "3-section mobile-first approach",
    "done": "Figma spec complete, assets exported",
    "open_items": "Copy needs CMO review",
    "concluded_by": "designer"
}
```
Validates: `concluded_by` participated in thread. Thread not already concluded. Returns 200 with `{"conclusion": ...}`.

`POST /conclude/reopen` — Reopen a concluded thread.
```json
{
    "channel": "general",
    "thread_id": "msg-42",
    "reason": "CMO needs to review copy",
    "slug": "ceo"
}
```
Validates: `slug` is the lead agent. Returns 200 with `{"reopened": true}`.

`GET /conclusions?channel=general&thread_id=msg-42&limit=20` — List conclusions for a channel. All params optional except `channel`.

`POST /handoff` — Create a handoff and transfer task ownership.
```json
{
    "channel": "general",
    "task_id": "task-3",
    "from_agent": "designer",
    "to_agent": "fe",
    "what_i_did": "Figma spec complete, 3 sections, assets in /public",
    "what_to_do": "Build responsive HTML/CSS from spec",
    "context": "Mobile-first, use design tokens in DESIGN.md"
}
```
Validates: `from_agent` is current task owner, `to_agent` is enabled channel member, task exists and is in_progress/open. Returns 200 with `{"task": ...}`.

**Action log entries**: `team_conclude` records kind `thread_concluded`, `team_handoff` records kind `task_handoff`, `team_reopen` records kind `thread_reopened`.

**Public accessor**: `IsThreadConcluded(channel, threadID string) bool` — used by launcher for notification gating.

### launcher.go

**Notification gating in `notificationTargetsForMessage()`:**
```
IF thread is concluded AND message is NOT explicitly tagged:
    skip thread-participant notifications (only CEO if tagged)
ELSE:
    existing logic (tags → tagged+CEO, thread → participants+CEO, else → broadcast)
```

Tagged messages pierce the concluded barrier. CEO messages always go through.

**Handoff notification injection in `deliverTaskNotification()`:**
When the action is `task_handoff`, inject the full handoff context into the notification text sent to the receiving agent:
```
[Handoff from @designer on task-3]: What was done: ... What to do: ... Context: ...
```

**New helper:**
- `isThreadConcluded(channel, threadID)` — calls `broker.IsThreadConcluded()`

### server.go (MCP tools)

**`team_conclude`** — Conclude a thread with a structured summary:
```go
type TeamConcludeArgs struct {
    Channel   string `json:"channel,omitempty"`
    ThreadID  string `json:"thread_id" jsonschema:"Root message ID of the thread to conclude"`
    Discussed string `json:"discussed" jsonschema:"What topics were covered"`
    Decided   string `json:"decided" jsonschema:"What decisions were made"`
    Done      string `json:"done" jsonschema:"What concrete work was completed"`
    OpenItems string `json:"open_items,omitempty" jsonschema:"Anything remaining or handed off"`
    MySlug    string `json:"my_slug,omitempty"`
}
```

Also posts a `[CONCLUSION]` message into the thread via `/messages` for TUI visibility.

**`team_handoff`** — Hand off a task to another agent with context:
```go
type TeamHandoffArgs struct {
    Channel  string `json:"channel,omitempty"`
    TaskID   string `json:"task_id" jsonschema:"Task to hand off"`
    ToAgent  string `json:"to_agent" jsonschema:"Agent slug receiving the handoff"`
    WhatIDid string `json:"what_i_did" jsonschema:"What you completed"`
    WhatToDo string `json:"what_to_do" jsonschema:"What the receiving agent should do"`
    Context  string `json:"context,omitempty" jsonschema:"Relevant details, gotchas, dependencies"`
    MySlug   string `json:"my_slug,omitempty"`
}
```

**`team_reopen`** — Reopen a concluded thread (CEO/lead only):
```go
type TeamReopenArgs struct {
    Channel  string `json:"channel,omitempty"`
    ThreadID string `json:"thread_id" jsonschema:"Root message ID of the thread to reopen"`
    Reason   string `json:"reason" jsonschema:"Why the thread needs to be reopened"`
    MySlug   string `json:"my_slug,omitempty"`
}
```

**`team_poll` changes:**
- Append `## Recent Conclusions` section with concluded threads in this channel (last 5)
- Append `## Pending Handoffs` section when the polling agent has a pending handoff (task owned by them with unacked handoff context)

### prompts.go

**CEO prompt addition:**
```
When a body of work is complete, use team_conclude to close the thread with a summary.
The summary should be something the human can read and forward — not internal shorthand.

When assigning work across agents, prefer team_handoff over raw task reassignment.
Handoffs carry context: what was done, what's next, and what the receiving agent needs to know.
```

**Specialist prompt addition:**
```
When you finish your piece of work and another agent needs to continue:
- Use team_handoff to transfer with context, not just task reassignment
- Include what you did, what's left, and any gotchas

When a discussion reaches a conclusion:
- Use team_conclude with a clear summary
- The summary is shown directly to the human — write it for them, not for agents
```

## TUI Rendering (Out of Scope)

Conclusion summaries will appear as `[CONCLUSION]` messages in the thread via the existing message rendering path. Enhanced TUI rendering (dedicated conclusion cards, sidebar indicators, etc.) is a separate effort. The dual-storage approach means the basic case works without TUI changes.

## What This Does NOT Change

- Broadcast model stays — handoffs and conclusions are visible to all
- Existing task lifecycle stays — conclude/handoff layers on top, no auto-complete coupling
- CEO still sees everything
- Channel membership unchanged
- Message structure unchanged (threads are identified by replyTo chains as before)

## Verification

1. `go build -o wuphf ./cmd/wuphf` compiles
2. `go test ./...` passes
3. Manual: Agent concludes thread → thread stops generating auto-notifications
4. Manual: Tagged message in concluded thread still delivers
5. Manual: Conclusion summary appears as `[CONCLUSION]` message in thread
6. Manual: Agent hands off task → receiving agent gets full handoff context in notification
7. Manual: Handoff to disabled agent → rejected with error
8. Manual: Non-participant tries to conclude → rejected
9. Manual: CEO reopens concluded thread → notifications resume
10. Manual: `team_poll` shows recent conclusions and pending handoffs
11. Manual: Double-conclude same thread → returns existing conclusion (idempotent)
