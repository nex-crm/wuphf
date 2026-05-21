// Capture the wiki article loader's three observable states for issue #935:
//   1. healthy fetch — article renders within ~1s
//   2. stalled fetch — loader transitions to a retry-able error after 5s
//   3. `#/wiki/notebooks` — the legacy splat now redirects to /notebooks
//      and lands on the bookshelf empty state instead of hanging on
//      "Loading article…"
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh wiki-loader-stuck <pr-number>

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

const STUB_ARTICLE = {
  path: "team/about/README.md",
  title: "About",
  content:
    "# About\n\nThis team works on local-first AI agents. The home page is the wiki, and the wiki writes itself as agents do work.",
  last_edited_by: "ceo",
  last_edited_ts: "2026-05-19T12:00:00Z",
  revisions: 4,
  contributors: ["ceo", "pm"],
  backlinks: [],
  word_count: 42,
  categories: ["About"],
};

const EMPTY_CATALOG = { entries: [] };
const EMPTY_SECTIONS = { sections: [] };
const EMPTY_HISTORY = { commits: [] };
const EMPTY_NOTEBOOK_CATALOG = {
  agents: [],
  total_agents: 0,
  total_entries: 0,
  pending_promotion: 0,
};

const { browser, context, page, errors } = await launchBrowser({
  viewport: { width: 1480, height: 980 },
});

// ─── 1. Healthy article load ──
await installCommonMocks(context, {
  extra: async (ctx) => {
    await ctx.route("**/api/wiki/catalog*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(EMPTY_CATALOG),
      }),
    );
    await ctx.route("**/api/wiki/sections*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(EMPTY_SECTIONS),
      }),
    );
    await ctx.route("**/api/wiki/article*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(STUB_ARTICLE),
      }),
    );
    await ctx.route("**/api/wiki/history*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(EMPTY_HISTORY),
      }),
    );
    await ctx.route("**/api/wiki/visual-artifact*", (r) =>
      r.fulfill({ status: 404, body: "" }),
    );
    await ctx.route("**/api/humans*", (r) =>
      r.fulfill({ contentType: "application/json", body: JSON.stringify([]) }),
    );
  },
});
await bootShell(page);
await page.evaluate(() => {
  window.location.hash = "#/wiki/team/about/README.md";
});
await page.waitForSelector('[data-testid="wk-article-body"]', {
  timeout: 5_000,
});
await shotPage(page, OUT, "01-wiki-article-loaded");

// ─── 2. Stalled fetch → retry-able error ──
await installCommonMocks(context, {
  extra: async (ctx) => {
    await ctx.route("**/api/wiki/catalog*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(EMPTY_CATALOG),
      }),
    );
    await ctx.route("**/api/wiki/sections*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(EMPTY_SECTIONS),
      }),
    );
    // Hold the article fetch open so the loader transitions to the
    // 5s-timeout error state. This is exactly the B-08 hang the issue
    // describes — without the fix the placeholder would never resolve.
    await ctx.route("**/api/wiki/article*", () => new Promise(() => {}));
    await ctx.route("**/api/wiki/history*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(EMPTY_HISTORY),
      }),
    );
    await ctx.route("**/api/wiki/visual-artifact*", (r) =>
      r.fulfill({ status: 404, body: "" }),
    );
    await ctx.route("**/api/humans*", (r) =>
      r.fulfill({ contentType: "application/json", body: JSON.stringify([]) }),
    );
  },
});
await bootShell(page);
await page.evaluate(() => {
  window.location.hash = "#/wiki/team/about/README.md";
});
// Wait past the 5s timeout, then capture the error+retry state.
await page.waitForSelector(".wk-retry-btn", { timeout: 10_000 });
await shotPage(page, OUT, "02-wiki-article-timeout-retry");

// ─── 3. `#/wiki/notebooks` redirects to /notebooks (B-17 fix) ──
await installCommonMocks(context, {
  extra: async (ctx) => {
    await ctx.route("**/api/wiki/catalog*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(EMPTY_CATALOG),
      }),
    );
    await ctx.route("**/api/wiki/sections*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(EMPTY_SECTIONS),
      }),
    );
    await ctx.route("**/api/notebook/catalog*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(EMPTY_NOTEBOOK_CATALOG),
      }),
    );
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
  window.location.hash = "#/wiki/notebooks";
});
await page.waitForSelector('[data-testid="notebook-surface"]', {
  timeout: 10_000,
});
// Confirm the hash landed on /notebooks, not #/wiki/notebooks.
await page.waitForFunction(
  () => window.location.hash === "#/notebooks",
  null,
  { timeout: 5_000 },
);
await shotPage(page, OUT, "03-wiki-notebooks-redirect");

console.log(`captured 3 screenshots to ${OUT}`);
if (errors.length > 0) {
  console.error("page errors during capture:", errors);
}
await browser.close();
