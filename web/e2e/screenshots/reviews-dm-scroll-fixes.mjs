// Capture the two visible fixes in this PR:
// 1. Crowded wiki promotion reviews remain vertically scrollable.
// 2. The DM live stream accordion collapses while staying mounted.
//
// Run via:
//   web/e2e/screenshots/publish.sh reviews-dm-scroll-fixes <pr-number>

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

const now = new Date("2026-05-26T09:00:00Z").getTime();

const members = [
  {
    slug: "ceo",
    name: "CEO",
    role: "Lead",
    provider: "claude-code",
    status: "active",
    built_in: true,
    online: true,
  },
  {
    slug: "pm",
    name: "PM",
    role: "Product",
    provider: "claude-code",
    status: "active",
    built_in: true,
    online: true,
  },
];

function iso(hoursAgo) {
  return new Date(now - hoursAgo * 3_600_000).toISOString();
}

function crowdedReviews() {
  return Array.from({ length: 28 }, (_, index) => {
    const n = index + 1;
    return {
      id: `review-${n}`,
      agent_slug: "pm",
      entry_slug: `promotion-${n}`,
      entry_title: `Promotion review ${n}`,
      proposed_wiki_path: `team/playbooks/promotion-${n}.md`,
      excerpt:
        "A wiki promotion with enough detail to make the review card readable while the column overflows.",
      reviewer_slug: "ceo",
      state: n <= 22 ? "pending" : "in-review",
      submitted_ts: iso(n),
      updated_ts: iso(n / 2),
      comments: [],
    };
  });
}

const activeTasks = [
  {
    id: "task-live-stream",
    title: "Audit promotion queue scroll behavior",
    status: "in_progress",
    owner: "ceo",
    channel: "general",
    created_at: iso(8),
    updated_at: iso(1),
  },
];

const messages = [
  {
    id: "dm-1",
    from: "human",
    channel: "ceo__human",
    content: "Can you keep an eye on the review queue while I collapse logs?",
    timestamp: iso(1),
  },
  {
    id: "dm-2",
    from: "ceo",
    channel: "ceo__human",
    content: "Yes. The live stream can be tucked away without dropping state.",
    timestamp: iso(0.5),
  },
];

async function installFeatureMocks(context) {
  await installCommonMocks(context, {
    extra: async (ctx) => {
      await ctx.route("**/api/config", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({
            llm_provider: "claude-code",
            llm_provider_configured: true,
            memory_backend: "markdown",
            team_lead_slug: "ceo",
          }),
        }),
      );
      await ctx.route(/\/api\/office-members(?:\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ members, meta: { humanHasPosted: true } }),
        }),
      );
      await ctx.route(/\/api\/members(?:\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ members, meta: { humanHasPosted: true } }),
        }),
      );
      await ctx.route(/\/api\/channels(?:\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({
            channels: [
              {
                slug: "general",
                name: "General",
                description: "Shared office channel",
              },
            ],
          }),
        }),
      );
      await ctx.route(/\/api\/messages(?:\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ messages }),
        }),
      );
      await ctx.route(/\/api\/tasks(?:\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ tasks: activeTasks }),
        }),
      );
      await ctx.route(/\/api\/agent-logs(?:\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({
            tasks: [
              {
                taskId: "task-live-stream",
                agentSlug: "ceo",
                toolCallCount: 7,
                firstToolAt: now - 30 * 60_000,
                lastToolAt: now - 5 * 60_000,
                hasError: false,
                sizeBytes: 4096,
              },
            ],
          }),
        }),
      );
      await ctx.route(/\/api\/review\/list(?:\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ reviews: crowdedReviews() }),
        }),
      );
      await ctx.route(/\/api\/agent-stream\/ceo(?:\?|$)/, async (r) =>
        r.fulfill({
          status: 200,
          headers: {
            "content-type": "text/event-stream",
            "cache-control": "no-cache",
          },
          body:
            'data: {"kind":"headless_event","status":"running","message":"Inspecting review queue"}\n\n',
        }),
      );
    },
  });
}

const { browser, context, page, errors } = await launchBrowser({
  viewport: { width: 1440, height: 900 },
});

await installFeatureMocks(context);
await page.goto(`${DEFAULT_BASE}/`, { waitUntil: "load" });
await page.waitForFunction(() => {
  const root = document.getElementById("root");
  return root && root.children.length > 0;
});
await flipStore(page);
await page.waitForTimeout(1_500);

await page.evaluate(() => {
  window.location.hash = "#/reviews";
});
await page.waitForSelector('[data-testid="review-queue-surface"]', {
  timeout: 10_000,
});
await page.waitForSelector(".nb-review-columns", { timeout: 10_000 });
await page.locator(".nb-review-columns").evaluate((el) => {
  el.scrollTop = Math.round(el.scrollHeight * 0.45);
});
await shotPage(page, OUT, "01-reviews-promotions-scrollable");
await shotElement(
  page,
  '[data-testid="review-queue-surface"]',
  OUT,
  "02-reviews-kanban-scroll-region",
);

await page.evaluate(() => {
  window.location.hash = "#/dm/ceo";
});
await page.waitForSelector('[data-testid="dm-workbench"]', {
  timeout: 10_000,
});
await page
  .getByTestId("collapsible-live-stream")
  .getByRole("button", { name: /live stream/i })
  .click();
await page.waitForFunction(
  () => {
    const section = document.querySelector(
      '[data-testid="collapsible-live-stream"]',
    );
    const header = section?.querySelector(".collapsible-section-header");
    const body = section?.querySelector(".collapsible-section-body");
    return (
      header?.getAttribute("aria-expanded") === "false" &&
      body instanceof HTMLElement &&
      body.hidden
    );
  },
  { timeout: 10_000 },
);
await shotPage(page, OUT, "03-dm-live-stream-collapsed");
await shotElement(
  page,
  '[data-testid="agent-workbench-pane"]',
  OUT,
  "04-agent-workbench-live-stream-collapsed",
);

console.log(`captured 4 screenshots to ${OUT}`);
if (errors.length > 0) {
  console.error("page errors during capture:", errors);
}
await browser.close();
