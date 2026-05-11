# `src/listener.ts` — broker loopback HTTP+SSE+WebSocket listener

The single entry point for the broker's network surface. Hosts call
`createBroker(config)` and get back a `BrokerHandle` exposing `url`, `port`,
`token`, and `stop()`.

## Bind discipline

`server.listen(port, "127.0.0.1")` is the only bind site in this package. The
host is hard-coded; widening it is forbidden by `AGENTS.md` rule 1 and by the
`check-invariants.sh` grep gate. Browsers, Electron WebViews, and Node clients
all connect over loopback only — there is no LAN, no `0.0.0.0`, no remote
ingress.

## Request pipeline

```mermaid
flowchart LR
  req["IncomingMessage"] --> guard["DNS-rebinding guard<br/>(Host + RemoteAddr)"]
  guard --> traversal["raw .. / NUL guard<br/>(decoded path)"]
  traversal --> auth["bearer gate<br/>(default-deny on /api/*)<br/>else 401"]
  auth --> dispatch{"pathname"}
  dispatch -- "/api-token" --> bootstrap["GET/HEAD only · bootstrap JSON"]
  dispatch -- "/api/health" --> health["GET/HEAD only · {ok:true}"]
  dispatch -- "/api/events" --> events["GET/HEAD only · SSE ready"]
  dispatch -- "POST /api/receipts" --> create["receiptFromJson → ReceiptStore.put<br/>201 / 400 / 409 / 413 / 415"]
  dispatch -- "GET /api/receipts/:id" --> read["ReceiptStore.get<br/>200 / 404"]
  dispatch -- "GET /api/threads/:tid/receipts" --> list["ReceiptStore.list({threadId})<br/>200 JSON array"]
  dispatch -- "unknown /api/*" --> apinotfound["404"]
  dispatch -- "/, /index.html, /assets/*" --> static["GET/HEAD only · RendererBundleSource"]
  dispatch -- "other" --> notfound["404"]
```

Every request goes through the DNS-rebinding guard first, then a raw-URL
traversal/NUL guard, then the default-deny bearer gate on the `/api`
namespace (`/api`, `/api/...` — but NOT `/api-token`, which is the
bootstrap). Method enforcement is per-route: each handler emits the
`Allow:` header for its own allowlist, so a `PUT /api/receipts` returns
`405 Allow: POST` and a `POST /api/health` returns `405 Allow: GET, HEAD`.
Authenticated requests to unknown `/api/*` paths return 404 before
falling into static dispatch.

## Wire-shape stability

`/api-token` returns the v0-compatible snake-case JSON `{ token, broker_url }`.
The broker emits this through `apiBootstrapToJson` from `@wuphf/protocol`; that
codec is the single source of truth for the wire shape and is round-trip
verified by both packages' tests.

## WebSocket upgrade

`/terminal/agents/:slug?token=<token>` accepts a WebSocket upgrade subject to
the same DNS-rebinding guard, plus an explicit origin check that allows
absent (Electron WebView / Node client) and loopback origins only. Branch-4
closes accepted upgrades immediately with `1011 not_implemented`; the agent
stdio bridge replaces this in a later branch.

## Static surface

When `RendererBundleSource` is supplied, `/`, `/index.html`, and `/assets/*`
are served from the configured directory with path-traversal protection.
Set `renderer: null` (the default) to disable static serving — useful for
the headless `wuphf serve` path and for dev mode where electron-vite owns
the renderer.

## Lifecycle

`stop()` is idempotent and per-handle: concurrent and follow-up calls share
one closure, so `wss.close` and `server.close` only run once. Active
WebSocket connections receive `1001 server_shutdown`; in-flight HTTP and SSE
streams close on `closeAllConnections()` (Node 18.2+).
