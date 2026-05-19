# @wuphf/broker

WUPHF v1 broker — loopback HTTP + SSE + WebSocket listener with a DNS-rebinding
guard, bearer-token auth, and the `/api-token` bootstrap. Pure Node; no Electron.

This package is the boundary between the renderer (Electron WebView or a regular
browser tab) and the broker's own state (receipt store, agent runners, AI
gateway). Everything above this layer is **app data** that travels
HTTP/SSE/WebSocket only — `contextBridge` carries OS verbs, never broker
state.

Receipts are the broker's first persistent surface. A receipt is a
tamper-evident record of an agent run (see `@wuphf/protocol`'s
`ReceiptSnapshot`); the broker exposes the receipt write path via REST so
hosts can persist runs behind a loopback boundary. Two `ReceiptStore`
implementations:

- `InMemoryReceiptStore` (default) — process-local; lost across restarts.
  Useful for tests and headless smoke runs.
- `SqliteReceiptStore` from `@wuphf/broker/sqlite` — durable, SQLite
  event-log-backed. Loads the native `better-sqlite3` binding only when
  imported, so consumers that only need the in-memory path don't pay the
  cost. Hosts (e.g. the Electron utility process) wire this in by
  passing `receiptStore: SqliteReceiptStore.open({ path })`, or by using
  `SqliteReceiptStore.fromDatabase(db, eventLog)` with
  `createThreadSubsystem(db, eventLog, receiptStore)` when thread routes are
  mounted.

## API

```ts
import { createBroker, InMemoryReceiptStore } from "@wuphf/broker";

const broker = await createBroker({
  port: 0, // ephemeral; broker.port reports the assigned value
  renderer: { dir: "/abs/path/to/renderer/dist" }, // or null to disable static
  // Optional. Defaults to a fresh InMemoryReceiptStore per createBroker call.
  receiptStore: new InMemoryReceiptStore(),
});

// broker.url   = "http://127.0.0.1:<port>"
// broker.port  = BrokerPort
// broker.token = ApiToken
await broker.stop();
```

Hosts that supply their own `receiptStore` own its lifecycle: `broker.stop()`
closes the HTTP/WebSocket surface and the WS server, but it does NOT close
the injected store. Call `store.close()` (or equivalent) after `broker.stop()`
to release any underlying handle.

For the durable store:

```ts
import { createBroker } from "@wuphf/broker";
import { SqliteReceiptStore } from "@wuphf/broker/sqlite";

const store = SqliteReceiptStore.open({ path: "/abs/path/event-log.sqlite" });
const broker = await createBroker({ port: 0, receiptStore: store });
// ...
await broker.stop();
store.close();
```

For thread routes, construct the receipt store from the same database/event-log
handle and pass the subsystem to the broker:

```ts
import { createEventLog, openDatabase, runMigrations } from "@wuphf/broker/event-log";
import { SqliteReceiptStore } from "@wuphf/broker/sqlite";
import { createThreadSubsystem } from "@wuphf/broker/threads";

const db = openDatabase({ path: "/abs/path/event-log.sqlite" });
runMigrations(db);
const eventLog = createEventLog(db);
const receiptStore = SqliteReceiptStore.fromDatabase(db, eventLog);
const threads = createThreadSubsystem(db, eventLog, receiptStore);
const broker = await createBroker({ port: 0, threads });
```

## Routes

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/api-token` | none (loopback only) | Returns `{ token, broker_url }` (snake_case wire). |
| GET | `/api/health` | bearer | Returns `{ "ok": true }`. Process-only liveness — does NOT probe the receipt store. Storage failures surface on the receipt routes themselves. A dedicated storage-diagnostics endpoint is tracked as future work. |
| GET | `/api/events` | bearer | SSE stream; emits `ready`, keepalive comments, and invalidation-only `thread.created`, `thread.updated`, and `thread.pinned_approvals.changed` events when thread routes are mounted. Thread event ids are committed LSNs; clients must refetch on `ready`, reconnect, and every thread invalidation. |
| POST | `/api/receipts` | bearer | Body: receipt JSON. 201 + canonical body on insert, 409 on `id` collision, 400 on parse/validation or a V2 `threadId` that does not exist in the SQLite thread projection, 413 on `> 1 MiB`, 415 on non-JSON content-type, 507 `{"error":"store_full"}` when the store reaches `maxReceipts` or `SqliteReceiptStore` hits `SQLITE_FULL`, 503 `{"error":"store_busy"}` + `Retry-After: 1` for transient `SQLITE_BUSY`/`LOCKED`, 503 `{"error":"storage_error"}` for persistent `SQLITE_READONLY`/`SQLITE_IOERR_*`/`SQLITE_CORRUPT`. |
| GET | `/api/receipts/:id` | bearer | 200 + receipt JSON on hit, 404 on miss or malformed id. |
| GET | `/api/v1/threads/:tid/receipts` | bearer | Canonical JSON array of V2 receipts in the thread. Supports `?cursor=<opaque>` (empty string ≡ absent) and `?limit=<positive integer>` (1–1000; default `MAX_LIST_LIMIT=1000`). Responses include `Link: <...>; rel="next"` when another page exists. Returns 400 on invalid cursor/limit and 404 on malformed thread id. |
| GET | `/api/threads/:tid/receipts` | bearer | One-release alias for `/api/v1/threads/:tid/receipts`. |
| GET | `/api/v1/threads` | bearer | Lists folded thread views through `threadListResponseToJsonValue({ threads })`. Each view includes read-time `effectiveStatus`, optional `attentionReason`, `boardColumn`, `currentSeat`, and `pendingApprovalCount`. Supports `?status=` by stored/effective status or board column. `Thread.task_ids` comes from the bounded `thread_receipts` projection. Mounted when `createBroker({ threads })` is supplied. |
| GET | `/api/v1/threads/replay-check` | bearer | Replays `thread.*`, `approval.*`, and thread-scoped `receipt.put` events from LSN 0, compares the live thread state, pinned approvals, receipt index, and read-time effective status projections, and returns `ThreadReplayCheckReport` through `threadReplayCheckReportToJsonValue`. 200 when `ok: true`; 500 with structured discrepancies on drift or historical invariant violations. |
| GET | `/api/v1/threads/:id` | bearer | Returns one thread view through `threadGetResponseToJsonValue({ thread })`, or 404 on malformed/missing id. |
| GET | `/api/v1/threads/:id/pinned-approvals` | bearer | Returns `{ threadId, headLsn, approvals }` through `threadPinnedApprovalsResponseToJsonValue`. `approvals` is the token-redacted `ApprovalView[]` for `pending_approvals` rows with matching `thread_id` and `status='pending'`. |
| POST | `/api/v1/threads` | bearer | Creates a thread and its initial spec revision. Body parses through `threadCreateRequestFromJson`: `{ title, specContent, externalRefs?, idempotencyKey }`; `idempotencyKey` must be a 26-char ULID and becomes the thread id and initial revision id. 201 returns `threadMutationResponseToJsonValue({ threadId, headLsn, revisionId, contentHash })`; duplicate idempotency keys replay the original response without appending events. |
| PATCH | `/api/v1/threads/:id/spec` | bearer | Appends a spec revision with OCC. Body parses through `threadSpecEditRequestFromJson`: `{ baseRevisionId, baseContentHash, content, idempotencyKey }`; `idempotencyKey` must be a 26-char ULID and becomes the new revision id. Stale bases return 409. 200 returns the mutation response envelope. |
| PATCH | `/api/v1/threads/:id/status` | bearer | Appends a status transition. Body parses through `threadStatusChangeRequestFromJson`: `{ fromStatus, toStatus, idempotencyKey }`. `fromStatus` mismatches return 409; attempts to transition out of `merged`/`closed` return 422. 200 returns the mutation response envelope for the unchanged current spec revision. |
| POST | `/api/v1/cost/events` | bearer + operator capability | Body: cost event JSON. Requires `Idempotency-Key: cmd_cost.event_<ULID>`, `X-Operator-Identity`, and `X-Operator-Capability` when `cost.operatorToken` is configured. 201 returns `{ lsn, agentDayTotal, taskTotal, newCrossings }`; duplicate idempotency keys replay the original response. |
| POST | `/api/v1/cost/budgets` | bearer + operator capability | Body: budget JSON. Requires `Idempotency-Key: cmd_cost.budget.set_<ULID>`, `X-Operator-Identity`, and `X-Operator-Capability` when configured. The broker overwrites `setBy`/`setAt` server-side. 201 returns `{ lsn, tombstoned }`; `limitMicroUsd: 0` returns 400 because tombstones use DELETE. |
| DELETE | `/api/v1/cost/budgets/:id` | bearer + operator capability | Tombstones an existing budget. Requires `Idempotency-Key: cmd_cost.budget.tombstone_<ULID>`, `X-Operator-Identity`, and `X-Operator-Capability` when configured. 200 returns `{ lsn, tombstoned: true }`; 404 on malformed or missing id. |
| POST | `/api/v1/cost/idempotency/prune` | bearer + operator capability | Deletes `command_idempotency` rows older than `?olderThanMs=<positive-ms>`; defaults to 24h. Returns `{ pruned, olderThanMs, cutoffMs }`. Event-log and projection rows are never deleted. |
| GET | `/api/v1/cost/budgets` | bearer | Lists projected budgets as `{ budgets: [...] }`. |
| GET | `/api/v1/cost/budgets/:id` | bearer | Returns one projected budget, or 404 on malformed/missing id. |
| GET | `/api/v1/cost/summary` | bearer | Returns current cost projections: agent spend, budgets, and threshold crossings. |
| GET | `/api/v1/cost/replay-check` | bearer | Replays cost events and compares projections. 200 when `ok: true`; 500 with structured discrepancies when drift or unparseable cost payloads are found. |
| POST | `/api/v1/approvals` | bearer | Body: `ApprovalRequestCreateRequest` route envelope from `@wuphf/protocol` with `idempotencyKey`. Appends `approval.requested`, creates a `pending` folded `ApprovalRequest`, returns `ApprovalRequestCreateResponse`, and emits `approval.requested`. If thread routes are mounted and the request omits `threadId`, the broker assigns the per-broker inbox thread id. Thread-scoped approvals also emit `thread.pinned_approvals.changed`. If `idempotencyKey` is an `ApprovalRequestId` ULID the broker uses it as the request id; otherwise it derives a stable request id from that key. |
| GET | `/api/v1/approvals` | bearer | Lists token-redacted approval views as `ApprovalListResponse`. Supports `?status=pending\|approved\|rejected\|abstained`, `?threadId=<ThreadId>`, `?taskId=<TaskId>`, `?limit=<n>` capped to the protocol maximum, and `?cursor=<EventLsn>` pagination. |
| GET | `/api/v1/approvals/:id` | bearer | Returns one token-redacted approval view as `ApprovalGetResponse`, or 404 on malformed/missing id. |
| POST | `/api/v1/approvals/:id/decision` | bearer | Body: `ApprovalDecisionRequest` route envelope from `@wuphf/protocol` with `idempotencyKey`. `approve` requires a `SignedApprovalToken`; `reject`/`abstain` do not carry one. Appends `approval.decided`, validates the folded terminal approval through the protocol codec, records the supplied approve token without WebAuthn verification, returns `ApprovalDecisionResponse`, emits `approval.decided`, emits `thread.pinned_approvals.changed` for thread-scoped approvals, and returns 409 if already decided. Malformed JSON returns 400; route-envelope or approval-command validation failures return 422. |
| GET | `/api/agents/:agentId/provider-routing` | bearer | Returns the per-agent provider-routing config via `agentProviderRoutingToJsonValue`. Mounted when `createBroker({ runners: { agentProviderRoutingStore } })` is supplied. |
| PUT | `/api/agents/:agentId/provider-routing` | bearer | Replaces all routes for the agent. Body parses through `agentProviderRoutingWriteRequestFromJson`; the body `agentId` must match the path `agentId`. |
| POST | `/api/webauthn/registration/challenge` | bearer + agent map + enrollable role | Broker control-plane route. Body `{ role }`; the role must be allowed by `webauthn.enrollableRoles` for the bearer-mapped agent. Returns `{ challengeId, creationOptions }` where `creationOptions` is the W3C `PublicKeyCredentialCreationOptions` JSON from `@simplewebauthn/server`. Mounted when `createBroker({ webauthn })` is supplied. Store writes map `SQLITE_BUSY`/`LOCKED` to 503 + `Retry-After`, `SQLITE_FULL` to 507, and storage-unavailable errors to `503 {"error":"storage_error"}`. |
| POST | `/api/webauthn/registration/verify` | bearer + agent map | Broker control-plane route. Body `{ challengeId, attestationResponse }`; verifies attestation with `@simplewebauthn/server`, persists the credential against the bearer-mapped agent and authorized challenge role, then returns `{ credentialId, role }`. Store write failures use the same 503/507 mapping as registration challenge creation. |
| POST | `/api/webauthn/cosign/challenge` | bearer + agent map | Broker control-plane route. Body `{ claim, scope }`; parses claim/scope through `@wuphf/protocol`, requires agent-scoped claims to target the bearer-mapped agent, binds a random WebAuthn challenge to the canonical preimage, and returns `{ challengeId, requestOptions }` where `requestOptions` is the W3C `PublicKeyCredentialRequestOptions` JSON. Store write failures use the same 503/507 mapping as registration challenge creation. |
| POST | `/api/webauthn/cosign/verify` | bearer + agent map | Verifies assertion origin, RP ID, credential id, role, monotonic sign count, and threshold. Pending thresholds return `{ status: "approval_pending", satisfiedRoles, requiredThreshold }`; satisfied thresholds return `signedApprovalTokenToJsonValue(token)`. Replays of consumed `tokenId`s return the recorded outcome only within the challenge validity window; after expiry they return `400 {"error":"challenge_expired"}`. Store write failures use the same 503/507 mapping as registration challenge creation. |
| POST | `/api/runners` | bearer + runner agent map | Body: `RunnerSpawnRequest`. The bearer maps to one `agentId`; mismatches return 403. The broker mints/injects `BrokerIdentity`, resolves the `CredentialHandle`, and returns `{ runnerId }`. Mounted only when `createBroker({ runners })` is supplied. |
| GET | `/api/runners/:id/events` | bearer + runner agent map | SSE stream of `RunnerEvent` values. The caller's bearer-mapped `agentId` must match the runner owner. |
| GET | `/`, `/index.html` | none (loopback) | Renderer bundle (404 if `renderer: null`). |
| GET | `/assets/*` | none (loopback) | Static assets under the renderer dir. |
| WS | `/terminal/agents/:slug?token=` | token + loopback origin | Currently closes with `1011 not_implemented`; the agent stdio bridge replaces this in a later branch. |

Receipt thread-list pagination keeps the response body as a bare JSON array.
Follow the RFC 8288 `Link` header to continue:

```http
GET /api/v1/threads/01ARZ3NDEKTSV4RRFFQ69G5FAZ/receipts?limit=2
Link: </api/v1/threads/01ARZ3NDEKTSV4RRFFQ69G5FAZ/receipts?cursor=bHNuOjI&limit=2>; rel="next"

GET /api/v1/threads/01ARZ3NDEKTSV4RRFFQ69G5FAZ/receipts?cursor=bHNuOjI&limit=2
```

## Invariants

1. **Bind is `127.0.0.1` only.** Never `0.0.0.0`, never a LAN IP.
2. **DNS-rebinding guard runs on every request.** Both `Host`
   (`127.0.0.1` or `localhost`, with an optional port) and `RemoteAddr`
   (loopback peer IP) must pass.
3. **Constant-time token comparison.** Bearer compare goes through
   `node:crypto.timingSafeEqual`.
4. **Token is bootstrap-only on `/api-token`.** Every other API surface requires
   the bearer; `/api-token` is loopback-guarded but returns the token to the
   first same-origin caller.
5. **No app data over `contextBridge`.** This package never imports `electron`.
6. **Receipts are insert-if-absent.** A POST with an `id` that already exists
   returns 409 and the stored value is **not** overwritten. Idempotency-key
   semantics (same id + byte-identical payload → 200 no-op) are deferred to a
   future widening of `put`'s return shape.
7. **Receipts go through the protocol codec.** `receiptFromJson` runs the full
   validator (boundary budget, frozen-args canonicalization, shape, branded
   ids) before the broker even touches the store. There is no fast-path that
   skips validation.
8. **Runner credentials stay broker-mediated.** The broker route maps bearer
   tokens to `AgentId`, injects the `BrokerIdentity`, and passes runners only a
   `secretReader` closure. Runner processes never receive or construct broker
   identity.
9. **Provider-routing writes are path-bound.** The URL `agentId` and request
   body `agentId` must match before the broker writes routing state.
10. **WebAuthn ceremony objects are broker control-plane shapes.** The
    creation/request option JSON is the standardized W3C shape emitted by
    `@simplewebauthn/server`, not a WUPHF protocol wire contract. Final
    approval tokens still go through `signedApprovalTokenToJsonValue`.
11. **WebAuthn registration roles are broker-authorized.** The request body can
    ask for a role, but only roles listed for the bearer-mapped agent in
    `webauthn.enrollableRoles` can receive a registration challenge. Agents
    without an explicit entry cannot enroll any role.
12. **Packaged WebAuthn uses localhost as the browser origin.** The listener
    still binds `127.0.0.1`, but the desktop loads
    `http://localhost:<port>/` and the broker appends
    `http://localhost:<port>` to WebAuthn `allowedOrigins` so the RP ID
    `localhost` matches the page origin.
13. **Pending approvals are explicit backend events.** The broker appends
    `approval.requested` / `approval.decided` events and projects
    `pending_approvals`; it does not derive pending approvals from
    `receipt.approvals[]`.
14. **Effective thread status is read-time only.** There is no stored
    `effective_status` projection. The route layer derives it from stored
    thread status, pending pinned approval count, and the latest thread receipt
    status.
15. **Pinned approvals are a bounded query.** A thread's pinned approvals are
    `pending_approvals` rows for that `thread_id` with `status='pending'`.
    Threadless approval creates default to the deterministic inbox thread when
    the thread subsystem is mounted.

## Spec anchors

- `business-musings/wuphf-greenfield-rewrite-rfc-2026-05.md` §7.3 (IPC discipline) and §15 Stream A row "feat/broker-loopback-listener".
- `docs/architecture/broker-contract.md` (the v0 broker contract this branch carries forward to v1).
- `@wuphf/protocol#ApiBootstrap`, `BrokerUrl`, and `isLoopbackRemoteAddress`.

## Validation

```bash
bun run typecheck
bun run test
bun run check
bun run check:invariants
```
