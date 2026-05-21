// Capture the OnboardingChat (full-screen wizard) header label per phase
// for issue #939.
//
// Before this fix, the header read:
//   STEP 1 OF 5 · OFFICE NAME
//   STEP 2 OF 5 · WHO YOU ARE
//   PHASE: WEBSITE        (no counter — fell through to fallback)
//   PHASE: SCAN           (no counter — fell through to fallback)
//   STEP 3 OF 5 · PICK A STARTING BLUEPRINT
//   STEP 5 OF 5 · FIRST TASK   (step 4 silently skipped on scratch path)
//
// After: each phase shows a short human label, no step counter.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh wizard-step-counter <pr-number>

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

function stateFor(phase, extra = {}) {
  return {
    onboarded: false,
    phase,
    form_answers: { company_name: "Acme Billing" },
    pending_suggestion: null,
    ...extra,
  };
}

async function installMocks(context, stateFixture) {
  await installCommonMocks(context, {
    extra: async (ctx) => {
      await ctx.route("**/api/onboarding/state", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify(stateFixture),
        }),
      );
      await ctx.route("**/api/onboarding/answer", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ ok: true }),
        }),
      );
    },
  });
}

async function bootWizard(page) {
  await page.goto(DEFAULT_BASE, { waitUntil: "load" });
  // Full-screen wizard mounts at top level via RootRoute when the boot
  // phase is set and onboarded is false.
  await page.waitForSelector('[data-testid="onboarding-chat"]', {
    timeout: 15_000,
  });
  await page.waitForTimeout(300);
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1280, height: 800 },
});

const FRAMES = [
  ["greet", "01-greet-office-name"],
  ["identity", "02-identity-what-you-do"],
  ["website", "03-website"],
  ["scan", "04-scan"],
  ["blueprint", "05-blueprint-pick-a-starter"],
  ["team", "06-team-confirm"],
  ["bridge", "07-bridge-first-task"],
];

for (const [phase, label] of FRAMES) {
  await installMocks(context, stateFor(phase));
  await bootWizard(page);
  await shotPage(page, OUT, label);
}

console.log(`captured ${FRAMES.length} screenshots to ${OUT}`);
await browser.close();
