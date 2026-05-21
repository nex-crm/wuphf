// Capture the issue detail surface with the human comment composer.
//
// Run via:
//   web/e2e/screenshots/publish.sh issue-document-comments <pr-number>

import process from "node:process";

import {
  DEFAULT_BASE,
  installCommonMocks,
  launchBrowser,
  shotPage,
} from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const TASK_ID = "task-comment-001";

const TASK_ROW = {
  id: TASK_ID,
  channel: "engineering",
  title: "Add account-level audit log export",
  details:
    "Operators need a CSV export of account audit events for compliance reviews.",
  owner: "engineer",
  status: "blocked",
  lifecycle_state: "blocked_on_pr_merge",
  updated_at: "2026-05-21T04:40:00Z",
};

const DECISION_PACKET = {
  taskId: TASK_ID,
  lifecycleState: "blocked_on_pr_merge",
  spec: {
    problem:
      "Compliance reviewers need account audit events without manually querying logs.",
    targetOutcome:
      "A workspace admin can export account audit events as a CSV from settings.",
    assignment:
      "Add CSV export for account audit logs, including actor, action, target, and timestamp.",
    acceptanceCriteria: [
      { statement: "Workspace admins can download a CSV from settings." },
      { statement: "The export includes actor, action, target, and timestamp." },
      { statement: "Non-admin users cannot access the export endpoint." },
    ],
    feedback: [
      {
        id: "comment-ceo",
        author: "ceo",
        body: "Blocked on confirming whether security wants IP address in the first release.",
        appendedAt: "2026-05-21T04:30:00Z",
      },
    ],
  },
  updatedAt: "2026-05-21T04:40:00Z",
};

async function installIssueMocks(context) {
  await installCommonMocks(context, {
    extra: async (ctx) => {
      await ctx.route(/\/api\/tasks(?:[/?]|$)/, (route) => {
        const url = route.request().url();
        const match = url.match(/\/tasks\/([^?/]+)(\?|$)/);
        if (match) {
          return route.fulfill({
            contentType: "application/json",
            body: JSON.stringify(DECISION_PACKET),
          });
        }
        return route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ tasks: [TASK_ROW] }),
        });
      });
    },
  });
}

async function bootIssue(page) {
  await page.goto(`${DEFAULT_BASE}/#/issues/${TASK_ID}`, { waitUntil: "load" });
  await page.waitForSelector("[data-testid='issue-comment-form']", {
    timeout: 10_000,
  });
  await page.waitForTimeout(400);
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1280, height: 900 },
});

await installIssueMocks(context);
await bootIssue(page);
await shotPage(page, OUT, "01-issue-comment-form");

await page
  .getByTestId("issue-comment-input")
  .fill("Please include source IP in the export if it is already captured.");
await shotPage(page, OUT, "02-issue-comment-composing");

console.log(`captured 2 screenshots to ${OUT}`);
await browser.close();
