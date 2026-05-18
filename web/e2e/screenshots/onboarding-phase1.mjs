// Capture the Phase 1 pre-office provider picker in two states:
// all runtimes detected, and no runtimes detected (the evaluator path).
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh onboarding-phase1 <pr-number>

import process from "node:process";

import {
  DEFAULT_BASE,
  installCommonMocks,
  launchBrowser,
  shotPage,
} from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const PREREQS_ALL_DETECTED = [
  { name: "claude", required: false, found: true, version: "v1.2.3" },
  { name: "codex", required: false, found: true, version: "v0.8.1" },
  { name: "opencode", required: false, found: true, version: "v0.4.0" },
];

const PREREQS_NONE = [
  { name: "claude", required: false, found: false },
  { name: "codex", required: false, found: false },
  { name: "opencode", required: false, found: false },
];

async function installPrePickMocks(context, prereqs) {
  await installCommonMocks(context, {
    extra: async (ctx) => {
      // Override the default onboarded:true stub so RootRoute renders
      // the pre-office picker instead of the Shell.
      await ctx.route("**/api/onboarding/state", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ onboarded: false }),
        }),
      );
      await ctx.route("**/api/onboarding/prereqs", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify(prereqs),
        }),
      );
    },
  });
}

async function bootPrePick(page) {
  // Phase 1 ships behind a localStorage flag (lib/featureFlags.ts).
  // Seed the flag BEFORE the first React render so RootRoute picks up
  // the V2 surface on its initial mount.
  await page.addInitScript(() => {
    try {
      window.localStorage.setItem("wuphf:onboarding-v2", "1");
    } catch {
      // private-mode tabs can throw; the picker falls back to the
      // ?onboardingV2=1 query param in that case.
    }
  });
  // Set up the prereqs-response wait BEFORE navigation so a fast fetch
  // doesn't race the listener. Deterministic replacement for the old
  // fixed-400ms sleep (PR #889 CodeRabbit feedback).
  const prereqsResponse = page.waitForResponse(
    (res) =>
      res.url().includes("/api/onboarding/prereqs") &&
      res.request().method() === "GET" &&
      res.ok(),
    { timeout: 15_000 },
  );
  await page.goto(`${DEFAULT_BASE}/?onboardingV2=1`, { waitUntil: "load" });
  await prereqsResponse;
  await page.waitForSelector('[data-testid="pre-pick-screen"]', {
    timeout: 15_000,
  });
  // Wait for the resolved status labels to land (either "Detected" with
  // a version, or "Not installed"). The card subtree starts in
  // "Checking…" and re-renders once prereqs resolve; capturing during
  // that transient leaks an empty-state into screenshots.
  await page.waitForFunction(
    () => {
      const txt = document.body?.textContent ?? "";
      return /Detected|Not installed/.test(txt);
    },
    null,
    { timeout: 5_000 },
  );
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1200, height: 800 },
});

await installPrePickMocks(context, PREREQS_ALL_DETECTED);
await bootPrePick(page);
await shotPage(page, OUT, "01-pre-pick-all-detected");

await installPrePickMocks(context, PREREQS_NONE);
await bootPrePick(page);
await shotPage(page, OUT, "02-pre-pick-none-detected");

console.log(`captured 2 screenshots to ${OUT}`);
await browser.close();
