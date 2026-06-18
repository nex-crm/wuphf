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
   through `src/wuphf-bridge.ts` (`callBroker` / `getOfficeMembers` / `getTasks`),
   which is read-only and limited to an allowlist of broker GET paths. The ONE
   office write is `createTask({ title, details })` — the host confirms with the
   human, then creates a normal office task. Wire it to a button (e.g. "Start a
   follow-up task"); never call it on load.
   - **Integration-backed apps.** `listIntegrations()` lists the user's connected
     tools and their READ actions; `callIntegration(platform, action, params)`
     runs one. A READ action (GET/LIST/SEARCH/FETCH) returns its result; a
     MUTATING action is never executed by the app — it comes back
     `{ status: "needs_approval", request_id }` and the host raises a human
     approval card. `getEmails()` is a thin wrapper over
     `callIntegration('gmail', 'GMAIL_FETCH_EMAILS', …)` showing the pattern.
   - **AI-powered apps.** `ai(prompt, input?, { json? })` runs a bounded one-shot
     LLM step over data you already fetched through the bridge (summarize/score/
     classify). It is read-only reasoning, not a network call. With `{ json: true }`
     you get a parsed object back. If no provider is configured you get
     `{ error: "ai_unavailable" }` — render a fallback.
3. **No secrets, no auth.** The app never holds a token; the host bridge uses the
   signed-in user's session. Do not invent API keys or login flows.
4. **Keep it one app.** Single SPA, no router needed for a focused tool. Don't add
   server code, databases, or build steps beyond Vite.
5. **Protected files — use, don't rewrite.** Do NOT reimplement or simplify
   `src/wuphf-bridge.ts`. Its helpers (`callBroker`, `getTasks`,
   `getOfficeMembers`, `createTask`, `callIntegration`, `listIntegrations`,
   `ai`, `getEmails`) are already correct — `getTasks()` returns
   ALL office tasks across every channel, not just the default one. Import and
   call them as-is. Rewriting them silently loses fixes: e.g. replacing
   `getTasks()` with a bare `callBroker("/tasks")` reads only the empty "general"
   channel and your app shows no data. Likewise don't touch `vite.config.ts`'s
   singlefile setup or the `wuphf-app`/`wuphf-host` message shape — they must
   match the WUPHF host.

## Style

- Clear hierarchy, legible defaults, real empty/loading/error states.
- Light surface; the app renders on white inside WUPHF.
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
