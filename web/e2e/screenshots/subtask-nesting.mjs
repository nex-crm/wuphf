// Capture the sub-task nesting fix: a parent Task's sub-tasks render nested
// directly under its card, in the SAME lane as the parent, each carrying its
// own dedicated chat channel. Mocks /api/office/stats, /api/tasks, and the
// scheduler so the board renders without a live broker.
//
// Run via:
//   web/e2e/screenshots/publish.sh subtask-nesting <pr-number>

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
    active: 1,
    blocked: 0,
    review: 0,
    needs_human: 0,
    done: 1,
    archive: 0,
  },
  requests: { blocking: 0, notices: 0 },
  inbox_attention: 0,
  wiki_articles: 4,
  agents_active: 3,
  generated_at: NOW,
};

// One parent Issue with two sub-tasks. Each sub-task carries its OWN
// task-<id> channel (the fix) — distinct from the parent's — and nests under
// the parent card. The second child is in a "done" state, proving a child
// stays under its parent regardless of its own lifecycle stage.
const TASKS = {
  tasks: [
    {
      id: "office-1",
      title: "Launch the Q3 referral pilot",
      status: "in_progress",
      task_type: "issue",
      lifecycle_state: "running",
      owner: "ada",
      channel: "task-office-1",
    },
    {
      id: "office-2",
      title: "Draft the announcement copy",
      status: "in_progress",
      task_type: "issue",
      lifecycle_state: "running",
      owner: "wren",
      parent_issue_id: "office-1",
      channel: "task-office-2",
    },
    {
      id: "office-3",
      title: "Wire up the referral tracking",
      status: "done",
      task_type: "issue",
      lifecycle_state: "approved",
      owner: "milo",
      parent_issue_id: "office-1",
      channel: "task-office-3",
    },
    {
      id: "office-4",
      title: "Audit the billing webhooks",
      status: "open",
      task_type: "issue",
      lifecycle_state: "intake",
      owner: "",
      channel: "task-office-4",
    },
    {
      id: "office-5",
      title: "Migrate the wiki search index",
      status: "done",
      task_type: "issue",
      lifecycle_state: "approved",
      owner: "wren",
      channel: "task-office-5",
    },
  ],
};

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1280, height: 820 },
});

// Anchor on the real `/api/...` request path (regex, not glob) so the vite
// module `/src/api/tasks.ts` isn't matched and served as JSON.
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
    await ctx.route(/\/api\/scheduler(\?|$)/, (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ jobs: [] }),
      }),
    );
    await ctx.route(/\/api\/inbox\/items(\?|$)/, (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ items: [], counts: {}, refreshedAt: NOW }),
      }),
    );
  },
});

await page.goto(`${DEFAULT_BASE}/#/tasks`, { waitUntil: "load" });
await page.waitForSelector(".status-bar", { timeout: 45_000 });
await flipStore(page);
await page.waitForSelector('[data-testid="issues-list"]', { timeout: 45_000 });
// Readiness: wait until the parent's nested sub-task list has rendered.
await page.waitForSelector('[data-testid="issue-subtasks-office-1"]', {
  timeout: 15_000,
});

// 1. Whole board: the parent card in the In-progress lane carries its two
// sub-tasks nested beneath it, behind the guide rail.
await shotPage(page, OUT, "01-board-subtasks-nested-under-parent");

// 2. The parent's lane on its own: parent card + the two nested sub-task
// cards (one running, one done), tied to the parent in the same lane.
await shotElement(
  page,
  '[data-testid="issues-kanban-column-in_progress"]',
  OUT,
  "02-in-progress-lane-parent-with-subtasks",
);

console.log(`captured 2 screenshots to ${OUT}`);
await browser.close();
