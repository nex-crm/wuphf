# Approval Request Module

> Path: `packages/protocol/src/approval-request.ts`.

The approval-request module defines the protocol artifact consumed by broker
routes and renderer projections for pending approvals. It mirrors the thread
slice: camelCase runtime interfaces, snake_case artifact JSON, camelCase audit
payload bodies, strict known-key rejection, and field-enumerated serializers.

## Public Surface

| Surface | Purpose |
|---|---|
| `ApprovalRequestId` | ULID brand exported from the receipt brand module. |
| `ApprovalRequest` | Folded projection state for one approval request. |
| `ApprovalDecisionRecord` | Decided state payload with optional cosign token. |
| `approvalRequestToJsonValue` / `approvalRequestFromJsonValue` | Snake_case artifact codec. |
| `validateApprovalRequest` | Runtime validator with claim/scope and status/decision invariants. |
| `approvalAuditPayloadToBytes` / `approvalAuditPayloadFromJsonValue` | Canonical JSON bytes for `approval_requested` and `approval_decided`. |

## Invariants

1. `ApprovalRequest.status === "pending"` requires `decision` to be absent.
2. Non-pending statuses require `decision`, and the decision must match the
   folded status: `approve -> approved`, `reject -> rejected`,
   `abstain -> abstained`.
3. `claim` and `scope` compose existing `ApprovalClaim` / `ApprovalScope`
   primitives and must bind the same fields as `SignedApprovalToken`.
4. Audit payloads carry full replay content, never hash-only summaries.
5. Stream events are invalidation-only: `{ requestId, threadId?, headLsn }`.

## Wire Shapes

`ApprovalRequest` artifact JSON:

```json
{
  "request_id": "01HRQ7KZ7D4E6F8G9H0J1K2M3N",
  "claim": {},
  "scope": {},
  "risk_class": "high",
  "thread_id": "01ARZ3NDEKTSV4RRFFQ69G5FAY",
  "task_id": "01ARZ3NDEKTSV4RRFFQ69G5FAW",
  "receipt_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "requested_by": "fran@example.com",
  "requested_at": "2026-05-08T18:00:00.000Z",
  "status": "pending",
  "schema_version": 1
}
```

Audit payload JSON stays camelCase to match the existing thread audit payload
family. The audit chain stores the canonical UTF-8 bytes from
`approvalAuditPayloadToBytes`, and `testdata/audit-event-vectors.json` pins
both `approval_requested` and `approval_decided` vectors.
