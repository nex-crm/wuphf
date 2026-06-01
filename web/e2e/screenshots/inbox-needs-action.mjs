// Capture the Inbox surface before vs. after the Needs action filter
// becomes the default tab. Mocks /api/inbox/items with three pending
// rows + two terminal/back-with-author rows (rejected task, review in
// changes_requested) so the All vs. Needs action delta is visible.
//
// Run via:
//   web/e2e/screenshots/publish.sh inbox-needs-action <pr-number>

import process from "node:process";

import {
  DEFAULT_BASE,
  flipStore,
  installCommonMocks,
  launchBrowser,
  shotElement,
} from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const NOW = new Date().toISOString();

const INBOX_ITEMS = {
  items: [
    {
      kind: "task",
      taskId: "task-2741",
      title: "Refactor agent-rail event pill state",
      agentSlug: "mira",
      createdAt: NOW,
      isUnread: true,
      task: {
        taskId: "task-2741",
        title: "Refactor agent-rail event pill state",
        assignment: "Decide whether to ship the refactor",
        state: "decision",
        severityCounts: {
          critical: 0,
          major: 1,
          minor: 0,
          nitpick: 0,
          skipped: 0,
        },
        lastChangedAt: NOW,
        elapsed: "10m",
        isUrgent: false,
      },
    },
    {
      kind: "request",
      requestId: "req-1",
      title: "Bump Postgres to 17?",
      agentSlug: "ada",
      channel: "general",
      createdAt: NOW,
      request: {
        kind: "approval",
        question: "Bump Postgres to 17 in staging?",
        from: "ada",
        blocking: true,
      },
    },
    {
      kind: "review",
      reviewId: "rev-1",
      title: "Promote draft to wiki",
      agentSlug: "wren",
      createdAt: NOW,
      review: {
        state: "pending",
        reviewerSlug: "owner",
        sourceSlug: "wren",
        targetPath: "wiki/draft.md",
      },
    },
    {
      kind: "task",
      taskId: "task-rejected",
      title: "Reject ship of the refactor",
      agentSlug: "mira",
      createdAt: NOW,
      task: {
        taskId: "task-rejected",
        title: "Reject ship of the refactor",
        assignment: "Triage the rejection",
        state: "rejected",
        severityCounts: {
          critical: 0,
          major: 0,
          minor: 0,
          nitpick: 0,
          skipped: 0,
        },
        lastChangedAt: NOW,
        elapsed: "1h",
        isUrgent: false,
      },
    },
    {
      kind: "review",
      reviewId: "rev-cr",
      title: "Wiki promotion bounced back",
      agentSlug: "wren",
      createdAt: NOW,
      review: {
        state: "changes_requested",
        reviewerSlug: "owner",
        sourceSlug: "wren",
        targetPath: "wiki/draft.md",
      },
    },
  ],
  counts: {
    decisionRequired: 0,
    running: 0,
    blocked: 0,
    approvedToday: 0,
    unread: 1,
  },
  refreshedAt: NOW,
};

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1280, height: 800 },
});

await installCommonMocks(context, {
  extra: async (ctx) => {
    await ctx.route("**/api/inbox/items*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(INBOX_ITEMS),
      }),
    );
    await ctx.route("**/api/requests*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ requests: [] }),
      }),
    );
    await ctx.route("**/api/reviews*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ reviews: [] }),
      }),
    );
  },
});

await page.goto(`${DEFAULT_BASE}/inbox`, { waitUntil: "load" });
await page.waitForSelector(".status-bar", { timeout: 15_000 });
await flipStore(page);
await page.waitForSelector(".inbox-filter-bar", { timeout: 10_000 });
await page.waitForTimeout(400);

// 1. Default tab — Needs action is selected, terminal/back-with-author
// rows are hidden, three actionable rows remain.
await shotElement(page, ".inbox-shell", OUT, "01-needs-action-default");

// 2. Click All to reveal the full actionable list including rejected
// + changes_requested rows.
await page.locator('[data-testid="inbox-filter-all"]').click();
await page.waitForTimeout(200);
await shotElement(page, ".inbox-shell", OUT, "02-all-shows-everything");

console.log(`captured 2 screenshots to ${OUT}`);
await browser.close();
