# Broker Loopback Contract

## Summary

The broker UI contract is a Go HTTP server bound to `127.0.0.1` that serves the
React bundle, a same-origin reverse proxy for HTTP and SSE API calls, and the
loopback WebSocket transport used by the agent terminal. The contract is the
same no matter which host starts it: today's `cmd/wuphf` binary used by
`npx wuphf`, and the future `cmd/wuphf-desktop` Wails shell, must both rely on
the broker listener rather than inventing a second app-data IPC surface.

## Listener

The canonical web UI listener lives in `internal/team/broker_web_proxy.go` as
`(*Broker).ServeWebUI(port int)`.

`ServeWebUI` owns the browser-visible listener. It builds a mux for static
assets, `/api/*`, `/onboarding/*`, `/api-token`, and public generated image
files, then binds exactly `127.0.0.1:<port>`. It does not bind `localhost`,
`0.0.0.0`, a LAN address, or a remote host. Callers choose the port before
calling it.

The default local port pair is:

| Surface | Default | Source |
|---|---:|---|
| Broker API listener | `7890` | `internal/brokeraddr/addr.go` (`DefaultPort`) |
| Web UI listener | `7891` | `cmd/wuphf/main.go` (`--web-port`) and `internal/workspaces/ports.go` (`MainWebPort`) |

`internal/brokeraddr/` is the source for resolving the broker API base URL and
token file. The web UI default is the adjacent port, not
`brokeraddr.DefaultPort`. Code that starts a desktop shell must keep that
distinction clear: `ServeWebUI(port int)` receives the web UI port, while
`brokeraddr.ResolveBaseURL()` and `b.Addr()` identify the broker API listener
behind the proxy.

At startup, `ServeWebUI` injects the two web origins into `b.webUIOrigins`:

```text
http://localhost:<web-port>
http://127.0.0.1:<web-port>
```

`internal/team/broker_middleware.go` uses that list in `corsMiddleware` when
the broker API listener needs to answer browser requests from the web UI
origin. Empty and `null` origins do not get wildcard CORS; local CLI callers
and same-origin requests do not need it.

The DNS-rebinding guard for the web UI listener is `webUIRebindGuard` in
`internal/team/broker_middleware.go`. It rejects requests unless both checks
pass:

- `RemoteAddr` resolves to loopback (`127.0.0.0/8`, `::1`, or `localhost`).
- `Host` is one of the accepted loopback hostnames (`localhost`,
  `127.0.0.1`, or `::1`, with any port).

The guard is applied to the same-origin proxy routes and to `/api-token`
because those handlers can ride or reveal the broker bearer token. The listener
being bound to `127.0.0.1` is necessary but not sufficient: a browser can be
tricked into sending an attacker-controlled `Host` value to loopback through
DNS rebinding, so the handler also validates `Host`.

## Resource Resolution Order

`ServeWebUI` resolves the React bundle in one order. Hosts should not add
another asset path unless this document and the code change together.

1. On-disk `web/dist/index.html`.

   `ServeWebUI` first looks next to the executable at
   `<exe-dir>/web/dist/index.html`. If `<exe-dir>/web` does not exist, source
   checkout runs fall back to repository-relative `web/dist/index.html`. This
   is the local development path after:

   ```bash
   cd web
   bun run build
   ```

2. Embedded filesystem from `wuphf.WebFS()`.

   `embed.go` embeds `all:web/dist` into the top-level `wuphf` package.
   `WebFS()` strips the `web/dist` prefix and returns `ok=false` when
   `index.html` is missing. Release binaries depend on this path so they can
   serve the UI without a separate filesystem bundle.

3. `missingWebAssetsHandler()`.

   If neither on-disk nor embedded assets contain `index.html`, the web UI
   listener returns an actionable setup page. It intentionally does not serve
   raw Vite source files, because browsers would load TypeScript sources as
   static text and leave the app stalled.

This order is part of the host contract. `cmd/wuphf` and `cmd/wuphf-desktop`
should both start the same broker web listener and let it decide which bundle
source is available.

## API Surface

All app data stays on loopback transports owned by the broker. Browser mode and
desktop mode share the same transport contracts so the React app does not learn
whether it is inside a normal browser tab or a Wails WebView.

The existing app-data transports are HTTP, SSE, and WebSocket. The desktop plan
must include all three; the agent terminal WebSocket is real app data, not an
OS integration detail.

| Transport | Browser/WebView entry point | Broker route | Owning files |
|---|---|---|---|
| HTTP | `/api/*` on the web UI listener | stripped path on broker API listener | `internal/team/broker_web_proxy.go`, `internal/team/broker.go`, `web/src/api/client.ts` |
| SSE | `/api/events` on the web UI listener | `/events` on broker API listener | `internal/team/broker_sse.go`, `web/src/hooks/useBrokerEvents.ts`, `web/src/api/*` subscribers |
| WebSocket | broker loopback URL from `/api-token` | `/terminal/agents/{slug}` | `internal/team/broker_terminal.go`, `web/src/lib/agentTerminalSocket.ts`, `web/src/api/client.ts (websocketURL)` |

HTTP requests use the same-origin proxy. `web/src/api/client.ts` keeps
`apiBase` as `/api` in proxy mode, so `get`, `post`, `put`, `patch`, and
`del` call the web UI origin. `webUIProxyHandler` strips the `/api` prefix,
forwards the request to `brokeraddr.ResolveBaseURL()` or, when available,
`b.Addr()`, and injects:

```text
Authorization: Bearer <broker-token>
```

server-side. For proxied HTTP calls the React code does not attach the bearer
header itself. The proxy also preserves open-ended SSE reads by switching to an
HTTP client with no timeout when the request advertises
`Accept: text/event-stream`.

SSE is the shared broker event stream. The browser-visible route is
`/api/events`, which the proxy forwards to the broker's `/events` handler.
`internal/team/broker.go` registers `/events` directly on the broker mux, and
`internal/team/broker_sse.go` authenticates inline before streaming.

The stream emits named events from the broker's fanout channels, including:

- `ready`
- `message`
- `action`
- `activity`
- `office_changed`
- `wiki:write`
- `notebook:write` for non-human actors
- `entity:brief_synthesized`
- `entity:fact_recorded`
- wiki section updates through `wikiSectionsEventName`
- `playbook:execution_recorded`
- `playbook:synthesized`
- `pam:action_started`
- `pam:action_done`
- `pam:action_failed`

The client-side pattern is `new EventSource(sseURL("/events"))`. In proxy mode
that resolves to `/api/events`. Do not add per-surface SSE endpoints such as
`/wiki/stream` or `/notebooks/stream` unless the broker actually serves them;
the current web tests assert that subscribers use the shared `/events` stream.

WebSocket is the terminal transport. `internal/team/broker.go` registers
`/terminal/agents/` with `b.requireAuth(b.handleAgentTerminal)`, and
`internal/team/broker_terminal.go` upgrades the request with
`gorilla/websocket`. The path shape is:

```text
/terminal/agents/{slug}?task={taskID}
```

`web/src/lib/agentTerminalSocket.ts` builds that path and calls
`websocketURL()` from `web/src/api/client.ts (websocketURL)`. In current proxy mode,
`websocketURL()` uses the `broker_url` returned by `/api-token`, changes
`http` to `ws`, and appends the token as a `token` query parameter. That means
the terminal socket is loopback app data on the broker API listener, not a
Wails event and not a web UI `/api/*` proxy route.

The WebSocket origin check in `terminalOriginAllowed` accepts absent origins
and loopback origins (`localhost` or loopback IPs). Authentication still flows
through `requireAuth`, and `requestAuthToken` accepts the token from either an
`Authorization` header or the `token` query parameter used by the WebSocket
client.

## Auth And Token Flow

The browser bootstrap lives in `web/src/api/client.ts` as `initApi()`. Its
first request is always:

```text
GET /api-token
```

That request is same-origin with the web UI listener. `ServeWebUI` handles it
directly, wraps it with `webUIRebindGuard`, and returns:

```json
{
  "token": "<broker bearer token>",
  "broker_url": "http://127.0.0.1:<broker-port>"
}
```

On success, `initApi()` stores the token, stores `broker_url` as `brokerDirect`,
and keeps `useProxy = true`. In that mode, ordinary HTTP and SSE calls use the
web UI proxy, while the terminal WebSocket uses `brokerDirect`.

If `/api-token` fails, `initApi()` falls back to direct broker mode and tries
`GET http://localhost:7890/web-token`. That fallback exists for older direct
browser flows and manual broker connection paths. It is not the desktop
bootstrap contract.

The desktop implication is strict: the Wails WebView must load the React bundle
from the broker web UI loopback origin, for example
`http://127.0.0.1:<web-port>/`, so `/api-token` is a same-origin request. A
`wails://` asset URL would make `/api-token` target the WebView asset scheme
instead of the broker web UI listener, forcing the app into fallback behavior
and bypassing the same bootstrap path that browser mode uses.

The token is sensitive. The HTTP/SSE proxy path injects it server-side, but the
React process still receives it from `/api-token` because the current
WebSocket and direct fallback paths need it. That is why `/api-token` is guarded
by loopback `RemoteAddr` and loopback `Host`, and why desktop should not create
a second token delivery mechanism through Wails bindings.

## Coexistence With Workspace Ports

Multiple local Wuphf instances coexist by using workspace-bound port pairs.
The allocator lives in `internal/workspaces/ports.go`.

The main workspace keeps the historical pair:

```text
broker: 7890
web:    7891
```

Additional workspaces use even broker ports from `7910` through `7998`, with
the web UI on the next port:

```text
broker: N
web:    N + 1
```

`AllocatePortPair(reg *Registry)` scans registered workspaces and returns the
first unclaimed pair. It trusts the registry rather than probing the OS with
`lsof`; real bind conflicts surface when the broker starts and can be repaired
through workspace doctor flows.

The desktop shell must respect the same allocator. It must not assume that
`7890` and `7891` are free, and it must not invent a separate desktop-only port
range. If a future `StartWeb(ctx, opts)` helper starts the broker for Wails, it
should consume the same workspace registry data and return the selected web URL
and broker URL to the shell.

`internal/brokeraddr/addr.go` also keeps token files isolated for non-default
broker ports. `ResolveTokenFile()` uses the default token file only for
`DefaultPort`; other ports get a port-suffixed token file. That behavior is
part of the coexistence contract because concurrent brokers cannot safely
share a bearer token file.

## What Is Not Part Of The Contract

Wails events are not an app-data transport. `runtime.EventsEmit`, generated
Wails bindings, or any other Wails RPC surface may be used for OS verbs only:
tray, notifications, dock badges, deep links, autostart, and updater status.
Messages, tasks, wiki state, requests, receipts, terminal bytes, and all other
product data stay on broker HTTP, SSE, or WebSocket.

`wails://` or another asset scheme is not the React bootstrap origin for the
desktop app. The WebView loads the broker web UI loopback URL so the same
`/api-token`, `/api/*`, `/api/events`, and terminal WebSocket behavior applies
in browser mode and desktop mode.

Non-loopback broker addresses are not part of this contract. The broker web UI
listener binds `127.0.0.1`, and the broker API listener resolves to loopback
for the local app path. Do not expose these listeners on `0.0.0.0`, LAN
interfaces, public hostnames, or cloud ingress as part of the desktop work.

Plain HTTP egress is not part of the contract. Plain `http://127.0.0.1:<port>`
is local IPC between the host, browser/WebView, and broker. Plain HTTP to a
remote service is a different security model and must not be introduced under
the broker loopback contract.

New endpoints are not implied by this document. In particular, the current app
does not have Wails RPC endpoints for app data, per-surface SSE streams, or a
web UI proxy endpoint for the terminal WebSocket. If a future change adds any
of those, the code, tests, and this contract must change in the same patch.
