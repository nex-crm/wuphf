# AI_RULES — building a WUPHF App

These rules are the contract for any App Builder work on this project. Read them
before editing and keep them satisfied; the publish step (`register_app`) and the
sandbox enforce most of them, so breaking one means a broken or rejected app.

## Stack (do not swap)

- React 19 + TypeScript, built with Vite + `vite-plugin-singlefile`.
- Plain CSS in `src/styles.css` using the system font stack. No Tailwind, no UI
  kit, no CSS framework, no icon font — keep the bundle small and self-contained.

## Hard rules

1. **Self-contained output.** `bun run build` must emit ONE `dist/index.html`
   with all JS and CSS inlined. No external scripts, stylesheets, fonts, or
   images. No `@import`. Images must be inline `data:`/`blob:` URLs.
2. **No direct network.** The app runs in a sandbox with `connect-src 'none'`.
   NEVER use `fetch`, `XMLHttpRequest`, or `WebSocket`. Read workspace data ONLY
   through `src/wuphf-bridge.ts` (`callBroker` / `getOfficeMembers` / `getTasks`).
   The bridge is read-only and limited to an allowlist of broker GET paths.
3. **No secrets, no auth.** The app never holds a token; the host bridge uses the
   signed-in user's session. Do not invent API keys or login flows.
4. **Keep it one app.** Single SPA, no router needed for a focused tool. Don't add
   server code, databases, or build steps beyond Vite.
5. **Protected files.** Do not change the bridge wire contract in
   `src/wuphf-bridge.ts` (the `wuphf-app` / `wuphf-host` message shape) or
   `vite.config.ts`'s singlefile setup — they must match the WUPHF host.

## Style

- Clear hierarchy, legible defaults, real empty/loading/error states.
- Light surface; the app renders on white inside WUPHF.
- camelCase variables, PascalCase components, `is/has/should` booleans.

## Build & publish

```bash
bun install
bun run build          # produces dist/index.html (single file)
# then call the register_app MCP tool with the contents of dist/index.html
```

## Live-preview tooling (do not remove)

`src/wuphf-inspector.ts` and the `data-wuphf-source` stamping in `vite.config.ts`
power the live preview's **select to edit** and runtime-error surfacing. They are
dev-only — `vite.config.ts` injects the inspector and the production single-file
build strips all of it — so leave both in place. You may freely rewrite
`src/main.tsx`; the inspector loads via `index.html`, not the entry file.
