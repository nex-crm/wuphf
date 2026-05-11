# @wuphf/broker

WUPHF v1 broker — loopback HTTP + SSE + WebSocket listener with a DNS-rebinding
guard, bearer-token auth, and the `/api-token` bootstrap. Pure Node; no Electron.

This is the **branch-4 + branch-5 slice** of the rewrite. It owns the boundary
between the renderer (Electron WebView or a regular browser tab) and any later
broker functionality (event log, projections, agent runners, AI gateway).
Everything above this layer is **app data** that travels HTTP/SSE/WebSocket
only — `contextBridge` carries OS verbs, never broker state.

Branch 5 adds the first real `/api/*` mutations: the receipt write path. A
receipt is a tamper-evident record of an agent run (see `@wuphf/protocol`'s
`ReceiptSnapshot`); the broker exposes it via REST so hosts can persist runs
behind a loopback boundary. The current storage is an in-memory map per
`createBroker()` call. Branch 6 (`feat/event-log-projections`) will swap a
durable event-log-backed `ReceiptStore` in behind the same interface.

## API

```ts
import { createBroker, InMemoryReceiptStore } from "@wuphf/broker";

const broker = await createBroker({
  port: 0, // ephemeral; broker.port reports the assigned value
  renderer: { dir: "/abs/path/to/renderer/dist" }, // or null to disable static
  // Optional. Defaults to a fresh InMemoryReceiptStore per createBroker call.
  // Branch 6 hosts will supply an event-log-backed implementation here.
  receiptStore: new InMemoryReceiptStore(),
});

// broker.url   = "http://127.0.0.1:<port>"
// broker.port  = BrokerPort
// broker.token = ApiToken
await broker.stop();
```

## Routes

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/api-token` | none (loopback only) | Returns `{ token, broker_url }` (snake_case wire). |
| GET | `/api/health` | bearer | Returns `{ "ok": true }`. |
| GET | `/api/events` | bearer | SSE stream; emits `ready` then keepalive comments. |
| POST | `/api/receipts` | bearer | Body: receipt JSON. 201 + canonical body on insert, 409 on `id` collision, 400 on parse/validation, 413 on `> 1 MiB`, 415 on non-JSON content-type. |
| GET | `/api/receipts/:id` | bearer | 200 + receipt JSON on hit, 404 on miss or malformed id. |
| GET | `/api/threads/:tid/receipts` | bearer | JSON array of V2 receipts in the thread, 404 on malformed thread id. |
| GET | `/`, `/index.html` | none (loopback) | Renderer bundle (404 if `renderer: null`). |
| GET | `/assets/*` | none (loopback) | Static assets under the renderer dir. |
| WS | `/terminal/agents/:slug?token=` | token + loopback origin | Branch-4 closes with `1011 not_implemented`. |

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
   returns 409 and the stored value is **not** overwritten. Idempotency keys
   (same id + byte-identical payload → 200 no-op) land in branch 6 alongside
   the durable event log.
7. **Receipts go through the protocol codec.** `receiptFromJson` runs the full
   validator (boundary budget, frozen-args canonicalization, shape, branded
   ids) before the broker even touches the store. There is no fast-path that
   skips validation.

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
