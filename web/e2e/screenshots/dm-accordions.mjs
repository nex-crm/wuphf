// Capture before/after of the DM agent workbench accordion fix (PR #748).
//
// The bug surfaced on tight viewports — short laptop screens or any time the
// chat drawer's InterviewBar widened the bottom of the DM view. The four
// workbench accordions (Active tasks, Live stream, Recent activity, Recent
// tasks) shared a flex column with no `flex-shrink: 0`, so a tall sibling
// (the live-stream log fixed at 320px) squeezed sibling headers out of view.
// On top of that, `.dm-chat-drawer` had no internal scroll and capped at
// 38vh — when an InterviewBar opened, the composer got clipped.
//
// This spec captures the same DM page in two states (idle + with a blocking
// approval) under both the old CSS and the new CSS, so a reviewer can scan
// the four screenshots side-by-side. The "before" panes inject the pre-fix
// values via `addStyleTag` on top of the patched stylesheet — same DOM,
// same data, only the rules under test differ.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh dm-accordions 748

import process from "node:process";

import {
  bootShell,
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

// 1280×760 mirrors a typical 13" laptop after the OS chrome is subtracted —
// the viewport size where the bug was reported. 760 leaves the workbench
// pane with the same ~387px the user saw on a 779px window.
const { browser, context, page } = await launchBrowser({
  viewport: { width: 1280, height: 760 },
});

const AGENT_SLUG = "ceo";
const DM_URL = "/#/dm/ceo";

// Members payload mirrors the `/api/office-members` shape consumed by the
// sidebar. We only need enough for the agent rail to render; one member is
// fine because the DM view itself doesn't iterate the whole list.
const MEMBERS = {
  members: [
    {
      slug: AGENT_SLUG,
      name: "CEO",
      role: "CEO",
      created_by: "wuphf",
      created_at: "2026-05-01T10:00:00Z",
      built_in: true,
      provider: {},
      status: "idle",
      activity: "idle",
      online: true,
    },
    {
      slug: "planner",
      name: "Planner",
      role: "planner",
      built_in: false,
      provider: {},
      status: "idle",
      activity: "idle",
      online: false,
    },
  ],
};

// Two open tasks for @ceo so Active tasks renders rows instead of the empty
// state — keeps the section a meaningful height rather than a one-liner.
const TASK_LIST = {
  tasks: [
    {
      id: "task-100",
      title: "Investigate latest broker latency spike",
      status: "in_progress",
      owner: AGENT_SLUG,
      channel: "general",
      created_at: "2026-05-08T09:00:00Z",
      updated_at: "2026-05-09T10:00:00Z",
    },
    {
      id: "task-101",
      title: "Draft Q3 board update",
      status: "open",
      owner: AGENT_SLUG,
      channel: "general",
      created_at: "2026-05-07T15:00:00Z",
      updated_at: "2026-05-08T11:30:00Z",
    },
  ],
};

const AGENT_LOGS = {
  tasks: [
    {
      taskId: "task-100",
      agentSlug: AGENT_SLUG,
      toolCallCount: 4,
      firstToolAt: Date.parse("2026-05-09T09:30:00Z"),
      lastToolAt: Date.parse("2026-05-09T10:00:00Z"),
      hasError: false,
      sizeBytes: 12_400,
    },
  ],
};

const NO_REQUESTS = { requests: [] };

// Single blocking request — exercises the bug path where InterviewBar
// expands the chat drawer past its 38vh cap, clipping the composer.
const BLOCKING_REQUEST = {
  requests: [
    {
      id: "req-1",
      from: AGENT_SLUG,
      question:
        "@ceo wants to conn mod def: gj5en67fz04. Approve?",
      title: "CONN MOD DEF: gj5en67fz04 — [REDACTED] via Notion",
      kind: "external_action",
      blocking: true,
      required: true,
      channel: "general",
      created_at: "2026-05-09T10:05:00Z",
      options: [
        { id: "approve", label: "Approve" },
        { id: "deny", label: "Deny" },
      ],
      context: "Action: [REDACTED] via Notion. Account: [REDACTED]. Channel: #general",
    },
  ],
};

// CSS that reverts the patch in PR #748. Layered on top of the loaded
// stylesheet via `page.addStyleTag` so we can capture the broken state
// without checking out main. Each rule mirrors a removed value from the
// diff and uses `!important` to win specificity.
const PRE_FIX_CSS = `
  .collapsible-section { flex-shrink: 1 !important; }
  .collapsible-section-title {
    overflow: visible !important;
    text-overflow: clip !important;
    white-space: normal !important;
  }
  .dm-chat-drawer { max-height: 38vh !important; }
  .dm-chat-drawer-body { overflow-y: visible !important; }
  .agent-workbench-stream { min-height: 240px !important; }
  .agent-workbench-stream-log {
    height: 320px !important;
    max-height: none !important;
    min-height: 220px !important;
  }
`;

async function installDMMocks(context, { withBlocking = false } = {}) {
  // The patterns are anchored as regexes against the path so a glob like
  // `**/api/tasks*` doesn't accidentally intercept ESM imports vite serves
  // for `/src/api/tasks.ts` — that misroute breaks module loading and the
  // shell never mounts.
  await installCommonMocks(context, {
    extra: async (ctx) => {
      await ctx.route(/\/api\/office-members(\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify(MEMBERS),
        }),
      );
      await ctx.route(/\/api\/members(\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ members: MEMBERS.members }),
        }),
      );
      await ctx.route(/\/api\/channels(\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({
            channels: [{ slug: "general", topic: "" }],
          }),
        }),
      );
      await ctx.route(/\/api\/tasks(\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify(TASK_LIST),
        }),
      );
      await ctx.route(/\/api\/agent-logs(\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify(AGENT_LOGS),
        }),
      );
      await ctx.route(/\/api\/messages(\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ messages: [] }),
        }),
      );
      await ctx.route(/\/api\/requests(\?|$)/, (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify(withBlocking ? BLOCKING_REQUEST : NO_REQUESTS),
        }),
      );
    },
  });
}

async function captureState({ name, withBlocking, preFix }) {
  const base = process.env.BASE_URL ?? "http://localhost:5273";
  await installDMMocks(context, { withBlocking });
  // Navigate to about:blank first so the new state forces a full re-mount.
  // Without this, page.goto to the same hash route is treated as a no-op
  // by react-router and the previous state lingers — every screenshot ends
  // up identical.
  await page.goto("about:blank");
  await page.goto(`${base}/${DM_URL}`, { waitUntil: "load" });
  await page.waitForSelector(".status-bar", { timeout: 15_000 });
  await page.evaluate(async () => {
    const m = await import("/src/stores/app.ts");
    m.useAppStore.setState({
      brokerConnected: true,
      onboardingComplete: true,
    });
  });
  await page.waitForSelector('[data-testid="dm-workbench"]', {
    timeout: 10_000,
  });
  if (preFix) {
    await page.addStyleTag({ content: PRE_FIX_CSS });
  }
  // Let layout settle (live stream connects, accordion gap reflows). 800ms
  // is generous but cheap — each spec runs once per PR. Long enough for
  // the InterviewBar query to land and re-render the drawer.
  await page.waitForTimeout(800);
  // Frame on the DMView (workbench + drawer) so the screenshot focuses on
  // the area the fix touches and the sidebar's variable agent count
  // doesn't muddy the diff.
  await shotElement(page, '[data-testid="dm-workbench"]', OUT, name);
}

await captureState({
  name: "01-before-idle",
  withBlocking: false,
  preFix: true,
});
await captureState({
  name: "02-after-idle",
  withBlocking: false,
  preFix: false,
});
await captureState({
  name: "03-before-with-interview",
  withBlocking: true,
  preFix: true,
});
await captureState({
  name: "04-after-with-interview",
  withBlocking: true,
  preFix: false,
});

console.log(`captured 4 screenshots to ${OUT}`);
await browser.close();
