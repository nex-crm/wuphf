# web/e2e/screenshots

Capture web UI states for a PR and embed them in the PR description. Runs
against `vite dev` with mocked `/api/*` so the broker doesn't need to be
running and the captured states are reproducible.

This is **separate from** `web/e2e/tests/` — that suite runs Playwright
specs against a real `wuphf` binary for smoke testing. The harness here
is image-only and uses route mocks; specs here would be a poor fit for
the test suite (no assertions, output is binary).

## Usage

```bash
# 1. Write a spec at web/e2e/screenshots/<feature>.mjs.
#    Copy version-chip.mjs and adjust the mocks + screenshot calls.

# 2. Run the wrapper.
web/e2e/screenshots/publish.sh <feature> <pr-number>

# 3. The wrapper:
#    - boots vite if not already running on $BASE_URL
#    - runs the spec, writing PNGs to /tmp/wuphf-screenshots-<feature>/
#    - pushes them to an orphan `screenshots/pr-<n>` branch
#    - appends the markdown block (with raw URLs) to the PR description
```

### Modes

```bash
# Capture + print markdown only. Useful when iterating on a spec
# without polluting the PR or pushing a branch.
publish.sh --dry-run <feature> <pr-number>

# Post screenshots as a NEW PR comment (don't touch the body). Use
# this when the PR already has a hand-written description you don't
# want to replace.
publish.sh --comment <feature> <pr-number>
```

## Writing a spec

A spec is a plain `.mjs` file that imports helpers from `./lib.mjs`,
sets up per-state mocks, drives the page, and writes PNGs to
`$WUPHF_SCREENSHOTS_OUT` (the wrapper sets this).

Minimal shape:

```js
import process from "node:process";
import {
  bootShell,
  installCommonMocks,
  launchBrowser,
  shotElement,
  shotPage,
} from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const { browser, context, page } = await launchBrowser();

await installCommonMocks(context, {
  // Per-state mocks: pass `upgradeCheck`, `upgradeRun`, `brokerRestart`,
  // or override `health`. Use `extra` for endpoints not covered by the
  // common-mocks list.
  health: { /* … */ },
});
await bootShell(page);

await shotElement(page, ".my-feature", OUT, "01-default-state");

await browser.close();
```

See `version-chip.mjs` for a worked example covering 7 states (chip
tooltip variants + 5 modal variants).

### File-naming convention

PNGs sort lexically into the PR body in capture order, so prefix each
filename with a two-digit number: `01-…`, `02-…`. The publish script
uses the basename (minus `.png`) as the alt text.

### Matching the running app's state

`bootShell` flips the zustand store directly via dynamic import of
`/src/stores/app.ts` so the Shell renders without us mocking the SSE
stream. The defaults are `brokerConnected: true, onboardingComplete:
true`. Pass `storeOverrides` to set additional store fields (e.g. an
active route or theme).

If a spec needs to capture an *unbooted* state (the wizard, splash, or
disconnect banner), don't call `bootShell`; navigate manually and
either skip `flipStore` or pass `{ onboardingComplete: false }` etc.

## When to use this

By default, every PR that changes anything under `web/` should ship
with screenshots — that's the rule in CLAUDE.md. The harness is the
shortest path to satisfy it. Skip when:

- The change is a refactor with no visible UI delta.
- The diff is purely test, doc, or build config.
- The same feature already has screenshots in a sibling PR you can
  link to instead.

## Why an orphan branch instead of GitHub user-attachments

GitHub's drag-drop image upload (the one the web UI uses) hits a
session-only endpoint on the `user-attachments` subdomain. There is no
public REST/GraphQL equivalent, so we can't programmatically post the
inline images the web UI produces. An orphan `screenshots/pr-<n>`
branch is the simplest public-API alternative — cheap to push, easy to
delete after merge, and the raw URLs render in PR markdown the same
way `user-attachments` URLs do.
