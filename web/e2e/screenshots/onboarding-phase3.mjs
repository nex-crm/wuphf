// Capture Phase 3 Issue surface in 4-5 key states:
//   01 — Issues list empty state (no tasks)
//   02 — Issue document in Drafting state (full spec visible)
//   03 — Issue document in Running state (spec collapsed, timeline visible)
//   04 — Issue document in Approved state (spec collapsed, Expand affordance)
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh onboarding-phase3 <pr-number>
//
// No real broker needed. We mock /tasks?all_channels=true and /tasks/<id>
// endpoints with canned JSON so the components render in deterministic state.

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

// ── Fixtures ───────────────────────────────────────────────────────────────

const TASK_DRAFTING = {
  taskId: "task-001",
  id: "task-001",
  title: "Stripe webhook handler",
  lifecycleState: "drafting",
  lifecycle_state: "drafting",
  status: "open",
  pipeline_stage: "draft",
  spec: {
    goal: "Receive Stripe webhook events (charge.succeeded, charge.failed) and update local subscription state.",
    context: "Subscriptions are stored in the billing database. Stripe sends events to POST /stripe/webhook.",
    approach: "Implement HMAC-SHA256 signature verification per Stripe docs. Use idempotency keys to deduplicate events.",
    acceptance: "- Webhook endpoint at POST /stripe/webhook\n- HMAC-SHA256 signature verified per Stripe docs\n- charge.succeeded marks sub as active\n- charge.failed marks sub as past_due, sends email",
  },
  comments: [
    {
      id: "c1",
      author: "ceo",
      isAgent: true,
      body: "Drafted spec based on our chat. Engineer, can you sanity-check Approach?",
      appendedAt: "2026-05-17T10:03:00Z",
    },
    {
      id: "c2",
      author: "engineer",
      isAgent: true,
      body: "Approach looks good but I'd add idempotency via the Stripe Event ID. Want me to spec it?",
      appendedAt: "2026-05-17T10:04:00Z",
    },
    {
      id: "c3",
      author: "human",
      isAgent: false,
      body: "Yes, please add to acceptance criteria.",
      appendedAt: "2026-05-17T10:05:00Z",
    },
  ],
};

const TASK_RUNNING = {
  ...TASK_DRAFTING,
  taskId: "task-002",
  id: "task-002",
  lifecycleState: "running",
  lifecycle_state: "running",
  pipeline_stage: "implement",
  status: "in_progress",
};

const TASK_APPROVED = {
  ...TASK_DRAFTING,
  taskId: "task-003",
  id: "task-003",
  lifecycleState: "approved",
  lifecycle_state: "approved",
  pipeline_stage: "ship",
  status: "done",
};

// ── Helpers ────────────────────────────────────────────────────────────────

async function installIssueMocks(context, tasks = []) {
  await installCommonMocks(context, {
    extra: async (ctx) => {
      // Override the default onboarded=true stub to keep it onboarded.
      // The issues surface is a normal Shell route; no special onboarding state.
      await ctx.route("**/api/tasks**", (r) => {
        const url = r.request().url();
        // /tasks/<id> — single task fetch
        const match = url.match(/\/tasks\/([^?/]+)(\?|$)/);
        if (match) {
          const taskId = decodeURIComponent(match[1]);
          const task = tasks.find((t) => t.id === taskId);
          if (task) {
            return r.fulfill({
              contentType: "application/json",
              body: JSON.stringify(task),
            });
          }
          return r.fulfill({
            status: 404,
            contentType: "application/json",
            body: JSON.stringify({ error: "not found" }),
          });
        }
        // /tasks?... — list
        return r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ tasks }),
        });
      });
    },
  });
}

async function bootIssuesList(page) {
  await page.goto(`${DEFAULT_BASE}/#/issues`, { waitUntil: "load" });
  await page.waitForTimeout(800);
}

async function bootIssueDetail(page, taskId) {
  await page.goto(`${DEFAULT_BASE}/#/issues/${taskId}`, { waitUntil: "load" });
  await page.waitForTimeout(800);
}

// ── Capture ────────────────────────────────────────────────────────────────

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1280, height: 900 },
});

// Frame 01 — Issues list empty state
await installIssueMocks(context, []);
await bootIssuesList(page);
await shotPage(page, OUT, "01-issues-empty-state");

// Frame 02 — Issues list with one open issue
await installIssueMocks(context, [TASK_DRAFTING]);
await bootIssuesList(page);
await shotPage(page, OUT, "02-issues-list-with-open-issue");

// Frame 03 — Issue document Drafting state (full spec visible)
await installIssueMocks(context, [TASK_DRAFTING]);
await bootIssueDetail(page, "task-001");
await shotPage(page, OUT, "03-issue-document-drafting");

// Frame 04 — Issue document Running state (spec auto-collapsed)
await installIssueMocks(context, [TASK_RUNNING]);
// Clear sessionStorage so the collapse default applies fresh.
await page.evaluate(() => {
  try {
    sessionStorage.clear();
  } catch {
    // ignore
  }
});
await bootIssueDetail(page, "task-002");
await shotPage(page, OUT, "04-issue-document-running-collapsed");

// Frame 05 — Issue document Approved state (spec collapsed + Expand affordance)
await installIssueMocks(context, [TASK_APPROVED]);
await page.evaluate(() => {
  try {
    sessionStorage.clear();
  } catch {
    // ignore
  }
});
await bootIssueDetail(page, "task-003");
await shotPage(page, OUT, "05-issue-document-approved-collapsed");

console.log(`captured 5 screenshots to ${OUT}`);
await browser.close();
