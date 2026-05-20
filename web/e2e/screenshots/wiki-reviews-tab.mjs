// Capture the Reviews tab inside Wiki, restored after Phase 2b
// retired the standalone /reviews surface. Captures the wiki shell with
// the Reviews tab active, plus the 5-column Kanban empty state.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh wiki-reviews-tab <pr-number>

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

const { browser, context, page, errors } = await launchBrowser({
  viewport: { width: 1480, height: 980 },
});

await installCommonMocks(context, {
  extra: async (ctx) => {
    await ctx.route("**/api/review/list*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ reviews: [] }),
      }),
    );
  },
});

await bootShell(page);
await page.evaluate(() => {
  window.location.hash = "#/reviews";
});
await page.waitForSelector('[data-testid="review-queue-surface"]', {
  timeout: 10_000,
});
// Let the wiki-tabs badge fetch settle so the chrome stops flashing.
await page.waitForTimeout(500);

// 1. Full Wiki shell — Reviews tab active, 5-column empty Kanban below.
await shotPage(page, OUT, "01-wiki-reviews-tab-active");

// 2. Just the wiki-tabs chrome — proves the Reviews tab + badge slot are back.
await shotElement(page, ".wiki-tabs", OUT, "02-wiki-tabs-bar");

// 3. The Kanban surface alone (5 columns, "Empty" placeholder per column).
await shotElement(
  page,
  '[data-testid="review-queue-surface"]',
  OUT,
  "03-reviews-kanban-empty",
);

console.log(`captured 3 screenshots to ${OUT}`);
if (errors.length > 0) {
  console.error("page errors during capture:", errors);
}
await browser.close();
