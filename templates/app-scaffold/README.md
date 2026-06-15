# WUPHF App scaffold

A minimal Vite + React + TypeScript project that builds to a single
self-contained `index.html`. The built-in **App Builder** agent copies this
scaffold to build internal tools ("Apps") for an office.

## How the App Builder uses it

1. Copy this directory to a scratch workspace.
2. Read `AI_RULES.md` — it is the build contract.
3. Edit `src/App.tsx` (and add components/styles) to implement the requested tool.
   Read workspace data through `src/wuphf-bridge.ts`, never `fetch`.
4. `bun install && bun run build` → `dist/index.html` (one self-contained file).
5. Call the `register_app` MCP tool with the contents of `dist/index.html` to
   publish it under **Apps**.

If this scaffold is not present (e.g. an `npx wuphf` install with no repo
checkout), create the equivalent with `bun create vite@latest . --template
react-ts`, add `vite-plugin-singlefile`, set `base: "./"`, and copy
`src/wuphf-bridge.ts` + `AI_RULES.md` from this template's documented contract.

## Local preview

`bun run dev` serves it at the Vite dev URL. The office roster panel is a live
demo of the bridge; replace it with the real tool.
