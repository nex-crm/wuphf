# Web Path Forward

Date: 2026-05-04
Branch: `research/web-path-forward-20260504`
Base: `origin/main` at `735c3619`

## Goal

Improve the Web surface so new product areas can be added without fragile
navigation glue, wide central files, or hidden state coupling. The main
candidate is a real TanStack Router migration, but the broader goal is route
ownership, typed data contracts, and repeatable test evidence.

## Current Findings

- `@tanstack/react-router` is already a dependency, and
  `web/src/lib/router.ts` defines a small route tree, but it is not mounted.
  `web/src/main.tsx` renders `<App />` directly inside `QueryClientProvider`
  and never uses `RouterProvider`.
- Real navigation is `web/src/hooks/useHashRouter.ts`: manual hash parsing,
  manual hash serialization, and two-way sync with Zustand using ignore flags.
  That makes the URL and store competing sources of truth.
- Route state lives in `web/src/stores/app.ts` as `currentApp`,
  `currentChannel`, `wikiPath`, `wikiLookupQuery`, `notebookAgentSlug`, and
  `notebookEntrySlug`. The same store also owns legitimate ephemeral UI state
  like modals, sidebar state, unread counts, and thread panel state.
- `web/src/App.tsx` is the route switchboard. It imports every app panel,
  special-cases wiki/notebooks/reviews, and maps app IDs to components in one
  central block. Adding a product area means touching global navigation state,
  the central switch, sidebar constants, tests, and often route hash code.
- Hash URLs are still the right starting point. The Go Web UI server currently
  serves static files through `http.FileServer` at `/`; it does not provide a
  SPA fallback for arbitrary browser-history routes. Moving off hashes should
  be a later server change, not part of the first router migration.
- The data layer is partially on the right path. `web/src/api/workspaces.ts`
  has typed hooks and key factories; other domains still mix ad hoc query keys
  and large catch-all API helpers. The router work should reinforce domain
  slices instead of creating another cross-cutting layer.
- Existing E2E coverage has a useful `app-routes.spec.ts` guard for app route
  isolation. What is missing is a route contract test that proves every
  registered route can render, parse params/search, and navigate without going
  through the old Zustand bridge.

## Recommendation

Adopt TanStack Router, but do it as a source-of-truth migration instead of a
wrapper around the existing hash hook.

Use `createHashHistory()` first so the URL shape remains compatible with the
current static file server and external links. TanStack's docs explicitly
support hash history for servers that do not rewrite all paths to
`index.html`.

Make the URL own navigation state:

- Route params own channel, DM agent, app ID, wiki article path, notebook agent,
  notebook entry, and review route.
- Search params own query-like state such as wiki lookup `q`.
- Zustand keeps session UI state only: sidebar open/collapsed state, modal
  state, unread counters, active thread, active agent panel, theme, connection
  state, onboarding state, and transient composer state.

Start with code-based route modules rather than immediately adding file-based
route generation. TanStack's docs recommend file-based routes in general, but
this repo already has a code-based stub and no router plugin wiring. A first
code-based migration reduces build-tool churn and still gives typed params,
typed search, nested layouts, route errors, and route-local loaders. Revisit
file-based routes after the route ownership model has settled.

## Target Route Shape

Initial hash routes should preserve current URLs:

| Route | Owns |
|---|---|
| `/` | redirect/default to `/channels/general` |
| `/channels/$channelSlug` | channel message feed |
| `/dm/$agentSlug` | DM view, deriving broker direct channel slug |
| `/apps/$appId` | generic app panel route with validated app IDs |
| `/console` | legacy alias redirect to `/apps/console` |
| `/threads` | legacy alias redirect to `/apps/threads` |
| `/wiki` | wiki catalog/root |
| `/wiki/lookup?q=...` | wiki lookup answer route |
| `/wiki/$` | splat article path |
| `/notebooks` | notebook catalog |
| `/notebooks/$agentSlug` | agent notebook |
| `/notebooks/$agentSlug/$entrySlug` | notebook entry |
| `/reviews` | promotion review queue |

Shell layout should be a route layout, not an `App.tsx` switch. Global hosts
such as toast, confirm, provider switcher, Telegram connect, search, help, and
thread/agent panels can remain mounted at the root or shell layout.

## Migration Sequence

1. Route inventory and invariant tests
   - Introduce a `web/src/routes/` directory and route ID/app ID types.
   - Add tests that compile the route tree, validate app IDs, and preserve
     current hash URLs.
   - Keep `useHashRouter` active during this step.

2. Mount TanStack Router with a temporary adapter
   - Replace `<App />` entry with `<RouterProvider router={router} />`.
   - Use `createHashHistory()` and keep current visual output.
   - Route components may initially write into the existing store one-way so
     old components keep working. Do not keep store-to-hash sync.

3. Convert navigators to typed navigation
   - Replace `setCurrentApp`, `setCurrentChannel`, `setWikiPath`, and
     `setNotebookRoute` calls in sidebar/search/composer shortcuts with
     `<Link>` or `router.navigate`.
   - Add small navigation helpers only where route APIs would otherwise leak
     into non-route utilities.

4. Move route-owned state out of Zustand
   - Channel message components read `channelSlug` from route params.
   - DM components read `agentSlug` and derive the direct channel slug.
   - Wiki/notebook/review components read params/search directly.
   - Remove `currentApp`, `currentChannel`, `wikiPath`, `wikiLookupQuery`,
     `notebookAgentSlug`, and `notebookEntrySlug` from the store after the last
     consumer is converted.

5. Split route panels and add route boundaries
   - Move the large panel map out of `App.tsx` into route modules.
   - Add route-level pending/error boundaries so one panel can fail without
     blanking the whole office.
   - Lazy-load heavy panels such as Skills, Settings, Graph, Artifacts, and
     wiki/notebook surfaces.

6. Optional browser-history follow-up
   - Add an explicit Go SPA fallback that serves `index.html` for non-API,
     non-asset routes.
   - Only then consider `createBrowserHistory()`.

## Reliability Work Beyond Router

- Standardize query keys per domain. Use key factories like
  `workspaceKeys` for tasks, messages, office, wiki, notebook, reviews,
  skills, platform, and providers. SSE invalidation should target these
  factories instead of ad hoc string prefixes.
- Continue extracting API contracts by domain. The existing docs already track
  `WEB-ROUTER-001` as deferred until contract slices mature; the router PRs
  should not add new implicit response shapes.
- Create a shared Web test harness for `QueryClientProvider`, router provider,
  store reset, and common API mocks. Several tests currently hand-roll
  `QueryClient` wrappers.
- Add a route matrix E2E test after migration. It should visit every public
  route, assert no React errors, assert shell stability, and assert active
  sidebar state.
- Centralize event types for `/events`. Route-level data will be more reliable
  if SSE invalidation is typed by domain instead of matching anonymous event
  strings in one hook.
- Keep workspace switching as full-page navigation. The current security model
  intentionally avoids cross-broker tokens inside the SPA.

## Open Questions To Resolve Before Step 2

These are gaps in the current plan, surfaced during review:

1. **Replace the existing router stub, do not extend it.** `web/src/lib/router.ts`
   currently calls `createRouter({ routeTree })` with default (browser) history
   and a comment that incorrectly claims it works with Go's FileServer. The
   stub is missing the shell layout and the wiki/notebooks/dm/reviews routes.
   Step 2 must rewrite this file with `createHashHistory()` and the full route
   table from the Target Route Shape section, not build on top of the stub.

2. **The temporary store-write bridge in step 2 is debt with a deadline.** If
   route components write one-way into the existing store while old components
   keep reading it, that recreates the `ignoreNextHashChange` /
   `ignoreNextStoreSync` failure mode. The bridge must be removed by the end
   of step 4 in the same PR train; do not let it ship as a stable layer.

3. **DM hydration ownership.** `enterDM(agent, directChannelSlug(agent))` in
   the current hook mutates `channelMeta` so the rest of the UI can resolve
   the DM channel. Once `agentSlug` lives in the route, something else has to
   guarantee `channelMeta` is populated — either the DM route loader hydrates
   it from the API, or DM channel meta moves out of the store entirely. Decide
   before step 4 starts; otherwise that step stalls.

4. **Wiki lookup search params under hash history.** TanStack search params on
   hash history serialize as `#/wiki/lookup?q=foo`. The wiki lookup route's
   `validateSearch` should accept that shape directly. We do not need to
   support legacy URL formats — switch cleanly to the TanStack-native shape.

5. **E2E scope wording.** Step 2 mounts the router in product code, so its
   PR must run the full shell E2E matrix. The "first PR can skip full E2E"
   sentence in Evidence Per PR only applies to step 1.

6. **Step sizing.** Step 4 (draining `currentApp`, `currentChannel`,
   `wikiPath`, `wikiLookupQuery`, `notebookAgentSlug`, `notebookEntrySlug`
   from the store) touches every panel consumer and is the long pole of this
   migration. Plan it as multiple PRs, one domain at a time.

7. **Split the non-router reliability work into its own track.** Query-key
   standardization, the shared test harness, and centralized `/events` types
   are independent of the router migration and should not bloat its PR train.
   Land them before or after, not inside.

8. **No legacy URL aliases.** Drop the current `#/threads` →
   `apps/threads` mapping and the silent malformed-hash fallback to
   `channels/general`. Routes go straight to the new format; unknown routes
   should render an explicit not-found instead of redirecting silently.

## Things Not To Do

- Do not wrap the current `useHashRouter` in TanStack Router and call it done.
  That keeps the two-source-of-truth problem.
- Do not move to browser history before the Go Web UI server has a tested SPA
  fallback.
- Do not make TanStack Router loaders replace TanStack Query globally. Use
  route loaders for route prerequisites and preloading; keep server state in
  React Query where polling, invalidation, and mutations are already established.
- Do not combine this with a large visual redesign. Router migration should be
  behavior-preserving.

## Evidence Per PR

Focused route PRs should run:

- `bash scripts/test-web.sh web/src/path/to/focused.test.ts`
- `bash scripts/test-web.sh`
- from `web/`: `bunx tsc --noEmit`
- from `web/`: `bun run build`
- for route behavior: `web/e2e/run-local.sh shell`

The first PR can skip full E2E only if it does not mount the router in product
code. Any PR that changes real navigation should include the shell E2E route
matrix.

## Phase 0 Implemented

Phase 0 landed as behavior-preserving scaffolding:

- `web/src/routes/legacyHash.ts` extracts the current hash parser/serializer
  from `useHashRouter` into pure functions.
- `web/src/routes/legacyHash.test.ts` pins legacy route behavior for channels,
  DMs, app routes, wiki, wiki lookup, notebooks, reviews, malformed hashes,
  DM slug derivation, and serialization round trips.
- `web/src/routes/routeRegistry.ts` defines the planned route inventory,
  app-panel IDs, first-class sidebar surfaces, and route-owned state fields.
- `web/src/routes/routeRegistry.test.ts` proves sidebar apps are classified,
  route contracts stay unique, TanStack's unmounted route tree matches every
  planned route, wiki splat params are captured, and default hash history keeps
  static-file-server compatibility.
- `web/src/lib/router.ts` now exposes the fuller unmounted TanStack route tree
  plus `createAppRouter()` for tests and the future `RouterProvider` mount.
- `web/e2e/tests/telegram-connect.spec.ts` now asserts the current
  token-verify -> mode-selection -> group-pick/DM behavior instead of expecting
  verify to jump directly to the group picker.

Phase 0 also caught two useful migration details before runtime routing changed:

- Wiki splat routes must be nested behind a wiki index route, otherwise the
  splat route steals `/wiki`.
- TanStack hash history creates browser hrefs like `/#/channels/general`; that
  is compatible with the current static server, but tests should not expect a
  bare `#/...` href from Router links.
- The shell E2E suite had stale Telegram wizard expectations around the
  intermediate mode-selection step. Updating those tests keeps the migration
  evidence trustworthy instead of letting unrelated expected-flow drift block
  router work later.

## External Docs Checked

- TanStack Router route trees: `https://tanstack.com/router/latest/docs/routing/route-trees`
- TanStack Router history types: `https://tanstack.com/router/latest/docs/guide/history-types`
- TanStack Router search params: `https://tanstack.com/router/latest/docs/guide/search-params`
- TanStack Router data loading: `https://tanstack.com/router/latest/docs/guide/data-loading`
