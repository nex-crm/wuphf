// Capture obsidian-flavored callouts rendered in the wiki article surface.
// Real-example proof for PR #905 (wiki ↔ obsidian compatibility).
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh obsidian-callouts 905
//
// or stand-alone for local iteration:
//   BASE_URL=http://localhost:5273 WUPHF_SCREENSHOTS_OUT=/tmp/obsidian-shots \
//   node web/e2e/screenshots/obsidian-callouts.mjs

import process from "node:process";

import { bootShell, installCommonMocks, launchBrowser } from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh or set it manually");
  process.exit(2);
}

const ARTICLE_PATH = "people/sarah-chen";

const ARTICLE = {
  path: ARTICLE_PATH,
  title: "Sarah Chen",
  content: [
    "**Sarah Chen** is VP of Sales at [[companies/acme-corp|Acme Corp]] and the deal owner for the Q2 enterprise pilot.",
    "",
    "## Callout gallery",
    "",
    "> [!note] Quick context",
    "> Sarah joined Acme in 2024 from [[companies/initech]]. She owns enterprise pricing decisions.",
    "",
    "> [!info]",
    "> Pricing approval threshold is $250k ARR — anything above routes to her boss [[people/alex-kim]].",
    "",
    "> [!tip] Negotiation tactic",
    "> Reference the Q1 [[companies/contoso]] case study; Sarah cites it when objecting to a 3-year term.",
    "",
    "> [!important] Confidential",
    "> Sarah is mid-acquisition discussions with [[companies/globex]]. Do not surface this in shared channels.",
    "",
    "> [!warning] Stale contact",
    "> Last touch was 47 days ago. Re-engage before quarter-end or the pilot lapses.",
    "",
    "> [!caution] Hard line",
    "> No invoice-net-90 terms. She has rejected this twice; raising it again will burn the relationship.",
    "",
    "## Folded callouts",
    "",
    "> [!info]- Background — click to expand",
    "> Sarah's prior role at [[companies/initech]] involved a similar enterprise-pilot framework. The playbook there was: 30-day eval → 90-day paid pilot → ARR.",
    ">",
    "> She brings that mental model to Acme.",
    "",
    "> [!warning]+ Open question",
    "> Does Sarah's bonus structure depend on the Q2 pilot landing, or on full ARR? Confirm with [[people/alex-kim]] before our Thursday sync.",
    "",
    "## Wikilinks survive inside callouts",
    "",
    "> [!note] Cross-references",
    "> See also [[people/alex-kim]], [[companies/acme-corp]], and the [[playbooks/enterprise-pricing-objections|enterprise-pricing playbook]].",
  ].join("\n"),
  last_edited_by: "ceo",
  last_edited_ts: "2026-05-17T14:32:00Z",
  commit_sha: "a1b2c3d",
  revisions: 12,
  contributors: ["ceo", "pm", "archivist"],
  backlinks: [],
  word_count: 230,
  categories: ["Active pilot", "Enterprise"],
  human_read_count: 4,
  agent_read_count: 11,
  days_unread: 1,
};

const CATALOG = [
  { path: "people/sarah-chen", title: "Sarah Chen", author_slug: "ceo",
    last_edited_ts: ARTICLE.last_edited_ts, group: "people" },
  { path: "people/alex-kim", title: "Alex Kim", author_slug: "ceo",
    last_edited_ts: ARTICLE.last_edited_ts, group: "people" },
  { path: "companies/acme-corp", title: "Acme Corp", author_slug: "ceo",
    last_edited_ts: ARTICLE.last_edited_ts, group: "companies" },
  { path: "companies/contoso", title: "Contoso", author_slug: "ceo",
    last_edited_ts: ARTICLE.last_edited_ts, group: "companies" },
  // initech and globex deliberately absent → render as broken wikilinks
  // (red) to prove cross-reference resolution survives the callout boundary.
];

const wikiMocks = async (ctx) => {
  await ctx.route("**/api/wiki/article**", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify(ARTICLE) }),
  );
  await ctx.route("**/api/wiki/catalog", (r) =>
    r.fulfill({ contentType: "application/json", body: JSON.stringify({ articles: CATALOG }) }),
  );
  await ctx.route("**/api/wiki/sections", (r) =>
    r.fulfill({ contentType: "application/json", body: '{"sections":[]}' }),
  );
  await ctx.route("**/api/wiki/history**", (r) =>
    r.fulfill({ contentType: "application/json", body: '{"commits":[]}' }),
  );
  await ctx.route("**/api/wiki/sources**", (r) =>
    r.fulfill({ contentType: "application/json", body: '{"sources":[]}' }),
  );
};

const { browser, context, page, errors } = await launchBrowser({
  viewport: { width: 1400, height: 1800 },
});

await installCommonMocks(context, { extra: wikiMocks });
await bootShell(page, { afterFlipSelector: ".status-bar" });
await page.evaluate(async (path) => {
  const m = await import("/src/lib/router.ts");
  await m.router.navigate({ to: "/wiki/$", params: { _splat: path } });
}, ARTICLE_PATH);

// The article renders inside the wiki shell as an h1 followed by remark
// content. Wait on the callout aside since it's the actual proof; the h1
// can race with the markdown pipeline on cold HMR boots.
await page.locator(".wk-callout").first().waitFor({ timeout: 15_000 });
await page.locator("h1:has-text('Sarah Chen')").waitFor({ timeout: 5_000 });
await page.locator(".wk-callout").nth(5).waitFor({ timeout: 5_000 }); // sixth (caution) is the last in the gallery
await page.waitForTimeout(600); // let async wikilink resolution paint

if (errors.length > 0) {
  console.error("page errors:", errors);
}

// 1. Full-page hero: scroll to the gallery so it's centered for the
// header-context shot.
await page.locator("h2:has-text('Callout gallery')").scrollIntoViewIfNeeded();
await page.waitForTimeout(200);
await page.screenshot({
  path: `${OUT}/01-callouts-gallery-page.png`,
  animations: "disabled",
  fullPage: false,
});

// 2. Tight crop of the six callouts. A single bounding box covering all
// six produces one frame, not six.
async function clipBox(extractor, name) {
  const box = await page.evaluate(extractor);
  if (!box) return false;
  await page.screenshot({
    path: `${OUT}/${name}.png`,
    animations: "disabled",
    clip: box,
  });
  return true;
}

await clipBox(() => {
  const callouts = Array.from(document.querySelectorAll(".wk-callout"));
  if (callouts.length < 6) return null;
  const first = callouts[0].getBoundingClientRect();
  const last = callouts[5].getBoundingClientRect();
  return {
    x: Math.max(0, Math.floor(first.left - 12)),
    y: Math.max(0, Math.floor(first.top - 12)),
    width: Math.ceil(Math.max(first.width, last.width) + 24),
    height: Math.ceil(last.bottom - first.top + 24),
  };
}, "02-callouts-six-types");

// 3. Folded callouts — closed (info-) and open (warning+).
await page.locator("h2:has-text('Folded callouts')").scrollIntoViewIfNeeded();
await page.waitForTimeout(200);
await clipBox(() => {
  const heading = Array.from(document.querySelectorAll("h2"))
    .find((h) => h.textContent?.includes("Folded callouts"));
  if (!heading) return null;
  const callouts = [];
  let el = heading.nextElementSibling;
  while (el && el.tagName !== "H2") {
    if (el.classList?.contains("wk-callout")) callouts.push(el);
    el = el.nextElementSibling;
  }
  if (callouts.length === 0) return null;
  const first = callouts[0].getBoundingClientRect();
  const last = callouts[callouts.length - 1].getBoundingClientRect();
  return {
    x: Math.max(0, Math.floor(first.left - 12)),
    y: Math.max(0, Math.floor(first.top - 12)),
    width: Math.ceil(Math.max(first.width, last.width) + 24),
    height: Math.ceil(last.bottom - first.top + 24),
  };
}, "03-callouts-folded");

// 4. Single callout close-up showing wikilinks inside its body — the
// "cross-references survive the callout boundary" proof point.
await page.locator("h2:has-text('Wikilinks survive inside callouts')").scrollIntoViewIfNeeded();
await page.waitForTimeout(200);
await clipBox(() => {
  const heading = Array.from(document.querySelectorAll("h2"))
    .find((h) => h.textContent?.includes("Wikilinks survive"));
  if (!heading) return null;
  let el = heading.nextElementSibling;
  while (el && !el.classList?.contains("wk-callout")) el = el.nextElementSibling;
  if (!el) return null;
  const box = el.getBoundingClientRect();
  return {
    x: Math.max(0, Math.floor(box.left - 12)),
    y: Math.max(0, Math.floor(box.top - 12)),
    width: Math.ceil(box.width + 24),
    height: Math.ceil(box.height + 24),
  };
}, "04-callout-with-wikilinks");

console.log("captured callout screenshots to", OUT);
await browser.close();
