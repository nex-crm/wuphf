// Capture the two product-analytics consent surfaces added with PostHog
// analytics + session replay:
//   1. The onboarding wizard's final step, with the two consent toggles.
//   2. Settings -> Privacy & Analytics, with the same two toggles.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh analytics-consent <pr-number>

import process from "node:process";

import {
  DEFAULT_BASE,
  flipStore,
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

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1280, height: 860 },
});

// ── 1. Onboarding wizard final step (consent toggles) ──────────────────────
// A `phase` in /onboarding/state makes RootRoute mount the visual wizard
// directly (bypassing PrePickScreen).
await installCommonMocks(context, {
  extra: async (ctx) => {
    await ctx.route("**/api/onboarding/state", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ onboarded: false, phase: "greet" }),
      }),
    );
    await ctx.route("**/api/onboarding/blueprints", (r) =>
      r.fulfill({ contentType: "application/json", body: "[]" }),
    );
    for (const p of [
      "**/api/onboarding/answer",
      "**/api/onboarding/progress",
      "**/api/onboarding/complete",
    ]) {
      await ctx.route(p, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ ok: true }),
        }),
      );
    }
  },
});

await page.goto(`${DEFAULT_BASE}/`, { waitUntil: "load" });
await page.waitForSelector('[data-testid="onboarding-wizard"]', {
  timeout: 15_000,
});

// Advance meet -> wiki -> team, skip the team step (scratch path), then
// ship -> first-issue, where the consent toggles live.
const primary = '[data-testid="onboarding-wizard-primary"]';
const teamSkip = '[data-testid="onboarding-wizard-team-skip"]';
await page.click(primary); // meet -> wiki
await page.click(primary); // wiki -> team
await page.waitForSelector(teamSkip, { timeout: 10_000 });
await page.click(teamSkip); // team -> ship
await page.click(primary); // ship -> first-issue
await page.waitForSelector('[data-testid="onboarding-step-first-issue"]', {
  timeout: 10_000,
});
await page.waitForSelector('[data-testid="onboarding-analytics-consent"]', {
  timeout: 10_000,
});
await page.waitForTimeout(400);
await shotPage(page, OUT, "01-onboarding-consent-toggles");
await shotElement(
  page,
  '[data-testid="onboarding-analytics-consent"]',
  OUT,
  "02-onboarding-consent-detail",
);

// ── 2. Settings -> Privacy & Analytics ─────────────────────────────────────
await installCommonMocks(context, {
  extra: async (ctx) => {
    await ctx.route("**/api/config", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          llm_provider: "claude-code",
          memory_backend: "markdown",
          analytics_telemetry_enabled: true,
          analytics_session_recording_enabled: true,
          analytics_configured: true,
        }),
      }),
    );
    await ctx.route("**/api/status/local-providers", (r) =>
      r.fulfill({ contentType: "application/json", body: "[]" }),
    );
    await ctx.route("**/api/office-members", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ members: [] }),
      }),
    );
  },
});

await page.goto(`${DEFAULT_BASE}/#/apps/settings`, { waitUntil: "load" });
await flipStore(page, { onboardingComplete: true });
await page.waitForSelector(".status-bar", { timeout: 15_000 });
await page.waitForSelector('[data-testid="settings-nav-privacy"]', {
  timeout: 10_000,
});
await page.click('[data-testid="settings-nav-privacy"]');
await page.waitForSelector('[data-testid="settings-telemetry-toggle"]', {
  timeout: 10_000,
});
await page.waitForTimeout(400);
await shotPage(page, OUT, "03-settings-privacy-analytics");

console.log(`captured screenshots to ${OUT}`);
await browser.close();
