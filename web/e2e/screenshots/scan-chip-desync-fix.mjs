// Capture the scan-chip → blueprint chip-row swap that fixes #936.
//
// Two frames:
//   01-stranded — what users saw BEFORE the fix: scanning chip pinned in the
//                 composer slot with no buttons. Reproduced by stubbing
//                 /onboarding/state to return the scan chip as the pending
//                 suggestion even though the broker has moved on.
//   02-recovered — what users see AFTER the fix lands: the blueprint chip-row
//                  swaps into the slot automatically (no hard reload),
//                  surfacing the Bookkeeping / Niche CRM / scratch options.
//
// The two frames use the same mock backend layer; only the
// /onboarding/state response differs between mounts, which is exactly what
// the SSE-driven onboarding-state invalidate (useBrokerEvents.ts) causes
// in production.

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

const baseAnswers = {
  company_name: "Acme Billing",
  description: "Subscription billing for indie SaaS",
  website_url: "https://example.com",
};

// Frame 1 — the broken state: scan chip pinned with no buttons.
const STATE_STRANDED = {
  onboarded: false,
  phase: "scan",
  form_answers: baseAnswers,
  pending_suggestion: {
    id: "scan-progress-example-com",
    phase: "scan",
    kind: "ceo_scan_chip",
    payload: {
      url: "https://example.com",
      status: "scanning",
      scanning_label: "Scanning example.com…",
    },
  },
};

// Frame 2 — the recovered state: blueprint chip-row swaps in automatically.
const STATE_RECOVERED = {
  onboarded: false,
  phase: "blueprint",
  form_answers: { ...baseAnswers, scan_complete: false },
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

// Chat history shared between frames: the user submitted example.com, the
// broker emitted the scanning chip, then the scan failed.
const dmSlug = "dm:ceo:onboarding";
const FAILED_SCAN_MESSAGES = [
  {
    id: "msg-1",
    from: "ceo",
    channel: dmSlug,
    kind: "text",
    content: "Got a website I can scan for context?",
    timestamp: "2026-05-20T12:43:10Z",
  },
  {
    id: "msg-2",
    from: "human",
    channel: dmSlug,
    kind: "text",
    content: "https://example.com",
    timestamp: "2026-05-20T12:43:14Z",
  },
  {
    id: "msg-3",
    from: "ceo",
    channel: dmSlug,
    kind: "ceo_scan_chip",
    content: "Scanning example.com…",
    payload: { url: "https://example.com", status: "scanning" },
    timestamp: "2026-05-20T12:43:15Z",
  },
  {
    id: "msg-4",
    from: "ceo",
    channel: dmSlug,
    kind: "ceo_scan_chip",
    content: "Couldn't read that URL",
    payload: {
      url: "https://example.com",
      status: "failed",
      failed_label: "Couldn't read that URL",
    },
    timestamp: "2026-05-20T12:43:18Z",
  },
];

async function installFrameMocks(context, stateFixture, messages) {
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
      await ctx.route(
        `**/api/messages?channel=${encodeURIComponent(dmSlug)}**`,
        (r) =>
          r.fulfill({
            contentType: "application/json",
            body: JSON.stringify({ messages }),
          }),
      );
      await ctx.route("**/api/messages**", (r) =>
        r.fulfill({
          contentType: "application/json",
          body: JSON.stringify({ messages }),
        }),
      );
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
  await installFrameMocks(context, STATE_STRANDED, FAILED_SCAN_MESSAGES);
  await bootFrame(page);
  await shotPage(page, OUT, "01-stranded-before-fix");

  await installFrameMocks(context, STATE_RECOVERED, FAILED_SCAN_MESSAGES);
  await bootFrame(page);
  await shotPage(page, OUT, "02-recovered-after-fix");

  console.log(`captured 2 screenshots to ${OUT}`);
} finally {
  await browser.close();
}
