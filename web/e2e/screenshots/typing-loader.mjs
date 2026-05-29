// Capture the inline typing indicator (Claude-style ThinkingLoader with The
// Office verbs) rendered at the foot of the message feed, across all three
// themes, plus the multi-agent stacked-avatar variant.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh typing-loader <pr-number>

import {
  DEFAULT_BASE,
  flipStore,
  installCommonMocks,
  launchBrowser,
  shotElement,
} from "./lib.mjs";
import process from "node:process";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const CONFIG = {
  llm_provider: "claude-code",
  llm_provider_configured: true,
  memory_backend: "markdown",
  team_lead_slug: "ceo",
};

const MESSAGES = [
  {
    id: "msg-1",
    from: "you",
    channel: "general",
    content: "@dwight what is the status of the Scranton numbers?",
    timestamp: "2026-05-29T12:00:00Z",
  },
  {
    id: "msg-2",
    from: "dwight",
    channel: "general",
    content:
      "Pulling them now. Fact: the Scranton branch is the most efficient branch.",
    timestamp: "2026-05-29T12:00:30Z",
  },
];

// One active agent → single bubble; two → stacked avatars. `status: "active"`
// is what TypingIndicator keys on.
function memberPayload(members) {
  return { members, meta: { humanHasPosted: true } };
}

const SINGLE = memberPayload([
  {
    slug: "dwight",
    name: "Dwight",
    role: "Sales",
    provider: "claude-code",
    built_in: true,
    online: true,
    status: "active",
  },
]);

const MULTI = memberPayload([
  {
    slug: "dwight",
    name: "Dwight",
    role: "Sales",
    provider: "claude-code",
    built_in: true,
    online: true,
    status: "active",
  },
  {
    slug: "jim",
    name: "Jim",
    role: "Sales",
    provider: "claude-code",
    built_in: true,
    online: true,
    status: "active",
  },
]);

const json = (body) => ({
  contentType: "application/json",
  body: JSON.stringify(body),
});

async function mountFeed(context, members) {
  await installCommonMocks(context, {
    extra: async (ctx) => {
      await ctx.route("**/api/config", (r) => r.fulfill(json(CONFIG)));
      await ctx.route("**/api/office-members", (r) => r.fulfill(json(members)));
      // Channel members empty → the indicator falls back to all active office
      // members, which keeps this fixture independent of /members membership.
      await ctx.route("**/api/members*", (r) =>
        r.fulfill(json({ members: [] })),
      );
      await ctx.route("**/api/channels", (r) =>
        r.fulfill(
          json({
            channels: [
              { slug: "general", name: "General", description: "Shared" },
            ],
          }),
        ),
      );
      await ctx.route("**/api/messages*", (r) =>
        r.fulfill(json({ messages: MESSAGES })),
      );
      await ctx.route("**/api/workspaces/list", (r) =>
        r.fulfill(json({ workspaces: [] })),
      );
      await ctx.route("**/api/requests*", (r) =>
        r.fulfill(json({ requests: [] })),
      );
      await ctx.route("**/api/review/list*", (r) =>
        r.fulfill(json({ reviews: [] })),
      );
      await ctx.route("**/api/tasks/inbox", (r) =>
        r.fulfill(
          json({
            rows: [],
            counts: {
              decisionRequired: 0,
              running: 0,
              blocked: 0,
              mergedToday: 0,
            },
            refreshedAt: "2026-05-29T12:00:00Z",
          }),
        ),
      );
      await ctx.route("**/api/tasks?*", (r) => r.fulfill(json({ tasks: [] })));
      await ctx.route("**/api/usage", (r) =>
        r.fulfill(
          json({
            total: {
              input_tokens: 0,
              output_tokens: 0,
              cache_read_tokens: 0,
              cache_creation_tokens: 0,
              total_tokens: 0,
              cost_usd: 0,
              requests: 0,
            },
          }),
        ),
      );
      await ctx.route("**/api/commands", (r) => r.fulfill(json([])));
      await ctx.route("**/api/upgrade-check", (r) =>
        r.fulfill(
          json({
            current: "0.83.5",
            latest: "0.83.5",
            upgrade_available: false,
            is_dev_build: false,
          }),
        ),
      );
    },
  });
}

const { browser, context, page, errors } = await launchBrowser({
  viewport: { width: 1280, height: 850 },
});

async function capture(members, theme, name) {
  // Park on a blank page first so the previous capture's React Query
  // refetch timers are torn down before we swap routes — otherwise an
  // in-flight poll hits an unrouted endpoint and logs a transient 502.
  await page.goto("about:blank", { waitUntil: "load" });
  await mountFeed(context, members);
  await page.goto(`${DEFAULT_BASE}/#/channels/general`, { waitUntil: "load" });
  await page.waitForSelector(".status-bar", { timeout: 15_000 });
  await flipStore(page);
  // Drive the real setTheme action (not setState) so data-theme is written and
  // RootRoute's effect swaps in the matching /themes/<id>.css.
  await page.evaluate(async (t) => {
    const m = await import("/src/stores/app.ts");
    m.useAppStore.getState().setTheme(t);
  }, theme);
  await page.waitForSelector(`html[data-theme="${theme}"]`, {
    timeout: 10_000,
  });
  try {
    await page.waitForSelector(".typing-message", { timeout: 10_000 });
  } catch (err) {
    console.error(await page.locator("body").innerText());
    throw err;
  }
  // Let the phrase cycle land on a real Office verb (initial mount may show
  // the first tick) and the dots settle into their wave.
  await page.waitForTimeout(900);
  const header = await page
    .locator(".typing-message .message-author")
    .innerText();
  console.log(`[${name}] typing header → ${header}`);
  await shotElement(page, ".conversation-chat", OUT, name);
}

await capture(MULTI, "noir-gold", "01-typing-multi-noir");
await capture(SINGLE, "nex", "02-typing-feed-light");
await capture(SINGLE, "nex-dark", "03-typing-feed-dark");

// Transient resource blips (502s from poll requests racing a route swap
// during capture) are a harness artifact, not a product error. Real page
// errors (pageerror, uncaught) still fail the run.
const realErrors = errors.filter((e) => !e.includes("Failed to load resource"));
if (realErrors.length > 0) {
  console.error(realErrors.join("\n"));
  await browser.close();
  process.exit(1);
}

console.log(`captured 3 screenshots to ${OUT}`);
await browser.close();
