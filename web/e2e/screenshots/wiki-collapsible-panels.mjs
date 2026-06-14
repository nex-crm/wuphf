// Capture the wiki's collapsible flanking panels across their four states.
// The left page tree and the right "details" rail each fold to a thin,
// clickable strip so the reading column reclaims the width.
//
//   1. Both panels open — the baseline three-column article view.
//   2. Left page tree folded to a rail.
//   3. Right details rail folded.
//   4. Both folded — the reading column at its widest.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh wiki-collapsible-panels <pr-number>

import {
  bootShell,
  installCommonMocks,
  launchBrowser,
  shotPage,
} from "./lib.mjs";
import process from "node:process";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const ARTICLE = {
  path: "team/companies/acme.md",
  title: "Acme Corp",
  content: [
    "# Acme Corp",
    "",
    "Acme is a mid-market logistics company evaluating our platform for",
    "their revenue operations team.",
    "",
    "## Background",
    "",
    "Founded in 2014, Acme runs a regional freight network across the",
    "Midwest. Their RevOps lead, Dana, owns the evaluation.",
    "",
    "## Current status",
    "",
    "In a paid pilot. Two agents are embedded in their Slack, drafting",
    "renewal briefs and reconciling the carrier scorecard each week.",
    "",
    "## Open questions",
    "",
    "Whether the pilot expands to the full carrier portfolio after Q3.",
  ].join("\n"),
  last_edited_by: "ceo",
  last_edited_ts: "2026-06-12T12:00:00Z",
  revisions: 7,
  contributors: ["ceo", "pm", "revops"],
  backlinks: [],
  word_count: 96,
  categories: ["Companies"],
};

const TREE = [
  {
    name: "companies",
    path: "team/companies",
    type: "dir",
    title: "Companies",
    children: [
      {
        name: "acme.md",
        path: "team/companies/acme.md",
        type: "page",
        title: "Acme Corp",
      },
      {
        name: "globex.md",
        path: "team/companies/globex.md",
        type: "page",
        title: "Globex",
      },
    ],
  },
  {
    name: "people",
    path: "team/people",
    type: "dir",
    title: "People",
    children: [
      {
        name: "dana.md",
        path: "team/people/dana.md",
        type: "page",
        title: "Dana (RevOps lead)",
      },
    ],
  },
  {
    name: "playbooks",
    path: "team/playbooks",
    type: "dir",
    title: "Playbooks",
    children: [
      {
        name: "renewal.md",
        path: "team/playbooks/renewal.md",
        type: "page",
        title: "Renewal Playbook",
      },
    ],
  },
];

async function mockWiki(ctx) {
  const json = (body) => (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify(body) });
  await ctx.route("**/api/wiki/tree*", json({ nodes: TREE }));
  await ctx.route("**/api/wiki/catalog*", json({ entries: [] }));
  await ctx.route("**/api/wiki/sections*", json({ sections: [] }));
  await ctx.route("**/api/wiki/article*", json(ARTICLE));
  await ctx.route("**/api/wiki/history*", json({ commits: [] }));
  await ctx.route("**/api/wiki/visual-artifact*", (r) =>
    r.fulfill({ status: 404, body: "" }),
  );
  await ctx.route("**/api/humans*", json([]));
}

const { browser, context, page, errors } = await launchBrowser({
  viewport: { width: 1480, height: 940 },
});

await installCommonMocks(context, { extra: mockWiki });
await bootShell(page);
await page.evaluate(() => {
  window.location.hash = "#/wiki/team/companies/acme.md";
});
await page.waitForSelector('[data-testid="wk-article-body"]', {
  timeout: 8_000,
});
// Expand the open page's branch so the tree shows real structure. Scope to
// the sidebar — "Companies" also appears as a category link in the article.
await page
  .locator('[data-testid="wk-nav-sidebar"]')
  .getByText("Companies", { exact: true })
  .click();
await page.waitForTimeout(250);
await shotPage(page, OUT, "01-both-panels-open");

// ─── Fold the left page tree ──
await page.getByRole("button", { name: "Collapse Pages panel" }).click();
await page.waitForSelector('button[aria-label="Expand Pages panel"]', {
  timeout: 4_000,
});
await shotPage(page, OUT, "02-left-tree-collapsed");

// ─── Also fold the right details rail ──
await page.getByRole("button", { name: "Collapse Details panel" }).click();
await page.waitForSelector('button[aria-label="Expand Details panel"]', {
  timeout: 4_000,
});
await shotPage(page, OUT, "03-both-collapsed");

// ─── Restore the tree; keep the right rail folded ──
await page.getByRole("button", { name: "Expand Pages panel" }).click();
await page.waitForSelector('[data-testid="wk-tree"]', { timeout: 4_000 });
await shotPage(page, OUT, "04-right-rail-collapsed");

console.log(`captured 4 screenshots to ${OUT}`);
if (errors.length > 0) {
  console.error("page errors during capture:", errors);
}
await browser.close();
