// Capture the Routines list before/after the one-shot-watchdog filter fix.
//
// The broker enqueues one-shot task_follow_up / request_follow_up / recheck
// jobs on every task-lifecycle transition. They ride the wire with
// interval_minutes: 0, and the old classifier treated `typeof 0 === "number"`
// as a cadence, so they flooded the Routines list. This spec seeds a realistic
// /scheduler payload (2 real routines + 1 system cron + 4 one-shot watchdogs)
// and shoots the Routines surface.
//
// Run order is controlled by the wrapper / driver, which sets the code state
// (fixed vs reverted) and passes WUPHF_SHOT_LABEL so the before/after PNGs
// land side by side in one orphan branch:
//
//   WUPHF_SHOT_LABEL=01-before  (classifier reverted to origin/main)
//   WUPHF_SHOT_LABEL=02-after   (classifier fixed)

import process from "node:process";

import {
  flipStore,
  installCommonMocks,
  launchBrowser,
  shotElement,
} from "./lib.mjs";

const BASE = process.env.BASE_URL ?? "http://localhost:5273";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh / driver");
  process.exit(2);
}
const LABEL = process.env.WUPHF_SHOT_LABEL ?? "routines";

// Anchor everything to a fixed clock so the seeded next_run / due_at values
// are stable across runs (no Date.now() drift between before and after).
const NOW = Date.parse("2026-06-01T08:00:00Z");
const inMin = (m) => new Date(NOW + m * 60_000).toISOString();
const inHrs = (h) => new Date(NOW + h * 3_600_000).toISOString();

// Two genuine recurring routines — these SHOULD always appear.
const RECURRING = [
  {
    slug: "rita-standup-digest",
    label: "Morning standup digest",
    kind: "agent",
    target_type: "agent",
    target_id: "rita",
    interval_minutes: 1440,
    enabled: true,
    next_run: inHrs(25),
    last_run: inHrs(-23),
    last_run_status: "ok",
  },
  {
    slug: "workflow:weekly-pipeline-report",
    label: "Weekly pipeline report",
    kind: "workflow",
    target_type: "workflow",
    schedule_expr: "0 9 * * 1",
    interval_minutes: 0,
    enabled: true,
    next_run: inHrs(73),
    last_run: inHrs(-95),
    last_run_status: "ok",
  },
];

// One broker-managed system cron — hidden behind the "Show system" toggle in
// both before and after (its visibility is governed by system_managed, not by
// the bug).
const SYSTEM = [
  {
    slug: "nex-insights",
    label: "Nex insights",
    kind: "cron",
    interval_minutes: 30,
    enabled: true,
    system_managed: true,
    next_run: inMin(30),
    last_run: inMin(-30),
    last_run_status: "ok",
  },
];

// Four one-shot watchdog jobs the broker auto-enqueues per task transition.
// interval_minutes: 0, no cron, not system-managed — pure plumbing. These are
// the rows that cluttered the list before the fix.
const WATCHDOGS = [
  {
    slug: "task_follow_up:general:task-101",
    label: "Follow up on Draft the Q3 launch brief",
    kind: "task_follow_up",
    target_type: "task",
    target_id: "task-101",
    interval_minutes: 0,
    due_at: inMin(45),
    next_run: inMin(45),
    status: "scheduled",
  },
  {
    slug: "recheck:general:task:task-102",
    label: "Recheck task Reconcile the billing export",
    kind: "recheck",
    target_type: "task",
    target_id: "task-102",
    interval_minutes: 0,
    due_at: inHrs(2),
    next_run: inHrs(2),
    status: "scheduled",
  },
  {
    slug: "task_follow_up:general:task-103",
    label: "Follow up on Sync the CRM contacts",
    kind: "task_follow_up",
    target_type: "task",
    target_id: "task-103",
    interval_minutes: 0,
    due_at: inMin(90),
    next_run: inMin(90),
    status: "scheduled",
  },
  {
    slug: "request_follow_up:general:req-7",
    label: "Follow up on access request from Priya",
    kind: "request_follow_up",
    target_type: "request",
    target_id: "req-7",
    interval_minutes: 0,
    due_at: inHrs(3),
    next_run: inHrs(3),
    status: "scheduled",
  },
];

const JOBS = [...RECURRING, ...SYSTEM, ...WATCHDOGS];

const { browser, context, page } = await launchBrowser();

await installCommonMocks(context, {
  extra: async (ctx) => {
    // GET /api/scheduler (+ optional ?due_only). The single `*` matches the
    // query string but not the /system-specs or /{slug}/runs subpaths.
    await ctx.route("**/api/scheduler", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ jobs: JOBS }),
      }),
    );
    await ctx.route("**/api/scheduler?*", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ jobs: JOBS }),
      }),
    );
  },
});

// Boot the Shell, flip the store connected, then route to Routines. The app
// uses hash routing, so deep-linking is a location.hash change after boot
// (the connect path lands on the CEO chat first). The list view + hidden
// system routines are the defaults for a fresh browser context (empty
// localStorage) — exactly what a user lands on.
await page.goto(`${BASE}/`, { waitUntil: "load" });
await page.waitForSelector(".status-bar", { timeout: 15_000 });
await flipStore(page);
await page.waitForTimeout(500);
await page.evaluate(() => {
  window.location.hash = "#/routines";
});
await page.waitForSelector('[data-testid="routines-app"]', { timeout: 15_000 });
await page.waitForSelector('[data-testid="routines-title"]', {
  timeout: 10_000,
});
// Let the 15s-interval query settle and the list paint.
await page.waitForTimeout(700);

await shotElement(page, '[data-testid="routines-app"]', OUT, `${LABEL}-routines-list`);

await browser.close();
