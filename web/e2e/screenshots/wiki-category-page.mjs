// Capture the Wikipedia-style category page after the Phase 3 nav flip:
// membership is now an article's many-to-many `categories:` frontmatter
// (catalog `categories`), with the folder `group` kept as a fallback.
//
//   1. An article (a hiring-loop playbook) showing its real category line —
//      it is filed in People + Revenue Operations, not its own folder.
//   2. Category: People — folder members PLUS that cross-folder playbook,
//      reached by clicking the category line (real in-app navigation).
//
// Navigation is via clicking the in-app category link rather than setting
// location.hash directly: the router round-trips the `_category/<slug>`
// pseudo-path, whereas a raw hash set percent-encodes the slash.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh wiki-category-page <pr-number>

import {
  DEFAULT_BASE,
  flipStore,
  installCommonMocks,
  launchBrowser,
  shotElement,
  shotPage,
} from "./lib.mjs";
import process from "node:process";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const e = (path, title, group, categories) => ({
  path,
  title,
  author_slug: "ceo",
  last_edited_ts: "2026-06-12T12:00:00Z",
  group,
  categories,
});

// People live in team/people/ (folder fallback). The hiring loop lives in
// team/playbooks/ but is filed into People via categories — it must show on
// the People category page. The MQL note is filed into a real category
// ("revenue-operations") that has no matching folder.
const CATALOG = [
  e("team/people/ana-ruiz.md", "Ana Ruiz", "people", []),
  e("team/people/dana-cole.md", "Dana Cole", "people", []),
  e("team/people/eng-watanabe.md", "Eng Watanabe", "people", []),
  e("team/people/zoe-park.md", "Zoe Park", "people", []),
  e("team/companies/acme-corp.md", "Acme Corp", "companies", ["companies"]),
  e("team/playbooks/hiring-loop.md", "Hiring Loop", "playbooks", [
    "people",
    "revenue-operations",
  ]),
  e("team/playbooks/mql-definition.md", "MQL Definition", "playbooks", [
    "revenue-operations",
  ]),
];

const ARTICLE = {
  path: "team/playbooks/hiring-loop.md",
  title: "Hiring Loop",
  content: [
    "# Hiring Loop",
    "",
    "**Hiring Loop** is the playbook the team runs to take an open role from",
    "intake to signed offer. It is filed under the People and Revenue",
    "Operations categories rather than its Playbooks folder.",
    "",
    "## Stages",
    "",
    "Intake → sourcing → structured loop → debrief → offer.",
  ].join("\n"),
  last_edited_by: "ceo",
  last_edited_ts: "2026-06-12T12:00:00Z",
  revisions: 4,
  contributors: ["ceo", "pm"],
  backlinks: [],
  word_count: 64,
  categories: ["people", "revenue-operations"],
};

async function mockWiki(ctx) {
  const json = (body) => (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify(body) });
  await ctx.route("**/api/wiki/catalog*", json({ articles: CATALOG }));
  await ctx.route("**/api/wiki/tree*", json({ nodes: [] }));
  await ctx.route("**/api/wiki/sections*", json({ sections: [] }));
  await ctx.route("**/api/wiki/categories*", json({ categories: [] }));
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

// Navigate via a full goto so the router initialises on the article route.
// (A raw `location.hash` set after boot desyncs the router and would mis-route
// the in-app category link to a search param.)
await page.goto(`${DEFAULT_BASE}/#/wiki/team/playbooks/hiring-loop.md`, {
  waitUntil: "load",
});
await page.waitForSelector(".status-bar", { timeout: 15_000 });
await flipStore(page, {});

// ─── The article, showing its real (cross-folder) category line ──
await page.waitForSelector('[data-testid="wk-article-body"]', {
  timeout: 8_000,
});
// The Categories line links to People + Revenue Operations (real categories).
await page
  .locator('[aria-label="Categories"]')
  .getByRole("link", { name: "People" })
  .waitFor({ timeout: 4_000 });
await shotPage(page, OUT, "01-article-categories");

// ─── Click the People category link → the category page (real nav) ──
await page
  .locator('[aria-label="Categories"]')
  .getByRole("link", { name: "People" })
  .click();
await page.waitForSelector('[data-testid="wk-category-page"]', {
  timeout: 8_000,
});
await page.waitForFunction(() =>
  document.body.textContent?.includes("Hiring Loop"),
);
await shotElement(
  page,
  '[data-testid="wk-category-page"]',
  OUT,
  "02-category-people-cross-folder",
);

console.log(`captured 2 screenshots to ${OUT}`);
if (errors.length > 0) {
  console.error("page errors during capture:", errors);
}
await browser.close();
