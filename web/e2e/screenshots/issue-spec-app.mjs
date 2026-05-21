// Capture the Issues app as a spec-level work surface, not a sidebar list.
//
// Run via:
//   web/e2e/screenshots/publish.sh issue-spec-app <pr-number>

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

const ISSUE_TASK = {
  id: "task-issue-001",
  channel: "product",
  title: "Spec human issue comments and review flow",
  details:
    "Create a project-sized issue spec for human comments, review gates, and agent task breakdown.",
  owner: "ceo",
  status: "open",
  task_type: "issue",
  pipeline_id: "issue",
  pipeline_stage: "draft",
  lifecycle_state: "drafting",
  issue_draft_spec: {
    goal: "Humans can file and discuss issue specs before agents cut execution tasks.",
    context: "Issues are project-sized specs, not every small follow-up.",
    approach: "Keep specs in the Issues app and task breakdowns in team_task records.",
    acceptance: "The sidebar links to Issues without rendering a sidebar issue list.",
    drafted_at: "2026-05-21T05:00:00Z",
  },
};

const SMALL_TASK = {
  id: "task-follow-up-001",
  channel: "product",
  title: "Reply with a status note",
  details: "Small follow-up that should not appear in the Issues app.",
  owner: "engineer",
  status: "open",
  task_type: "follow_up",
  pipeline_id: "follow_up",
};

async function installIssueAppMocks(context) {
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
          body: JSON.stringify({ tasks: [ISSUE_TASK, SMALL_TASK] }),
        }),
      );
    },
  });
}

async function gotoIssueRoute(page, hash, selector) {
  await page.goto(`${DEFAULT_BASE}/${hash}`, { waitUntil: "load" });
  await page.waitForSelector(".status-bar", { timeout: 15_000 });
  await flipStore(page);
  await page.waitForSelector(selector, { timeout: 10_000 });
  await page.waitForTimeout(400);
}

const { browser, context, page, errors } = await launchBrowser({
  viewport: { width: 1400, height: 900 },
});

await installIssueAppMocks(context);
await gotoIssueRoute(page, "#/issues", "[data-testid='issues-list']");
await shotPage(page, OUT, "01-issues-app-main-window");

await gotoIssueRoute(page, "#/issues/new", "[data-testid='issue-new']");
await shotPage(page, OUT, "02-issue-spec-form");

if (errors.length > 0) {
  console.error(errors.join("\n"));
  process.exit(1);
}

console.log(`captured 2 screenshots to ${OUT}`);
await browser.close();
