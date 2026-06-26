// Capture the Inbox→Task-board consolidation: Tasks is the primary Work
// nav item with the attention badge, and the board's "Needs human input"
// lane folds in blocking requests + pending reviews next to decision-state
// tasks. Mocks /api/office/stats, /api/tasks, and /api/inbox/items so the
// board renders the folded lane without a live broker.
//
// Run via:
//   web/e2e/screenshots/publish.sh consolidate-inbox-tasks <pr-number>

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

const NOW = new Date().toISOString();

const STATS = {
  tasks: {
    backlog: 1,
    active: 2,
    blocked: 0,
    review: 0,
    needs_human: 1,
    done: 1,
    archive: 0,
  },
  requests: { blocking: 1, notices: 0 },
  // requests (1) + reviews (1) + decision task (1) = the attention roll-up
  // shown on the Tasks nav badge.
  inbox_attention: 3,
  wiki_articles: 4,
  agents_active: 2,
  generated_at: NOW,
};

const TASKS = {
  tasks: [
    {
      id: "task-decision",
      title: "Ship the agent-rail refactor",
      status: "review",
      task_type: "issue",
      lifecycle_state: "decision",
      owner: "mira",
      channel: "agent-rail-refactor",
    },
    {
      id: "task-running",
      title: "Wire the onboarding telemetry",
      status: "in_progress",
      task_type: "issue",
      lifecycle_state: "running",
      owner: "ada",
      channel: "onboarding-telemetry",
    },
    {
      id: "task-backlog",
      title: "Audit the billing webhooks",
      status: "open",
      task_type: "issue",
      lifecycle_state: "intake",
      owner: "",
      channel: "billing-webhooks",
    },
    {
      id: "task-done",
      title: "Migrate the wiki search index",
      status: "done",
      task_type: "issue",
      lifecycle_state: "approved",
      owner: "wren",
      channel: "wiki-search",
    },
  ],
};

const INBOX_ITEMS = {
  items: [
    {
      kind: "request",
      requestId: "req-1",
      title: "Bump Postgres to 17 in staging?",
      agentSlug: "ada",
      channel: "onboarding-telemetry",
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
      title: "Promote onboarding playbook to the wiki",
      agentSlug: "wren",
      createdAt: NOW,
      review: {
        state: "pending",
        reviewerSlug: "owner",
        sourceSlug: "wren",
        targetPath: "playbooks/onboarding.md",
      },
    },
  ],
  counts: {
    decisionRequired: 1,
    running: 0,
    blocked: 0,
    approvedToday: 0,
    unread: 2,
  },
  refreshedAt: NOW,
};

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1280, height: 820 },
});

// Anchor on the real `/api/...` request path. A glob like `**/api/tasks*`
// also matches the vite module `/src/api/tasks.ts`, which would serve the
// app's own source as JSON and wedge the boot — so match the API pathname
// (a `?query` or end-of-string boundary) with a regex instead.
await installCommonMocks(context, {
  extra: async (ctx) => {
    await ctx.route(/\/api\/office\/stats(\?|$)/, (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(STATS),
      }),
    );
    await ctx.route(/\/api\/tasks(\?|$)/, (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(TASKS),
      }),
    );
    await ctx.route(/\/api\/inbox\/items(\?|$)/, (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(INBOX_ITEMS),
      }),
    );
    await ctx.route(/\/api\/scheduler(\?|$)/, (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ jobs: [] }),
      }),
    );
    await ctx.route(/\/api\/review\/list(\?|$)/, (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ reviews: [] }),
      }),
    );
  },
});

// Go straight to the board. The mocked /onboarding/state (onboarded:true)
// drives the real init flow to render the Shell, so no store flip is
// needed; flipStore is kept as belt-and-suspenders without a second
// page load (a reload would reset the zustand store). The first compile
// is cold, so the status-bar wait is generous.
await page.goto(`${DEFAULT_BASE}/#/tasks`, { waitUntil: "load" });
await page.waitForSelector(".status-bar", { timeout: 45_000 });
await flipStore(page);
await page.waitForSelector('[data-testid="issues-list"]', { timeout: 15_000 });
// Readiness, not a fixed delay: wait until both folded cards have rendered
// in the Needs-human lane (networkidle is unusable here — the mocked
// /api/events SSE stream is held open on purpose, so it never settles).
await page.waitForSelector('[data-testid="attention-request-row"]', {
  timeout: 10_000,
});
await page.waitForSelector('[data-testid="attention-review-row"]', {
  timeout: 10_000,
});

// 1. Whole shell: Tasks is the first Work nav item with the attention
// badge, and the board is the surface it opens.
await shotPage(page, OUT, "01-tasks-primary-with-board");

// 2. The "Needs human input" lane: a decision task plus the folded
// request + review cards (the two non-task halves of the old Inbox).
await shotElement(
  page,
  '[data-testid="issues-kanban-column-needs_human"]',
  OUT,
  "02-needs-human-lane-folds-requests-reviews",
);

// 3. /inbox now redirects to the board — old bookmarks resolve.
await page.goto(`${DEFAULT_BASE}/#/inbox`, { waitUntil: "load" });
await page.waitForSelector(".status-bar", { timeout: 30_000 });
await page.waitForSelector('[data-testid="issues-list"]', { timeout: 10_000 });
// Assert the actual routed hash, not a loose substring — `includes("/tasks")`
// would false-pass on any URL that merely contains the word.
const hash = new URL(page.url()).hash;
if (hash !== "#/tasks") {
  console.error(`expected /inbox to redirect to #/tasks, got ${page.url()}`);
  process.exit(1);
}
await shotPage(page, OUT, "03-inbox-redirects-to-tasks");

console.log(`captured 3 screenshots to ${OUT}`);
await browser.close();
