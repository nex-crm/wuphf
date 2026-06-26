# WUPHF App scaffold

A Vite + React + TypeScript **refine** project that builds to a single
self-contained `index.html`. The built-in **App Builder** agent copies this
scaffold to build internal tools ("Apps") for an office.

The stack is fixed: **refine** (`@refinedev/core`) for the headless data layer,
**Mantine** (`@mantine/core`) for UI, and `@refinedev/react-table` for sortable /
filterable / paginated data grids. refine's `dataProvider` is implemented over the
WUPHF bridge (`src/bridgeDataProvider.ts`), so every read/write flows through
`postMessage` — no HTTP, no storage, no router. See `AI_RULES.md` for the full
contract.

## How the App Builder uses it

1. Copy this directory to a scratch workspace.
2. Read `AI_RULES.md` — it is the build contract (resources, `useTable`, the
   bridge data provider, the hard sandbox constraints).
3. Edit `src/App.tsx` (and add components) to implement the requested tool. Read
   workspace data through refine hooks (`useTable` / `useList` / `useOne`) backed
   by `bridgeDataProvider`, or the `src/wuphf-bridge.ts` helpers — never `fetch`.
4. `bun install && bun run build` → `dist/index.html` (one self-contained file,
   ~200 KB gzip with refine + Mantine inlined).
5. Call the `register_app` MCP tool with the absolute path to `dist/index.html`
   (plus the source path) to publish it under **Apps**.

If this scaffold is not present (e.g. an `npx wuphf` install with no repo
checkout), create the equivalent with `bun create vite@latest . --template
react-ts`, add `vite-plugin-singlefile`, set `base: "./"`, add `@refinedev/core`
+ `@mantine/core` + `@refinedev/react-table`, and copy `src/wuphf-bridge.ts` +
`src/bridgeDataProvider.ts` + `AI_RULES.md` from this template's documented
contract.

## Local preview

`bun run dev` serves it at the Vite dev URL. `src/App.tsx` is a live refine +
Mantine data grid over the office task list (sortable, filterable, paginated) —
a worked example of the bridge data provider. Replace it with the real tool.
