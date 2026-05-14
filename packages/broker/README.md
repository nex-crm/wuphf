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
  passing `receiptStore: SqliteReceiptStore.open({ path })`.

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

## Routes

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/api-token` | none (loopback only) | Returns `{ token, broker_url }` (snake_case wire). |
| GET | `/api/health` | bearer | Returns `{ "ok": true }`. Process-only liveness — does NOT probe the receipt store. Storage failures surface on the receipt routes themselves. A dedicated storage-diagnostics endpoint is tracked as future work. |
| GET | `/api/events` | bearer | SSE stream; emits `ready` then keepalive comments. |
| POST | `/api/receipts` | bearer | Body: receipt JSON. 201 + canonical body on insert, 409 on `id` collision, 400 on parse/validation, 413 on `> 1 MiB`, 415 on non-JSON content-type, 507 `{"error":"store_full"}` when the store reaches `maxReceipts` or `SqliteReceiptStore` hits `SQLITE_FULL`, 503 `{"error":"store_busy"}` + `Retry-After: 1` for transient `SQLITE_BUSY`/`LOCKED`, 503 `{"error":"storage_error"}` for persistent `SQLITE_READONLY`/`SQLITE_IOERR_*`/`SQLITE_CORRUPT`. |
| GET | `/api/receipts/:id` | bearer | 200 + receipt JSON on hit, 404 on miss or malformed id. |
| GET | `/api/threads/:tid/receipts` | bearer | JSON array of V2 receipts in the thread. Supports `?cursor=<opaque>` (empty string ≡ absent) and `?limit=<positive integer>` (1–1000; default `MAX_LIST_LIMIT=1000`). Responses include `Link: <...>; rel="next"` when another page exists. Returns 400 on invalid cursor/limit and 404 on malformed thread id. |
| POST | `/api/v1/cost/events` | bearer + operator capability | Body: cost event JSON. Requires `Idempotency-Key: cmd_cost.event_<ULID>`, `X-Operator-Identity`, and `X-Operator-Capability` when `cost.operatorToken` is configured. 201 returns `{ lsn, agentDayTotal, taskTotal, newCrossings }`; duplicate idempotency keys replay the original response. |
| POST | `/api/v1/cost/budgets` | bearer + operator capability | Body: budget JSON. Requires `Idempotency-Key: cmd_cost.budget.set_<ULID>`, `X-Operator-Identity`, and `X-Operator-Capability` when configured. The broker overwrites `setBy`/`setAt` server-side. 201 returns `{ lsn, tombstoned }`; `limitMicroUsd: 0` returns 400 because tombstones use DELETE. |
| DELETE | `/api/v1/cost/budgets/:id` | bearer + operator capability | Tombstones an existing budget. Requires `Idempotency-Key: cmd_cost.budget.tombstone_<ULID>`, `X-Operator-Identity`, and `X-Operator-Capability` when configured. 200 returns `{ lsn, tombstoned: true }`; 404 on malformed or missing id. |
| POST | `/api/v1/cost/idempotency/prune` | bearer + operator capability | Deletes `command_idempotency` rows older than `?olderThanMs=<positive-ms>`; defaults to 24h. Returns `{ pruned, olderThanMs, cutoffMs }`. Event-log and projection rows are never deleted. |
| GET | `/api/v1/cost/budgets` | bearer | Lists projected budgets as `{ budgets: [...] }`. |
| GET | `/api/v1/cost/budgets/:id` | bearer | Returns one projected budget, or 404 on malformed/missing id. |
| GET | `/api/v1/cost/summary` | bearer | Returns current cost projections: agent spend, budgets, and threshold crossings. |
| GET | `/api/v1/cost/replay-check` | bearer | Replays cost events and compares projections. 200 when `ok: true`; 500 with structured discrepancies when drift or unparseable cost payloads are found. |
| POST | `/api/runners` | bearer + runner agent map | Body: `RunnerSpawnRequest`. The bearer maps to one `agentId`; mismatches return 403. The broker mints/injects `BrokerIdentity`, resolves the `CredentialHandle`, and returns `{ runnerId }`. Mounted only when `createBroker({ runners })` is supplied. |
| GET | `/api/runners/:id/events` | bearer + runner agent map | SSE stream of `RunnerEvent` values. The caller's bearer-mapped `agentId` must match the runner owner. |
| GET | `/`, `/index.html` | none (loopback) | Renderer bundle (404 if `renderer: null`). |
| GET | `/assets/*` | none (loopback) | Static assets under the renderer dir. |
| WS | `/terminal/agents/:slug?token=` | token + loopback origin | Currently closes with `1011 not_implemented`; the agent stdio bridge replaces this in a later branch. |

Receipt thread-list pagination keeps the response body as a bare JSON array. Follow
the RFC 8288 `Link` header to continue:

```http
GET /api/threads/01ARZ3NDEKTSV4RRFFQ69G5FAZ/receipts?limit=2
Link: </api/threads/01ARZ3NDEKTSV4RRFFQ69G5FAZ/receipts?cursor=bHNuOjI&limit=2>; rel="next"

GET /api/threads/01ARZ3NDEKTSV4RRFFQ69G5FAZ/receipts?cursor=bHNuOjI&limit=2
```

## Invariants

1. **Bind is `127.0.0.1` only.** Never `0.0.0.0`, never a LAN IP.
2. **DNS-rebinding guard runs on every request.** Both `Host` (allowed loopback
   hostname) and `RemoteAddr` (loopback peer IP) must pass.
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

## Spec anchors

- `business-musings/wuphf-greenfield-rewrite-rfc-2026-05.md` §7.3 (IPC discipline) and §15 Stream A row "feat/broker-loopback-listener".
- `docs/architecture/broker-contract.md` (the v0 broker contract this branch carries forward to v1).
- `@wuphf/protocol#ApiBootstrap`, `isAllowedLoopbackHost`, `isLoopbackRemoteAddress`.

## Validation

```bash
bun run typecheck
bun run test
bun run check
bun run check:invariants
```
