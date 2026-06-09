// Capture the two visible deltas of this PR:
//   1. The sidebar TOOLS list no longer carries Console or Receipts.
//   2. The per-task Activity rail is a state-change audit — lifecycle
//      transitions, requests, and sub-issue creations only (no comments,
//      no generic action-log noise). The comment event in the mock is
//      deliberately present in the payload and absent from the render.
//
// Run via:
//   web/e2e/screenshots/publish.sh remove-receipts-console <pr-number>

import process from "node:process";

import {
  DEFAULT_BASE,
  flipStore,
  installCommonMocks,
  launchBrowser,
  shotElement,
  shotPage,
} from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const TASK_ID = "task-audit-001";

// /tasks/<id> decision-packet shape — normalizeTaskDocument accepts the
// wrapped { task: {...} } form.
const TASK_DOC = {
  task: {
    id: TASK_ID,
    title: "Add account-level audit log export",
    details:
      "Operators need a CSV export of account audit events for compliance reviews.",
    channel: "engineering",
    owner: "engineer",
    lifecycle_state: "running",
    updated_at: "2026-06-09T10:05:00Z",
  },
};

// /tasks/<id>/activity — a mix of kinds. Only lifecycle / request /
// sub_issue should render; the comment is filtered out (it lives in chat).
const ACTIVITY = {
  task_id: TASK_ID,
  events: [
    {
      id: "ev-lifecycle",
      kind: "lifecycle",
      timestamp: "2026-06-09T09:30:00Z",
      actor: "ceo",
      summary: "Approved & started",
      lifecycle: { from: "drafting", to: "running" },
    },
    {
      id: "ev-request",
      kind: "request",
      timestamp: "2026-06-09T09:45:00Z",
      actor: "engineer",
      summary: "Should the export include the source IP address?",
      request: {
        request_id: "req-1",
        status: "open",
        question: "Should the export include the source IP address?",
        blocking: true,
      },
    },
    {
      id: "ev-subissue",
      kind: "sub_issue",
      timestamp: "2026-06-09T09:55:00Z",
      actor: "engineer",
      summary: "Added a sub-task: Write the CSV serializer",
      sub_issue: { sub_issue_id: "task-audit-002", title: "Write the CSV serializer" },
    },
    {
      // Excluded from the audit — this is what now lives in the chat only.
      id: "ev-comment",
      kind: "comment",
      timestamp: "2026-06-09T10:00:00Z",
      actor: "human",
      summary: "This comment should appear in the chat, not the Activity rail.",
    },
  ],
};

async function installMocks(browserContext) {
  await installCommonMocks(browserContext, {
    extra: async (ctx) => {
      await ctx.route("**/api/messages*", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ messages: [] }),
        }),
      );
      await ctx.route("**/api/office-members*", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ members: [], meta: {} }),
        }),
      );
      await ctx.route(/\/api\/tasks(?:[/?]|$)/, (route) => {
        const url = route.request().url();
        if (/\/tasks\/[^?/]+\/activity/.test(url)) {
          return route.fulfill({
            contentType: "application/json",
            body: JSON.stringify(ACTIVITY),
          });
        }
        const idMatch = url.match(/\/tasks\/([^?/]+)(?:\?|$)/);
        if (idMatch && idMatch[1] === TASK_ID) {
          return route.fulfill({
            contentType: "application/json",
            body: JSON.stringify(TASK_DOC),
          });
        }
        // List / sub-task queries.
        return route.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ tasks: [] }),
        });
      });
    },
  });
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1400, height: 920 },
});

await installMocks(context);

// ── 1. Sidebar without Console / Receipts ──────────────────────────────
await page.goto(`${DEFAULT_BASE}/#/`, { waitUntil: "load" });
await page.waitForSelector(".status-bar", { timeout: 15_000 });
await flipStore(page);
await page.waitForSelector("aside.sidebar .sidebar-apps", { timeout: 10_000 });
await page.waitForTimeout(400);
await shotElement(page, "aside.sidebar", OUT, "01-sidebar-no-console-receipts");

// ── 2. Task Activity rail — state-change audit only ─────────────────────
await page.goto(`${DEFAULT_BASE}/#/tasks/${TASK_ID}`, { waitUntil: "load" });
await page.waitForSelector(".status-bar", { timeout: 15_000 });
await flipStore(page);
await page.waitForSelector("[data-testid='task-rail-activity']", {
  timeout: 10_000,
});
// The Activity section is collapsed by default — expand it so the feed mounts.
await page.click("[data-testid='task-rail-activity'] .task-rail-section-header");
await page.waitForSelector(".issue-activity-feed-list", { timeout: 10_000 });
await page.waitForTimeout(400);
await shotElement(
  page,
  "[data-testid='task-rail-activity']",
  OUT,
  "02-activity-rail-state-events",
);
await shotPage(page, OUT, "03-task-detail-context");

console.log(`captured 3 screenshots to ${OUT}`);
await browser.close();
