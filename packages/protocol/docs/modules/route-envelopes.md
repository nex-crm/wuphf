# Module: ROUTE ENVELOPES

> Path: `packages/protocol/src/route-envelopes.ts` · Owner: protocol ·
> Stability: draft

## 1. Purpose

The route-envelope module owns the JSON bodies for broker HTTP routes that
cross the thread and approval boundaries. Brokers should decode request bodies
with these codecs and emit serializer output from these codecs rather than
hand-rolling route JSON.

## 2. Versioning

Thread and approval request/response envelopes carry `schemaVersion: 1`.
Decoders accept an absent `schemaVersion` as v1 for first-rollout
compatibility, but serializers always emit `1`. Future versions greater than
the supported version are rejected.

The shared route error body intentionally remains the exact unversioned
`{ error, message?, retryAfterMs? }` shape used for small 4xx/5xx responses.

## 3. Thread Route Shapes

`/api/v1/threads` uses:

| Codec | Wire fields |
|---|---|
| `threadCreateRequestFromJson` / `threadCreateRequestToJsonValue` | `{ schemaVersion?, title, specContent, externalRefs?, idempotencyKey }` |
| `threadSpecEditRequestFromJson` / `threadSpecEditRequestToJsonValue` | `{ schemaVersion?, baseRevisionId, baseContentHash, content, idempotencyKey }` |
| `threadStatusChangeRequestFromJson` / `threadStatusChangeRequestToJsonValue` | `{ schemaVersion?, fromStatus, toStatus, idempotencyKey }` |
| `threadMutationResponseFromJson` / `threadMutationResponseToJsonValue` | `{ schemaVersion?, threadId, headLsn, revisionId, contentHash }` |
| `threadListResponseFromJson` / `threadListResponseToJsonValue` | `{ schemaVersion?, threads, nextCursor? }` |
| `threadGetResponseFromJson` / `threadGetResponseToJsonValue` | `{ schemaVersion?, thread }` |

`threads` and `thread` wrap the existing snake_case `Thread` codec output.
The mutation response includes the accepted revision id and content hash so a
client can perform the next OCC spec edit without a follow-up GET.

## 4. Approval Route Shapes

`/api/v1/approvals` uses:

| Codec | Wire fields |
|---|---|
| `approvalRequestCreateRequestFromJson` / `approvalRequestCreateRequestToJsonValue` | `{ schemaVersion?, claim, scope, riskClass, threadId?, taskId?, receiptId?, idempotencyKey }` |
| `approvalDecisionRequestFromJson` / `approvalDecisionRequestToJsonValue` | `{ schemaVersion?, decision, token?, idempotencyKey }` |
| `approvalRequestCreateResponseFromJson` / `approvalRequestCreateResponseToJsonValue` | `{ schemaVersion?, approvalRequest, headLsn }` |
| `approvalDecisionResponseFromJson` / `approvalDecisionResponseToJsonValue` | `{ schemaVersion?, approvalRequest, headLsn }` |
| `approvalViewFromJson` / `approvalViewToJsonValue` | `{ schemaVersion, id, claim, scope, riskClass, threadId?, taskId?, receiptId?, requestedBy, requestedAt, status, decisionSummary? }` |
| `approvalListResponseFromJson` / `approvalListResponseToJsonValue` | `{ schemaVersion?, approvals, nextCursor? }` |
| `approvalGetResponseFromJson` / `approvalGetResponseToJsonValue` | `{ schemaVersion?, approval }` |

Approval envelopes reuse the existing `ApprovalClaim`, `ApprovalScope`,
`SignedApprovalToken`, and `ApprovalRequest` codecs for write-side create and
decision routes. The create request re-checks claim/scope bindings and receipt
co-sign top-level `receiptId` and `riskClass` bindings. The decision request
requires a token when `decision` is `approve`.

Read-side approval routes use `ApprovalView`, a distinct token-redacted
projection. `decisionSummary` contains only `{ decision, decidedBy, decidedAt }`
and the codec rejects a `token` field. Pending approvals must omit
`decisionSummary`; non-pending approvals must include it and its `decision` must
match `status`. List responses are capped by `MAX_ROUTE_APPROVAL_LIST_ITEMS`
and include `nextCursor?` for pagination.

## 5. Invariants

1. Every object boundary rejects unknown keys with `assertKnownKeys`.
2. Every variable-length string is covered by a field budget: thread titles,
   spec content canonical JSON, idempotency keys, cursor strings, route error
   codes, route error messages, and nested artifact strings.
3. Serializers are field-enumerated and route bytes are canonicalized through
   `canonicalJSON` in tests and vectors.
4. Route list responses are capped by `MAX_ROUTE_THREAD_LIST_ITEMS` or
   `MAX_ROUTE_APPROVAL_LIST_ITEMS`.
5. `testdata/route-envelope-vectors.json` and
   `testdata/verifier-reference.go` pin accepted and rejected wire behavior for
   Go/Rust implementers.
