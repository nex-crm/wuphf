// Capture the restored Tasks sidebar entry + /tasks board. PR #972 made
// the Issues kanban spec-only; this PR brings back the standalone Tasks
// surface so non-issue tasks (research, follow_up, launch, feature) stay
// visible.
//
// Run via:
//   web/e2e/screenshots/publish.sh tasks-visibility <pr-number>

import process from "node:process";

import {
  DEFAULT_BASE,
  flipStore,
  installCommonMocks,
  launchBrowser,
  shotPage,
} from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const TASKS = [
  {
    id: "task-follow-up-001",
    channel: "product",
    title: "Reply to Lenny with status note",
    details: "Quick follow-up after yesterday's call.",
    owner: "engineer",
    status: "open",
    task_type: "follow_up",
  },
  {
    id: "task-research-002",
    channel: "research",
    title: "Audit billing latency over the last 30 days",
    owner: "analyst",
    status: "in_progress",
    task_type: "research",
  },
  {
    id: "task-launch-003",
    channel: "gtm",
    title: "Ship the Q3 pricing-page launch checklist",
    owner: "marketing",
    status: "review",
    task_type: "launch",
  },
  {
    id: "task-feature-004",
    channel: "product",
    title: "Implement webhook retry backoff",
    owner: "engineer",
    status: "blocked",
    task_type: "feature",
  },
  {
    id: "task-done-005",
    channel: "product",
    title: "Cut release v0.84.0",
    owner: "ceo",
    status: "done",
    task_type: "launch",
  },
];

async function installTasksMocks(context) {
  await installCommonMocks(context, {
    extra: async (ctx) => {
      await ctx.route("**/api/review/list?scope=all", (route) =>
        route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ reviews: [] }),
        }),
      );
      await ctx.route(/\/api\/tasks(?:[/?]|$)/, (route) =>
        route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ tasks: TASKS }),
        }),
      );
    },
  });
}

async function gotoRoute(page, hash, selector) {
  await page.goto(`${DEFAULT_BASE}/${hash}`, { waitUntil: "load" });
  await page.waitForSelector(".status-bar", { timeout: 15_000 });
  await flipStore(page);
  await page.waitForSelector(selector, { timeout: 10_000 });
  await page.waitForTimeout(400);
}

const { browser, context, page, errors } = await launchBrowser({
  viewport: { width: 1400, height: 900 },
});

await installTasksMocks(context);

// 1. /tasks now mounts the TasksApp board with the sidebar entry highlighted.
await gotoRoute(page, "#/tasks", "[data-testid='app-page-tasks']");
await shotPage(page, OUT, "01-tasks-board-restored");

// 2. /issues remains spec-only (empty in this seed — no issue-spec tasks).
await gotoRoute(page, "#/issues", "[data-testid='issues-list-empty']");
await shotPage(page, OUT, "02-issues-still-spec-only");

if (errors.length > 0) {
  console.error(errors.join("\n"));
  process.exit(1);
}

console.log(`captured 2 screenshots to ${OUT}`);
await browser.close();
