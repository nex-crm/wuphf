# Module: THREAD

> Path: `packages/protocol/src/thread.ts`, receipt V2 bridge in
> `packages/protocol/src/receipt.ts` · Owner: protocol · Stability: draft

## 1. Purpose

The thread module defines the protocol slice for durable work threads:
bounded branded IDs, thread/spec/external-ref schemas, snake_case wire codecs,
receipt V2 attachment, audit payload validators, status-fold helpers, and
projection assertion helpers. Posts, cross-thread links, renderer surfaces, and
HTTP handlers are deliberately deferred to broker and Stream B work.

## 2. Lifecycle

```mermaid
sequenceDiagram
  participant Client
  participant Broker
  participant Protocol as @wuphf/protocol
  participant Audit
  Client->>Broker: create thread command + idempotencyKey
  Broker->>Protocol: validate ThreadCreatedAuditPayload
  Broker->>Audit: thread_created full payload
  Client->>Broker: edit spec command + baseRevisionId
  Broker->>Protocol: validate contentHash == sha256(canonical(content))
  Broker->>Audit: thread_spec_edited full content
  Client->>Broker: change status command
  Broker->>Protocol: validateThreadStatusFold
  Broker->>Audit: thread_status_changed
  Broker-->>Client: thread.updated invalidation only
```

## 3. Status

```mermaid
stateDiagram-v2
  [*] --> open
  open --> in_progress
  open --> closed
  in_progress --> needs_review
  in_progress --> closed
  needs_review --> merged
  needs_review --> closed
  merged --> [*]
  closed --> [*]
```

## 4. Schemas

```mermaid
classDiagram
  class Thread {
    +ThreadId id
    +string title
    +ThreadStatus status
    +ThreadSpecRevision spec
    +ThreadExternalRefs externalRefs
    +TaskId[] taskIds
  }
  class ThreadSpecRevision {
    +ThreadSpecRevisionId revisionId
    +ThreadId threadId
    +ThreadSpecRevisionId|null baseRevisionId
    +JsonValue content
    +Sha256Hex contentHash
    +SignerIdentity authoredBy
  }
  class ThreadExternalRefs {
    +string[] sourceUrls
    +string[] entityIds
  }
  Thread --> ThreadSpecRevision
  Thread --> ThreadExternalRefs
```

## 5. Receipt Version Handling

```mermaid
flowchart TD
  A[receipt JSON] --> B{schemaVersion}
  B -->|1| C{threadId present?}
  C -->|yes| X[reject /threadId]
  C -->|no| V1[ReceiptSnapshotV1]
  B -->|2| D{threadId present?}
  D -->|yes| E[brand ThreadId]
  D -->|no| V2[ReceiptSnapshotV2 inbox-compatible]
  E --> V2
  B -->|other| Y[reject /schemaVersion]
```

## 6. Invariants

| # | Invariant | Protocol enforcer |
|---|---|---|
| 1 | Receipt V1 rejects `threadId`; V2 accepts optional `threadId`; both canonical round-trip. | `receiptFromJson`, `receiptToJson`, `validateReceipt` |
| 2 | `Thread.spec.contentHash == sha256(canonical(content))`; spec edit payload carries full content. | `validateThreadSpecRevision`, `validateThreadSpecEditedAuditPayload` |
| 3 | Spec edits carry `baseRevisionId` matching the prior accepted revision. | `validateThreadSpecRevisionChain` |
| 4 | Status fold matches prior status and never transitions out of terminal. | `validateThreadStatusFold`, `validateThreadStatusChangedAuditPayload` |
| 5 | Projection-side pinned approval state has a bounded invalidation shape. | `ThreadInvalidationPayload` stream event shape |
| 6 | Projection-side current thread state has bounded schemas. | `Thread`, `ThreadSpecRevision`, `ThreadExternalRefs` |
| 7 | Edits/status changes/receipt thread IDs reference existing threads or inbox. | `validateThreadForeignKeys` helper signature |
| 8 | `Thread.taskIds` equals unique V2 receipt task IDs for that thread. | `validateThreadReceiptIndex` |
| 9 | Thread commands require idempotency keys. | `validateThreadCommand` |

## 7. Audit Findings

| # | Spec section | File:line | Discrepancy | Severity | Fix needed |
|---|---|---:|---|---|---|

## 8. Test Coverage Gaps

| # | Spec section | What's untested | Why it matters | Suggested test |
|---|---|---|---|---|
