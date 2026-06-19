# AI_RULES — building a WUPHF App

These rules are the contract for any App Builder work on this project. Read them
before editing and keep them satisfied; the publish step (`register_app`) and the
sealed sandbox enforce most of them, so breaking one means a broken or rejected
app.

## Stack (do not swap)

WUPHF Apps are **refine apps**. The stack is fixed:

- **React 19 + TypeScript**, built with **Vite + `vite-plugin-singlefile`** into
  ONE self-contained `index.html`.
- **[refine](https://refine.dev) (`@refinedev/core`)** — the headless data layer.
  Every read/write goes through refine's data hooks, backed by a `dataProvider`
  that talks to the WUPHF bridge (never HTTP).
- **[Mantine](https://mantine.dev) (`@mantine/core` + `@mantine/hooks`)** — the UI
  kit. Use Mantine components (`Table`, `Badge`, `Button`, `TextInput`, `Select`,
  `Card`, `Group`, `Stack`, `Title`, `Text`, …) for everything visual. Do NOT add
  Tailwind, another UI kit, an icon font, or a second CSS framework.
- **`@refinedev/react-table` + `@tanstack/react-table`** — the data-grid hook
  (`useTable`) that gives sorting / filtering / pagination for free.

Why this stack and not a heavier one: refine + Mantine builds to a single
self-contained file (~200 KB gzip) that runs in the sealed iframe. Ant Design
(`@refinedev/antd`) does NOT fit — it bundles to ~445 KB gzip — so it is not
available here. Stay on Mantine.

## How the providers are wired (don't break this shape)

`src/main.tsx` mounts the providers. Keep this structure even if you rewrite the
file:

```tsx
import "@mantine/core/styles.css";              // inlined into the single file
import { MantineProvider } from "@mantine/core";
import { Refine } from "@refinedev/core";
import { bridgeDataProvider } from "./bridgeDataProvider";

<MantineProvider forceColorScheme="light">       // apps render on white
  <Refine
    dataProvider={bridgeDataProvider}            // routes data through the bridge
    resources={[{ name: "tasks" }, { name: "members" }, { name: "emails" }]}
    options={{ disableTelemetry: true, warnWhenUnsavedChanges: false }}
  >
    <App />
  </Refine>
</MantineProvider>
```

- **`forceColorScheme="light"`** is required — the sealed sandbox blocks
  `localStorage`, and apps render on a white surface. Don't remove it.
- **NO `routerProvider`** — the sandbox has no app-owned URL. A WUPHF App is one
  screen; you don't need a router. `useTable` defaults to `syncWithLocation:
  false`, so table state lives in React, not the URL.
- **NO `authProvider`** — the bridge runs as the signed-in user; the app holds no
  token. Don't invent login flows or API keys.
- **NO `notificationProvider` / `liveProvider` / `accessControlProvider`** — all
  optional; some touch storage or sockets the sandbox blocks. Use plain Mantine
  state for toasts.

## Declaring resources and reading data

A **resource** is a named dataset. `bridgeDataProvider` maps resource names to
bridge calls:

| Resource name        | Bridge call               | Notes                                  |
| -------------------- | ------------------------- | -------------------------------------- |
| `"tasks"`            | `getTasks()`              | ALL office tasks, every channel        |
| `"members"`          | `getOfficeMembers()`      | the office roster                      |
| `"emails"`           | `getEmails({limit:50})`   | read-only Gmail; empty if not connected|
| any integration name | `callIntegration(...)`    | pass `meta:{ platform, action, params }`|

Declare the resources you use in `<Refine resources={[…]}>`, then read them with
refine hooks. **Never call `fetch` and never re-implement the data layer.**

**Use refine, or don't mount it.** If your app reads/writes workspace data, route
it through refine hooks (`useTable` / `useList` / `useOne` / `useCreate`) — that is
why `<Refine>` + `bridgeDataProvider` are wired. Do NOT mount `<Refine>` and then
ignore it by calling the bridge directly — a wired-but-unused data layer is dead
weight and an AI tell. Conversely, if your app ONLY uses `ai()` + `createTask`
(e.g. a paste-input tool with no workspace resources), you may drop `<Refine>`
entirely and keep just `MantineProvider` — don't carry an unused provider.

### A sortable / filterable / paginated grid — `useTable`

This is the main pattern. `src/App.tsx` is a complete worked example over
`"tasks"`. Copy its shape:

```tsx
import { useTable } from "@refinedev/react-table";
import { type ColumnDef, flexRender } from "@tanstack/react-table";
import { Table, Badge } from "@mantine/core";

const columns: ColumnDef<MyRow>[] = [
  { id: "title", accessorKey: "title", header: "Title" },
  { id: "status", accessorKey: "status", header: "Status",
    cell: ({ getValue }) => <Badge>{String(getValue())}</Badge> },
];

function Grid() {
  const { reactTable, refineCore } = useTable<MyRow>({
    columns,
    refineCoreProps: { resource: "tasks", syncWithLocation: false,
                       pagination: { pageSize: 10 } },
  });
  // refineCore.tableQuery.{isLoading,error,data}  → loading / error / empty states
  // reactTable.getHeaderGroups() / getRowModel()  → render into <Table>
  // reactTable.previousPage() / nextPage() / getCanNextPage() → pagination
  // header.column.getToggleSortingHandler()       → click-to-sort
}
```

The `useTable` return is `{ reactTable, refineCore }`:

- **`reactTable`** is the TanStack Table instance — `getHeaderGroups()`,
  `getRowModel()`, `getState().pagination`, `nextPage()`, sorting handlers.
- **`refineCore`** is refine state — `tableQuery.isLoading`, `tableQuery.error`,
  `tableQuery.data?.total`, plus `setFilters` / `setSorters` for server-driven
  filtering.

### A simple list — `useList`

```tsx
import { useList } from "@refinedev/core";
const { data, isLoading } = useList({ resource: "members" });
// data?.data is the array; render with Mantine
```

### A single record — `useOne` / `useShow`

```tsx
import { useOne } from "@refinedev/core";
const { data } = useOne({ resource: "tasks", id: someId });
```

### Creating a task — `useForm` / `useCreate` (the ONE write)

The only workspace write is creating an office task. The host shows the human a
confirmation first, then creates it. Wire it to a button — never fire on load.

```tsx
import { useCreate } from "@refinedev/core";
const { mutate, isLoading } = useCreate();
// on click:
mutate({ resource: "tasks", values: { title, details } });
```

`create` on any resource other than `"tasks"`, and any `update` / `delete`, throw
loudly — apps are read-mostly by design. Don't try to work around this.

## Integrations and AI

- **Integration-backed apps.** `listIntegrations()` lists the user's connected
  tools and their READ actions. To read an integration as a refine resource, pass
  `meta`: `useList({ resource: "slack-msgs", meta: { platform: "slack", action:
  "SLACK_FETCH_MESSAGES", params: {…} } })`. A READ action returns its result; a
  MUTATING action is never executed by the app — the broker raises a human
  approval card and the list comes back empty here. You can also call
  `callIntegration(platform, action, params)` directly from `wuphf-bridge.ts`.
- **AI-powered apps.** `ai(prompt, input?, { json? })` runs a bounded one-shot LLM
  step over data you already fetched through the bridge (summarize / score /
  classify). It is read-only reasoning, not a network call. With `{ json: true }`
  you get a parsed object. If no provider is configured you get
  `{ error: "ai_unavailable" }` — render a fallback.

## Hard rules

1. **Self-contained output.** `bun run build` must emit ONE `dist/index.html`
   with all JS and CSS inlined (Mantine's CSS included). No external scripts,
   stylesheets, fonts, or images. No `@import`. Images must be inline
   `data:`/`blob:` URLs.
2. **No direct network.** The app runs in a sealed sandbox: opaque origin (so NO
   `localStorage` / `sessionStorage` / cookies) and CSP `connect-src 'none'` (so
   NO `fetch` / `XMLHttpRequest` / `WebSocket`). Reach ALL data through refine's
   hooks (backed by `bridgeDataProvider`) or the `wuphf-bridge.ts` helpers
   directly. Never persist to storage; keep state in React.
3. **No secrets, no auth.** The app never holds a token; the bridge uses the
   signed-in user's session. No `authProvider`, no API keys, no login flows.
4. **One screen, no router.** A WUPHF App is a single focused tool. Don't add a
   `routerProvider`, server code, a database, or build steps beyond Vite.
5. **Protected files — use, don't rewrite.**
   - `src/wuphf-bridge.ts` — the only channel out of the sandbox. Its helpers
     (`callBroker`, `getTasks`, `getOfficeMembers`, `createTask`,
     `callIntegration`, `listIntegrations`, `ai`, `getEmails`) are already
     correct (e.g. `getTasks()` returns ALL channels, not just "general"). Import
     and call them as-is.
   - `src/bridgeDataProvider.ts` — refine's `DataProvider` over the bridge. Import
     `bridgeDataProvider`; do NOT reimplement it. Add a resource by extending its
     `readers` map or passing `meta:{platform,action}` for an integration.
   - `vite.config.ts` singlefile setup and the `wuphf-app` / `wuphf-host` message
     shape must match the WUPHF host — don't touch them.

   You MAY freely rewrite `src/App.tsx` (your tool) and `src/main.tsx` (provider
   wiring) — but keep the provider shape above.
6. **Request only what you need.** Fetch lean payloads. `getEmails()` already
   asks the broker for metadata + snippet only (`verbose:false`,
   `include_payload:false`) — it returns kilobytes, not megabytes — so use its
   `snippet`, and do NOT re-fetch full message bodies with a heavier action. For
   `callIntegration()`, pass params that bound the result (a `limit`, a query, a
   lean/metadata flag). Don't rely on the platform to trim your data: the broker
   passes the integration result through and ERRORS on an oversized read rather
   than truncating it, so an over-fetch comes back as a failure, not shrunk data.
7. **Handle failures gracefully.** Every bridge call — `getEmails()`,
   `callIntegration()`, `ai()` — can fail or return an error/empty result. Wrap
   each in `try`/`catch` (or `.catch`) AND check the typed outcome before using
   it: `getEmails()` returns `{ connected, emails, error? }` (never throws on a
   normal error); `callIntegration()` returns `{ connected, status, result?,
   error? }`; `ai()` returns `{ text?, object?, error? }` (e.g.
   `"ai_unavailable"`). Render a real connect-state / error-state / empty-state.
   NEVER run an unguarded `JSON.parse` on a bridge reply and never let a bad
   response crash the app into a white screen. If you make MORE THAN ONE `ai()`
   call (a multi-step pipeline), guard EVERY call — check `ai_unavailable` / a
   generic `error` / a malformed `object` after each one, not just the first.
8. **Don't refetch on every focus — this is ENFORCED.** Load once on mount;
   refresh only on an explicit user action (a Refresh button) or a deliberate
   schedule (a timer with a COMPUTED delay, e.g. a daily 9am refresh). Do NOT
   re-run work — a Gmail fetch, an `ai()` summary, any pipeline — from a
   `visibilitychange` or window-`focus`/`blur`/`pageshow` listener, and do NOT
   `setInterval` faster than 30s. The human switches browser tabs constantly, so a
   focus-triggered refresh reloads the whole app every time they come back and a
   tight poll hammers integration rate limits and LLM tokens. **The publish step
   rejects these patterns** (`register_app` returns a `file:line` violation list
   from the efficiency harness) — fix them and republish. refine's data layer
   already sets `refetchOnWindowFocus: false`; match that in any hand-rolled
   fetch/effect. If the HUMAN explicitly asked for focus-triggered refresh or a
   fast live cadence, record that on the offending line with
   `// wuphf-allow: focus-refresh — <what the human asked for>` (or
   `// wuphf-allow: poll — …`) — the only way to ship it. Separately, the broker
   meters `ai()` and integration reads PER-APP (per-minute + per-day), so an app
   that slips through still cannot burn the workspace's budget.

## Design

These rules keep the app correct. **`DESIGN.md` keeps it from looking AI-generated
— read it, hold its bar, and run its pre-ship checklist before `register_app`.** A
WUPHF App is a fixed-kit, single-screen Mantine tool on white; the design bar is
"make THAT feel crafted," not "add things the sandbox forbids." **ENFORCED — the
publish step rejects an app that:** (a) does not render inside `MantineProvider` /
import `@mantine/core` (build on the kit, don't hand-roll a CSS system); (b) ships
**default-themed Mantine** — a missing or no-op `createTheme` (override it: ≥2 of
primaryColor / defaultRadius / spacing / headings / fontFamily); or (c) renders a
**list as a pile of `<Card>`s** (`.map(...)` producing `<Card>` — use a `<Table>`
or rows instead). In short:

1. **Set a real theme.** Override Mantine once in `main.tsx` via `createTheme` —
   `primaryColor`, one `defaultRadius`, a heading scale, a tightened `spacing`
   scale. Never ship default-themed Mantine; it is the #1 AI tell. (Keep
   `forceColorScheme="light"` and the provider shape above — only `theme` is yours.)
2. **Hierarchy is type, not boxes.** One real `<Title>`, then `fw`/`size`/`c="dimmed"`
   for rank — three tiers, not five font sizes. Monospace numbers and IDs.
3. **Earn the card.** A pile of identical `<Card>`s is a tell — use a `<Table>` or
   divider-separated `<Stack>` rows for lists; cards only for standalone objects.
4. **One accent, with meaning.** Restrained neutral surface + one accent ≤10% of
   pixels; status colors mean status (`variant="light"` badges). No gradients,
   glassmorphism, colored card backgrounds, or decorative motion.
5. **Compose with intent.** Use the theme spacing scale (not raw px), vary rhythm,
   and use an asymmetric `Group`/`Grid` split when content has a primary + secondary
   region instead of a reflexive centered column.
6. **Real copy, real states.** Name the actual thing (no "Dashboard"/"Welcome"
   placeholders); designed empty / loading / error / not-connected states, every one.

## Style

- Use Mantine components and props (`c="dimmed"`, `fw`, `size`, `variant`) for
  hierarchy; reach into `src/styles.css` only for page-level layout.
- Real empty / loading / error / not-connected states — the worked example shows
  all four. An integration app must render a connect-state when `connected` is
  false, not an error.
- camelCase variables, PascalCase components, `is/has/should` booleans.

## Build & publish

Build errors are ground truth. **Run the verify gate before you publish** and do
NOT call `register_app` until it passes clean. If it fails, read the reported
`file:line:col` errors, fix them, and run the gate again — up to ~2 rounds. If it
still fails, report the blocker instead of publishing a broken app.

```bash
bun install
bun run verify         # GATE: tsc --noEmit && vite build — must pass before publish
bun run build          # produces dist/index.html (single file)
# then call register_app with:
#   html_path   = ABSOLUTE path to dist/index.html  (broker reads the bundle)
#   source_path = ABSOLUTE path to this project root (broker copies the whole
#                 tree minus node_modules/dist, so the saved source always builds)
# Do NOT paste the minified bundle and do NOT hand-list files — both drop data.
```

## Live-preview tooling (do not remove)

`src/wuphf-inspector.ts` and the `data-wuphf-source` stamping in `vite.config.ts`
power the live preview's **select to edit** and runtime-error surfacing. They are
dev-only — `vite.config.ts` injects the inspector and the production single-file
build strips all of it — so leave both in place. You may freely rewrite
`src/main.tsx`; the inspector loads via `index.html`, not the entry file.
