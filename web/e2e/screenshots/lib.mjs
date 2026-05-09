// Reusable helpers for PR-screenshot capture. Each spec under
// web/e2e/screenshots/<feature>.mjs imports from this file, registers any
// per-feature mocks, drives the UI, and writes PNGs to the directory the
// publish.sh wrapper passes via $WUPHF_SCREENSHOTS_OUT.
//
// Why a hand-rolled harness instead of `playwright test`:
//   1. We point at vite dev (`http://localhost:5273`) and intercept /api/*
//      with route mocks — no real wuphf broker needed. The existing
//      tests/ harness boots an actual wuphf binary; that's overkill for
//      "capture seven canned modal states".
//   2. The output is image artefacts, not pass/fail assertions. Plain
//      Node + Playwright API gives us a tighter loop than a test runner
//      whose retries / parallelism / reporters all get in the way.
//
// Coupling note: the helpers here assume the app's RootRoute boot path
// (initApi → /onboarding/state → setBrokerConnected → render Shell) and
// the zustand store layout in web/src/stores/app.ts. If those move, the
// `flipStore` defaults below need to follow.

import { chromium } from "@playwright/test";

export const DEFAULT_BASE = process.env.BASE_URL ?? "http://localhost:5273";
export const DEFAULT_HEALTH = {
  status: "ok",
  session_mode: "office",
  one_on_one_agent: "",
  focus_mode: false,
  provider: "anthropic",
  provider_model: "claude-sonnet-4-6",
  memory_backend: "local",
  memory_backend_active: "local",
  memory_backend_ready: true,
  nex_connected: false,
  build: { version: "0.83.5", build_timestamp: "2026-05-08T10:00:00Z" },
};

// launchBrowser opens a chromium instance + page with a captured-error
// channel. Pageerrors and console errors are forwarded to the returned
// `errors` array so a spec can assert on them after a flow.
export async function launchBrowser({ viewport, deviceScaleFactor = 2 } = {}) {
  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: viewport ?? { width: 1400, height: 900 },
    deviceScaleFactor,
  });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(`[pageerror] ${e.message}`));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(`[console.error] ${m.text()}`);
  });
  return { browser, context, page, errors };
}

// installCommonMocks installs the routes that every web boot needs to
// reach a Shell render with brokerConnected=true and onboardingComplete
// true. Pass per-feature overrides via `extra` to fulfill or modify
// specific endpoints. Calls context.unrouteAll first so successive
// installCommonMocks calls in one spec start clean.
export async function installCommonMocks(
  context,
  {
    health = DEFAULT_HEALTH,
    upgradeCheck = null,
    upgradeRun = null,
    brokerRestart = null,
    extra = null,
  } = {},
) {
  await context.unrouteAll().catch(() => {});

  // initApi() in api/client.ts hits these unprefixed token endpoints
  // before any /api/* call.
  await context.route("**/api-token", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ token: "stub", broker_url: null }),
    }),
  );
  await context.route("**/web-token", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ token: "stub" }),
    }),
  );

  // Onboarding gate — return onboarded so RootRoute renders Shell, not
  // the Wizard. Specs that want to capture the wizard should pass an
  // `extra` route that overrides this.
  await context.route("**/api/onboarding/state", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ onboarded: true }),
    }),
  );

  await context.route("**/api/health", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify(health),
    }),
  );
  await context.route("**/api/version", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify(health.build ?? { version: "stub", build_timestamp: "" }),
    }),
  );

  if (upgradeCheck != null) {
    await context.route("**/api/upgrade-check", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(upgradeCheck),
      }),
    );
  }
  if (upgradeRun != null) {
    await context.route("**/api/upgrade/run", async (r) => {
      if (typeof upgradeRun === "function") return upgradeRun(r);
      await r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(upgradeRun),
      });
    });
  }
  if (brokerRestart != null) {
    await context.route("**/api/broker/restart", async (r) => {
      if (typeof brokerRestart === "function") return brokerRestart(r);
      await r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(brokerRestart),
      });
    });
  }

  // Stubs the Shell expects so it renders without 502/error states.
  await context.route("**/api/humans/me", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ human: { slug: "you" } }),
    }),
  );
  await context.route("**/api/members*", (r) =>
    r.fulfill({ contentType: "application/json", body: "[]" }),
  );
  await context.route("**/api/channels", (r) =>
    r.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ channels: [] }),
    }),
  );

  // SSE: hold the request open without emitting. brokerConnected gets
  // set via flipStore after page load, which is faster and deterministic
  // compared to mocking the EventSource framing.
  await context.route("**/api/events*", async () => {
    await new Promise(() => {});
  });

  if (extra) await extra(context);
}

// flipStore writes to the app's zustand store directly so the Shell
// renders without us having to drive the real onboarding/SSE flow.
// Called AFTER the page has loaded and react has mounted; relies on the
// dev server exposing /src/stores/app.ts as an ESM module.
export async function flipStore(page, overrides = {}) {
  await page.evaluate(async (over) => {
    const m = await import("/src/stores/app.ts");
    m.useAppStore.setState({
      brokerConnected: true,
      onboardingComplete: true,
      ...over,
    });
  }, overrides);
}

// bootShell is the standard "load the page, wait for status bar, flip
// store, wait for whatever you specified" lifecycle. Most specs only
// need this and shotElement.
export async function bootShell(
  page,
  {
    base = DEFAULT_BASE,
    waitForSelector = ".status-bar",
    afterFlipSelector = null,
    storeOverrides = {},
    settleMs = 400,
  } = {},
) {
  await page.goto(`${base}/`, { waitUntil: "load" });
  await page.waitForSelector(waitForSelector, { timeout: 15_000 });
  await flipStore(page, storeOverrides);
  if (afterFlipSelector) {
    await page.waitForSelector(afterFlipSelector, { timeout: 10_000 });
  }
  if (settleMs > 0) await page.waitForTimeout(settleMs);
}

// shotElement captures a single locator into <outDir>/<name>.png. Names
// should sort lexically in capture order so the README/PR body picks
// them up in display order (`01-…`, `02-…`).
export async function shotElement(page, selector, outDir, name) {
  const loc = page.locator(selector);
  await loc.waitFor({ state: "visible", timeout: 10_000 });
  await page.waitForTimeout(150);
  await loc.screenshot({
    path: `${outDir}/${name}.png`,
    animations: "disabled",
  });
}

// shotPage captures the entire viewport. Use this when the "feature"
// fills the page (modal + dimmed backdrop, full-screen wizard step,
// etc.) and the surrounding chrome is part of the story.
export async function shotPage(page, outDir, name) {
  await page.waitForTimeout(150);
  await page.screenshot({
    path: `${outDir}/${name}.png`,
    animations: "disabled",
    fullPage: false,
  });
}
