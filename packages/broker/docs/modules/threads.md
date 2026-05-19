# `src/threads/*` — thread foundation

The thread foundation records thread lifecycle commands as event-log rows and
projects them into the disposable `threads` SQLite table. Hosts opt in with
`createBroker({ threads: { appender, state } })` after constructing
`createThreadStateStore(db)` and `createThreadAppender(db, eventLog, state)`.

Command routes are idempotent with the same `command_idempotency` table used by
the cost ledger. Header keys are `cmd_thread.create_<ULID>`,
`cmd_thread.spec.edit_<ULID>`, and `cmd_thread.status.change_<ULID>`. Duplicate
keys replay the original response bytes and append no new event.
`ThreadCreateCommand` does not carry a revision id, so the create key's ULID
suffix becomes the initial spec revision id.

The appender folds the target thread from `event_log` inside the same
`BEGIN IMMEDIATE` transaction that assigns new LSNs. It then runs the protocol
validators for command shape, spec revision chains, status folds, and foreign
keys before appending events and updating the projection. Spec edits require
both `baseRevisionId` and `baseContentHash`; stale bases return 409. Status
changes require `fromStatus` to match the folded head; terminal exits from
`merged` or `closed` return 422.

Reads return the folded projection as a protocol `threadToJsonValue(thread)`
body plus broker metadata (`head_lsn`, `receipt_ids`). `task_ids` and
`receipt_ids` are derived at read time from `ReceiptStore.list({ threadId })` in
LSN order. No receipt-side reactor is involved in this slice.

Accepted create commands emit invalidation-only `thread.created` SSE events.
Accepted spec and status changes emit `thread.updated`. The SSE data is a
validated protocol `ThreadStreamEvent` carrying `{ threadId, headLsn }`; the
thread body is never streamed.
