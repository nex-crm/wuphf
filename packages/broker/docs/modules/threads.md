# `src/threads/*` — thread foundation

The thread foundation records thread lifecycle commands as event-log rows and
projects them into the disposable `threads` SQLite table. Hosts opt in with
`createBroker({ threads })` after constructing one cohesive handle with
`createThreadSubsystem(db, eventLog, receiptStore)`. The subsystem binds the
appender, folded state, `thread_receipts` index, replay, and receipt store to
the same SQLite provenance. Startup also ensures one deterministic system inbox
thread exists as a normal `threads` row; approvals created without an explicit
thread id default to that inbox when approvals and threads are mounted together.

Command routes are idempotent with the same `command_idempotency` table used by
the cost ledger. Thread route request bodies parse through
`@wuphf/protocol` route-envelope codecs and carry a 26-character ULID
`idempotencyKey`; create uses that ULID as both the thread id and initial spec
revision id, and spec edits use it as the new revision id. Duplicate keys replay
the original response bytes and append no new event.

The appender validates under a `BEGIN IMMEDIATE` transaction and reads the
target thread's current revision/status from the keyed `threads` projection,
not by folding unrelated history. Spec edits require both `baseRevisionId` and
`baseContentHash`; stale bases return 409. Accepted spec revision ids are
globally unique via `thread_spec_revisions`. Status changes require
`fromStatus` to match the projected head; terminal exits from `merged` or
`closed` return 422.

Reads return protocol `threadListResponseToJsonValue` /
`threadGetResponseToJsonValue` bodies. The route layer enriches each thread
with read-time `effectiveStatus`, optional `attentionReason`, `boardColumn`,
`currentSeat`, and `pendingApprovalCount`; these fields are not stored
projection columns. `Thread.task_ids` comes from the bounded `thread_receipts`
projection in first-receipt LSN order; full receipt enumeration stays behind
`GET /api/v1/threads/:id/receipts`.

Effective status is derived deterministically in priority order: stored
`merged`/`closed` stays terminal; pending pinned approvals become
`needs_attention` with `pending_approval`; latest receipt `error` becomes
`needs_attention` with `failed`; latest receipt `stalled` becomes
`needs_attention` with `stalled`; otherwise stored status passes through.
Board columns are `needs_me`, `review`, `done`, or `running` from that effective
status. `currentSeat` is `human` for `needs_attention` and stored
`needs_review`, otherwise `agent`.

Pinned approvals are a read-time query over the approvals projection:
`pending_approvals.thread_id = :threadId AND status = 'pending'`. The response
for `GET /api/v1/threads/:id/pinned-approvals` uses
`threadPinnedApprovalsResponseToJsonValue` and returns token-redacted
`ApprovalView[]`.

Accepted create commands emit invalidation-only `thread.created` SSE events.
Accepted spec and status changes emit `thread.updated`. Approval request/decision
events scoped to a thread emit `thread.pinned_approvals.changed`. The SSE data
is a validated protocol `ThreadStreamEvent` carrying `{ threadId, headLsn }`,
and the SSE event id is the committed LSN. Clients must refetch on `ready`,
reconnect, and every thread invalidation. Last-Event-ID backfill from
`event_log` is a documented TODO; the thread body is never streamed.
