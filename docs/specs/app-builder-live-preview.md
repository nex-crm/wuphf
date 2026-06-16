# App Builder — Live Dev-Server Preview (SOTA)

Status: **Phase 1 SHIPPED (PR #1099) + instant-new-build (pre-scaffold,
#1100) + structured build-activity feed (Phase 2, #1102) + version timeline /
non-destructive preview (Phase 4) all SHIPPED on the stacked branch.**
Architecture approved by the founder (2026-06-15): live dev-server-per-app
behind a broker proxy; the single-file build stays as the sealed "ship"
artifact.

## Shipped so far

- **Phase 1 (PR #1099)** — `appDevManager` + per-app broker `httputil.ReverseProxy`
  (agent-proof CSP injection, HMR WS tunnel, DNS-rebind guard), `CustomAppFrame`
  dev mode, `AppLivePreview`. Existing apps preview live with HMR.
- **Instant new-build (stacked PR)** — a new "Build app: X" task **pre-scaffolds**
  the app's editable source from the embedded `templates/app-scaffold` the moment
  the task is created (`customAppStore.Scaffold`, embedded via the `templates`
  package; `MutateTask` create hook `maybePrescaffoldAppForCreate`). The draft is
  recorded `status:"building"` (hidden from the sidebar, resolved by the build
  task's preview) and the task brief carries the pre-created `app_id` so the agent
  publishes onto the same app. `register_app` flips it to `ready` and bumps the
  version; `writeAppSourceLocked` now preserves `node_modules` so the running dev
  server survives a publish and hot-reloads the new source. **Live-verified:** a
  brand-new build lights up a running scaffold in ~2s (bun install warm 99ms →
  Vite ready 664ms), bridge populates office data cross-origin.

## Why

Today the App Builder runs a full `bun install && bun run build` to a single
self-contained `index.html` *before anything is shown*, so the preview sits on a
"Building…" placeholder for minutes — the worst weakness of the feature
(observed live: 0 apps registered across a whole test, preview never populated).

The dyad research (`/tmp/dyad-research-report.md`) found that dyad's preview is
instant for one reason: **it does not build for preview.** It runs a real Vite
**dev server per app**, reverse-proxies it (tunneling Vite's HMR WebSocket), and
loads that *live origin* in the iframe. Edits hot-reload in milliseconds.

We adopt the same model, adapted to our Go broker + headless-agent + sandbox:

- **Preview = live dev server** (`bun run dev`) behind a broker reverse-proxy.
  Boots in ~1–2s after deps install; reflects every agent edit via HMR with no
  rebuild. This is what the human watches.
- **Ship artifact = the single-file build** (`vite-plugin-singlefile`), produced
  on "seal/publish". This is what gets stored, versioned, listed under Apps, and
  rendered in the fully-sealed sandbox for non-build viewing. Unchanged.

The data model does **not** change: dyad lets the app talk to Supabase/Neon
directly (`allow-same-origin`, no CSP). We keep our whitelisted-postMessage
bridge as the only data path, and inject a dev-mode CSP that blocks all network
except the HMR WebSocket (see Security).

## Architecture

```
App Builder edits src/  ──HMR──▶  bun run dev (:Pdev)
                                       │
runtime-home/.wuphf/apps/<id>/src/     │  scrape "Local: http://localhost:Pdev"
                                       ▼
            broker:  appDevManager  ── httputil.ReverseProxy + WS upgrade ──▶ :Pdev
                                       │  inject dev-mode CSP header
                                       ▼
            GET /apps/<id>/dev/*  (proxy route, stable per app)
                                       │
web:  CustomAppFrame (dev mode)  iframe src=/api/apps/<id>/dev/  (NOT srcDoc)
                                  + live boot-log overlay while starting
```

## Backend (Go, `internal/team`)

### `appDevManager` (new: `custom_app_dev.go`)
A process manager mirroring dyad's `process_manager.ts` / `runningApps`.

- `runningAppServers map[string]*appDevServer` keyed by app id; own mutex.
- `appDevServer{ id, port, cmd *exec.Cmd, proxy *httputil.ReverseProxy, startedAt, lastUsed, bootLog *ringBuffer, ready bool, readyURL string }`.
- `EnsureDevServer(id) (*appDevServer, error)`:
  1. Warm fast-path: if a server for `id` is running and healthy, bump `lastUsed`
     and return it (zero rebuild — dyad `app_handlers.ts:642`).
  2. Else: `bun install` in `…/apps/<id>/src/` if `node_modules` absent (warm
     shared cache — see Perf), then spawn `bun run dev --port <free>` with cwd =
     the app's src dir. Stream stdout/stderr into `bootLog` (ring buffer, last N
     KB) AND classify readiness by scraping `/(https?:\/\/(?:localhost|127\.0\.0\.1):\d+)/`
     (dyad `app_runtime_service.ts:588`). Mark `ready=true` + `readyURL` on match.
  3. Build the `httputil.ReverseProxy` to `readyURL` with a `Director` that
     strips the `/apps/<id>/dev` prefix, and a `ModifyResponse` that injects the
     dev-mode CSP header. WebSocket upgrade must pass through (Vite HMR): detect
     `Connection: Upgrade` and hijack/bidi-copy, or use a proxy that supports it.
- `StopDevServer(id)`: kill the process group, close the proxy, delete the entry.
- Idle GC: a ticker stops servers idle > 10 min (dyad `process_manager.ts:247`).
- Shutdown: kill all on broker stop (no orphaned `bun` processes).

### Broker routes (`broker_apps.go`)
- `GET  /apps/{id}/dev/*` → `EnsureDevServer(id)` then `proxy.ServeHTTP`. The
  first request blocks until ready (or streams the boot screen — see FE). Gated
  by the existing app read auth.
- `GET  /apps/{id}/dev/status` → `{ ready, bootLog, error }` for the FE overlay
  to poll/stream while starting.
- `POST /apps/{id}/dev/stop` (app-builder/human) → `StopDevServer`.
- Reuse `appWriterAllowed` semantics for stop; reads follow app read auth.

### Build/seal path (unchanged surface)
`register_app` (the ship artifact) stays exactly as today: the agent runs
`bun run build` → single-file `index.html` → `POST /apps`. The dev server is
purely the preview; sealing is still the durable, versioned, shareable artifact.

## Frontend (`web/src`)

- `CustomAppFrame`: add a `mode: "dev" | "sealed"` prop.
  - `sealed` (today): `srcDoc={withAppCsp(html)}`, `sandbox="allow-scripts"`.
  - `dev`: `src="/api/apps/<id>/dev/"`, `sandbox="allow-scripts allow-same-origin"`
    (HMR + Vite client need same-origin to the proxy; the injected dev CSP is the
    real network boundary — see Security). The bridge (`postMessage`) still works.
- `AppBuildPreview` (the task pane): default to **dev mode while the task is
  active** so the human watches the live build; switch to the sealed artifact
  once the app is published/the task is done.
- **Live boot-log overlay** (port dyad `PreviewLoadingScreen.tsx`): while
  `dev/status.ready === false`, show the streaming install/boot log with the
  sticky latest line — NOT a frozen "Building…". On error, show the tail + a
  "Fix with App Builder" action (Phase 3).
- `CustomAppView` (standalone app screen): a "Live"/"Sealed" toggle; Live ensures
  the dev server, Sealed shows the published single-file.

## Security (the load-bearing part)

The sealed artifact is unchanged: opaque-origin `allow-scripts`-only iframe +
injected `connect-src 'none'` CSP + whitelisted postMessage bridge.

The **dev preview loosens the iframe to a localhost proxy origin**, so it must
re-establish the no-exfiltration guarantee at the proxy:

- The proxy injects, on every dev response, a **dev-mode CSP**:
  `default-src 'self'; connect-src 'self' ws://<proxy-host> wss://<proxy-host>;
  img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'`.
  This permits Vite's HMR WS to the proxy origin and blocks all other network —
  so a generated app still cannot `fetch()` the internet, even in dev.
- Data access stays through the parent bridge (postMessage), never the network.
- `allow-same-origin` is scoped to the proxy origin only (the dev server is
  served from the broker's own origin via the `/apps/<id>/dev` path, so the app
  cannot reach the parent app's cookies/storage — it's a different path on the
  same origin; evaluate whether to serve dev previews from a **distinct port**
  to get a true separate origin, which is stronger). **Decision needed at build
  time:** distinct-port origin (stronger isolation, more infra) vs same-origin
  path (simpler). Default: distinct ephemeral port per broker, documented.
- Trust model: the dev server runs the agent's own source on the user's machine —
  the same code we already `bun build`. The CSP keeps the *rendered app* unable
  to exfiltrate; the dev server itself is local-only (bind 127.0.0.1).
- Pin `event.origin` on the host bridge listener to the proxy origin in dev mode.

Run `security-reviewer` + an orthogonal triangulation pass on the proxy + the
dev CSP + the WS tunnel before marking ready (new wire/serving surface, per
repo rules).

## Perf

- Warm dep cache: a shared bun cache dir so `bun install` resolves from cache
  (the scaffold already commits `bun.lock`). Optionally a pre-warmed
  `node_modules` template copied on scaffold to skip install entirely on first
  run (dyad does NOT do this — it's our chance to beat dyad's first-run latency).
- Warm-reuse + idle-GC bound the number of live `bun` processes.

## Phasing (after #1096 rebase)

1. **Dev-server preview + proxy + live boot-log** — the headline win (fixes
   time-to-first-preview). Effort L.
2. **Structured tool-activity cards** — SHIPPED (PR #1102). The `HeadlessEvent`
   stream already exists; the App-Builder feed (`buildActivity.ts` +
   `AppBuildActivity`) merges tool_use/tool_result into one resolving row.
   getState equivalent: native `tool_result`s arrive name-less (referenced by
   call id), so the reducer resolves a running row when the next tool_use starts
   and at each turn boundary — only the active tool spins, no zombies.
3. **Pre-commit `tsc --noEmit` + `vite build` gate + bounded ~2-round auto-fix**
   re-prompt with `file:line:col` (dyad `chat_stream_handlers.ts` auto-fix loop).
   Auto-loop build errors (ground truth); human-gate runtime errors. Effort M.
4. **Version timeline + non-destructive preview** — SHIPPED. Every published
   build is already retained append-only under `apps/<id>/versions/v<N>/`
   (snapshot dirs, NOT git — the store is deliberately decoupled from the wiki
   git worker). This phase exposes that history: each snapshot now carries a
   `meta.json` (who/when), `GET /apps/{id}/versions` returns structured,
   current-flagged entries, and the new `GET /apps/{id}/versions/{n}` reads a
   past build's bytes WITHOUT changing the current version. The app view gained a
   **History** toggle → a timeline rail beside the preview; selecting an older
   build previews it read-only (a banner offers "Restore this version" /
   "Back to current"). Restore reuses the existing append-only `Rollback`
   (re-publishes those bytes as a new forward version), so a restore is itself
   reversible. Restore moved out of the overflow menu (which now holds only the
   destructive Delete).
5. **Select-to-edit** (`data-dyad-id="file:line:col"` JSX tagging + injected
   selector → source map) **+ iframe error capture** via postMessage — the magic
   moment; works under our sandbox. Effort S–M.

## Open questions for build time

- Distinct-port vs same-origin-path for the dev preview (security vs infra).
- WebSocket passthrough mechanics for the Go reverse proxy (HMR).
- Windows process-group kill + port allocation parity.
- Resource ceiling: cap concurrent live dev servers; queue/evict policy.
- Does the dev server need the bridge, or only the sealed artifact? (Keep the
  bridge in both so data-reading apps work live.)

## Reuse, not rebuild

Reverse proxy = Go stdlib `httputil.ReverseProxy`. Process mgmt = `os/exec` +
existing patterns. Sandbox/CSP/bridge = the rich-artifact + custom-app
primitives already in the tree. Version timeline = the wiki/app git worker.
Nothing new is invented that the stdlib or the existing kernel already provides.
