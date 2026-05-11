# @wuphf/broker

WUPHF v1 broker — loopback HTTP + SSE + WebSocket listener with a DNS-rebinding
guard, bearer-token auth, and the `/api-token` bootstrap. Pure Node; no Electron.

This is the **branch-4 slice** of the rewrite. It owns the boundary between the
renderer (Electron WebView or a regular browser tab) and any later broker
functionality (event log, projections, agent runners, AI gateway). Everything
above this layer is **app data** that travels HTTP/SSE/WebSocket only —
`contextBridge` carries OS verbs, never broker state.

## API

```ts
import { createBroker } from "@wuphf/broker";

const broker = await createBroker({
  port: 0, // ephemeral; broker.port reports the assigned value
  renderer: { dir: "/abs/path/to/renderer/dist" }, // or null to disable static
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
