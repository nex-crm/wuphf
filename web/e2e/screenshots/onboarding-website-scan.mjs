// Capture the restored website-scan onboarding flow:
//   website-url form field → scanning chip → done chip → article reveal.
//
// Stubs /onboarding/state per frame so no real broker is needed.
// Pattern mirrors onboarding-phase2.mjs.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh onboarding-website-scan <pr-number>

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

// Shared baseline form_answers — the user has already answered company_name
// and description; what changes between frames is the pending suggestion and
// any chat messages on the CEO DM.
const baseAnswers = {
  company_name: "Acme Billing",
  description: "Subscription billing for indie SaaS",
};

// Frame 1: PhaseWebsite — the restored "Got a website I can scan for context?"
// card. This is the question that disappeared in the chat-mode redesign.
const STATE_WEBSITE_PROMPT = {
  onboarded: false,
  phase: "website",
  form_answers: baseAnswers,
  pending_suggestion: {
    id: "identity-website",
    phase: "website",
    kind: "ceo_form_field",
    payload: {
      field: "website_url",
      label: "Company website",
      placeholder: "acme.com",
      optional: true,
    },
  },
};

// Frame 2: PhaseScan — chip showing the scan is in progress. Status updates
// arrive via SSE in production; for screenshot purposes we render the chip
// in its initial "scanning" state directly from /onboarding/state.
const STATE_SCAN_IN_PROGRESS = {
  onboarded: false,
  phase: "scan",
  form_answers: {
    ...baseAnswers,
    website_url: "https://anthropic.com",
  },
  pending_suggestion: {
    id: "scan-progress-anthropic-com",
    phase: "scan",
    kind: "ceo_scan_chip",
    payload: {
      url: "https://anthropic.com",
      status: "scanning",
      scanning_label: "Scanning anthropic.com…",
    },
  },
};

// Frame 3: PhaseScan terminal — chip flips to "done" and the per-article
// reveal lines have been posted as separate CEO bubbles. In production these
// arrive via SSE; we mock them on the messages endpoint so the chat shows
// the staggered reveal in its final state.
const STATE_SCAN_DONE = {
  onboarded: false,
  // After the goroutine finishes it auto-advances to blueprint, so the
  // pending_suggestion for the dom is the blueprint chip_row. But the chat
  // history above it shows the reveal sequence.
  phase: "blueprint",
  form_answers: {
    ...baseAnswers,
    website_url: "https://anthropic.com",
    scan_complete: true,
  },
  pending_suggestion: {
    id: "blueprint-pick",
    phase: "blueprint",
    kind: "ceo_chip_row",
    payload: {
      field: "blueprint_id",
      label: "Pick a starter template, or start from scratch:",
      options: [
        { id: "bookkeeping-invoicing-service", label: "Bookkeeping" },
        { id: "niche-crm", label: "Niche CRM" },
        { id: "youtube-factory", label: "YouTube Factory" },
        { id: "", label: "Start from scratch" },
      ],
    },
  },
};

// Messages on the CEO DM for the reveal frame — the staggered "✓ <article>"
// lines that replace the old Step3bAnalysis reveal animation.
const dmSlug = "dm:ceo:onboarding";
const SCAN_REVEAL_MESSAGES = [
  {
    id: "msg-1",
    from: "ceo",
    channel: dmSlug,
    kind: "text",
    content: "Office name?",
    timestamp: "2026-05-18T12:43:00Z",
  },
  {
    id: "msg-2",
    from: "ceo",
    channel: dmSlug,
    kind: "text",
    content: "What does Acme Billing do?",
    timestamp: "2026-05-18T12:43:05Z",
  },
  {
    id: "msg-3",
    from: "ceo",
    channel: dmSlug,
    kind: "text",
    content: "Got a website I can scan for context?",
    timestamp: "2026-05-18T12:43:10Z",
  },
  {
    id: "msg-4",
    from: "ceo",
    channel: dmSlug,
    kind: "ceo_scan_chip",
    content: "Scanning anthropic.com…",
    payload: { url: "https://anthropic.com", status: "scanning" },
    timestamp: "2026-05-18T12:43:12Z",
  },
  {
    id: "msg-5",
    from: "ceo",
    channel: dmSlug,
    kind: "ceo_scan_chip",
    content: "Wiki updated ✓",
    payload: {
      url: "https://anthropic.com",
      status: "done",
      done_label: "Wiki updated ✓",
    },
    timestamp: "2026-05-18T12:43:38Z",
  },
  {
    id: "msg-6",
    from: "ceo",
    channel: dmSlug,
    kind: "text",
    content: "✓ team/about/company.md",
    timestamp: "2026-05-18T12:43:39Z",
  },
];

async function installFrameMocks(context, stateFixture, opts = {}) {
  await installCommonMocks(context, {
    extra: async (ctx) => {
      await ctx.route("**/api/onboarding/state", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify(stateFixture),
        }),
      );
      await ctx.route("**/api/onboarding/answer", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ ok: true }),
        }),
      );
      await ctx.route("**/api/onboarding/transition", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ ok: true, phase: stateFixture.phase }),
        }),
      );
      if (opts.messages) {
        await ctx.route(`**/api/messages?channel=${encodeURIComponent(dmSlug)}**`, (r) =>
          r.fulfill({
            contentType: "application/json",
            body: JSON.stringify({ messages: opts.messages }),
          }),
        );
        await ctx.route("**/api/messages**", (r) =>
          r.fulfill({
            contentType: "application/json",
            body: JSON.stringify({ messages: opts.messages }),
          }),
        );
      }
    },
  });
}

async function bootFrame(page) {
  await page.addInitScript(() => {
    try {
      window.localStorage.setItem("wuphf:onboarding-v2", "1");
      window.localStorage.setItem("wuphf:in-ceo-onboarding", "1");
    } catch {
      // private-mode tabs; the query param fallback handles it.
    }
  });
  await page.goto(`${DEFAULT_BASE}/?onboardingV2=1`, { waitUntil: "load" });
  await page.waitForSelector('[data-testid="onboarding-dm-route"]', {
    timeout: 15_000,
  });
  await page.waitForTimeout(400);
}

const { browser, context, page } = await launchBrowser({
  viewport: { width: 1280, height: 800 },
});

try {
  // Frame 1: the website prompt that the chat-mode redesign accidentally dropped.
  await installFrameMocks(context, STATE_WEBSITE_PROMPT);
  await bootFrame(page);
  await shotPage(page, OUT, "01-website-prompt");

  // Frame 2: scan running.
  await installFrameMocks(context, STATE_SCAN_IN_PROGRESS);
  await bootFrame(page);
  await shotPage(page, OUT, "02-scan-in-progress");

  // Frame 3: post-scan reveal — done chip + per-article CEO bubble.
  await installFrameMocks(context, STATE_SCAN_DONE, {
    messages: SCAN_REVEAL_MESSAGES,
  });
  await bootFrame(page);
  await shotPage(page, OUT, "03-scan-reveal");

  console.log(`captured 3 screenshots to ${OUT}`);
} finally {
  // Cleanup runs even if any frame throws — otherwise CI leaks a chromium
  // child process and the next run picks up a stale port (CodeRabbit on #911).
  await browser.close();
}
