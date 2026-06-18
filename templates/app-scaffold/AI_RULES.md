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
