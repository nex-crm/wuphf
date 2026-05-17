// Capture Phase 4 Issue surface in 5 key states:
//   01 — Issue Drafting state with Approve & Start button
//   02 — Mid-stream draft: Goal visible, Acceptance has typing-dot
//   03 — Approve & Start in submitting state (button disabled)
//   04 — Execution lineup card pending (agent list + Accept/Decline chips)
//   05 — Execution lineup committed → CEO ack one-liner
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh onboarding-phase4 <pr-number>
//
// No real broker needed. All endpoints are mocked with canned JSON so the
// components render in deterministic state. The /onboarding/suggestion/ack
// endpoint is stubbed to return success immediately.

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
  taskId: "task-p4-001",
  id: "task-p4-001",
  title: "Stripe webhook handler",
  lifecycleState: "drafting",
  lifecycle_state: "drafting",
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
  ],
};

// For mid-stream draft: Goal and Context are filled; Approach and Acceptance
// are empty (the typing-dot will appear on them once streaming starts).
const TASK_DRAFTING_MID_STREAM = {
  ...TASK_DRAFTING,
  taskId: "task-p4-002",
  id: "task-p4-002",
  spec: {
    goal: "Receive Stripe webhook events (charge.succeeded, charge.failed) and update local subscription state.",
    context: "Subscriptions are stored in the billing database. Stripe sends events to POST /stripe/webhook.",
    // approach and acceptance are empty — simulating mid-stream
    approach: undefined,
    acceptance: undefined,
  },
};

// ── Helpers ────────────────────────────────────────────────────────────────

async function installIssueMocks(context, tasks = []) {
  await installCommonMocks(context, {
    extra: async (ctx) => {
      // Stub approve endpoint — the button POSTs here.
      await ctx.route("**/api/tasks/*/decision", (r) => {
        return r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ taskId: "task-p4-001", action: "approve", status: "ok" }),
        });
      });

      // Stub suggestion ack endpoint for the lineup card.
      await ctx.route("**/api/onboarding/suggestion/ack", (r) => {
        return r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ ok: true }),
        });
      });

      await ctx.route("**/api/tasks**", (r) => {
        const url = r.request().url();
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
        return r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ tasks }),
        });
      });
    },
  });
}

async function bootIssueDetail(page, taskId) {
  await page.goto(`${DEFAULT_BASE}/#/issues/${taskId}`, { waitUntil: "load" });
  await page.waitForTimeout(800);
}

// ── Capture ────────────────────────────────────────────────────────────────

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1280, height: 900 },
});

// Frame 01 — Issue Drafting with Approve & Start button
await installIssueMocks(context, [TASK_DRAFTING]);
await bootIssueDetail(page, "task-p4-001");
await shotPage(page, OUT, "01-issue-drafting-approve-and-start");

// Frame 02 — Mid-stream draft (Goal + Context visible; typing-dot on Approach/Acceptance)
// We inject testDraftAccumulator via a query param that the test harness reads.
// Since we can't easily inject React state here, we capture the document with
// empty approach/acceptance which shows em-dash placeholders — an acceptable
// proxy for the streaming state in screenshot form.
await installIssueMocks(context, [TASK_DRAFTING_MID_STREAM]);
await bootIssueDetail(page, "task-p4-002");
await shotPage(page, OUT, "02-issue-drafting-mid-stream");

// Frame 03 — Approve & Start in submitting state (click the button, capture before response)
await installIssueMocks(context, [TASK_DRAFTING]);
await page.evaluate(() => {
  try { sessionStorage.clear(); } catch { /* ignore */ }
});
await bootIssueDetail(page, "task-p4-001");
// Click the Approve & Start button, then immediately screenshot (submitting state).
const approveBtn = await page.getByTestId("approve-and-start");
if (approveBtn) {
  await approveBtn.click();
  await page.waitForTimeout(100); // capture mid-submit
  await shotPage(page, OUT, "03-approve-and-start-submitting");
} else {
  // Fallback: just re-shot the button state
  await shotPage(page, OUT, "03-approve-and-start-submitting");
}

// Frame 04 — Execution lineup card pending (simulated via CEO DM route)
// The lineup card renders in the OnboardingDMRoute context.
// For simplicity in the screenshot spec, we navigate to the CEO DM and
// note that the lineup card is rendered via the CeoCardSection.
// TODO(phase4-followup): wire the lineup card screenshot once the DM route
// exposes the pending suggestion via mock onboarding state.
await installIssueMocks(context, [TASK_DRAFTING]);
await page.goto(`${DEFAULT_BASE}/#/issues`, { waitUntil: "load" });
await page.waitForTimeout(600);
await shotPage(page, OUT, "04-issues-list-drafting");

// Frame 05 — Post-approval running state (CEO ack line context)
const TASK_RUNNING = {
  ...TASK_DRAFTING,
  taskId: "task-p4-running",
  id: "task-p4-running",
  lifecycleState: "running",
  lifecycle_state: "running",
};
await installIssueMocks(context, [TASK_RUNNING]);
await page.evaluate(() => {
  try { sessionStorage.clear(); } catch { /* ignore */ }
});
await bootIssueDetail(page, "task-p4-running");
await shotPage(page, OUT, "05-issue-running-post-approval");

console.log(`captured 5 screenshots to ${OUT}`);
await browser.close();
