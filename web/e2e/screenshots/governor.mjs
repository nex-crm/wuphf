// Capture the Governor surfaces: the live run-control meter in the status bar
// and the paused review banner (budget, turns, stopped).
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh governor <pr-number>

import process from "node:process";

import {
  bootShell,
  installCommonMocks,
  launchBrowser,
  shotElement,
} from "./lib.mjs";

const OUT = process.env.WUPHF_SCREENSHOTS_OUT;
if (!OUT) {
  console.error("WUPHF_SCREENSHOTS_OUT is not set; run via publish.sh");
  process.exit(2);
}

const base = {
  maxTokens: 150_000,
  maxCostUsd: 3,
  maxTurns: 12,
  disabled: false,
};

const RUNNING = {
  ...base,
  paused: false,
  reason: "",
  turnsSinceCheckpoint: 7,
  tokensSinceCheckpoint: 92_000,
  costSinceCheckpoint: 1.34,
};

const PAUSED_BUDGET = {
  ...base,
  paused: true,
  reason: "budget",
  pausedAt: "2026-06-26T00:00:00Z",
  turnsSinceCheckpoint: 9,
  tokensSinceCheckpoint: 152_000,
  costSinceCheckpoint: 3.12,
};

const PAUSED_TURNS = {
  ...base,
  paused: true,
  reason: "turns",
  pausedAt: "2026-06-26T00:00:00Z",
  turnsSinceCheckpoint: 12,
  tokensSinceCheckpoint: 64_000,
  costSinceCheckpoint: 0.88,
};

const STOPPED = {
  ...base,
  paused: true,
  reason: "stop",
  pausedAt: "2026-06-26T00:00:00Z",
  turnsSinceCheckpoint: 4,
  tokensSinceCheckpoint: 38_000,
  costSinceCheckpoint: 0.51,
};

function governorMock(status) {
  return async (context) => {
    await context.route("**/api/governor", (r) =>
      r.fulfill({
        contentType: "application/json",
        body: JSON.stringify(status),
      }),
    );
  };
}

const { browser, context, page } = await launchBrowser();

// 1. Status bar — live run-control meter (turns · tokens · cost + Pause/Stop).
await installCommonMocks(context, { extra: governorMock(RUNNING) });
await bootShell(page, { afterFlipSelector: ".governor-control" });
await shotElement(page, ".status-bar", OUT, "01-statusbar-running-meter");

// 2. Paused — budget checkpoint banner.
await installCommonMocks(context, { extra: governorMock(PAUSED_BUDGET) });
await bootShell(page, { afterFlipSelector: ".governor-banner" });
await shotElement(page, ".governor-banner", OUT, "02-banner-budget");

// 3. Paused — turn-count checkpoint banner.
await installCommonMocks(context, { extra: governorMock(PAUSED_TURNS) });
await bootShell(page, { afterFlipSelector: ".governor-banner" });
await shotElement(page, ".governor-banner", OUT, "03-banner-turns");

// 4. Stopped — single Resume action.
await installCommonMocks(context, { extra: governorMock(STOPPED) });
await bootShell(page, { afterFlipSelector: ".governor-banner" });
await shotElement(page, ".governor-banner", OUT, "04-banner-stopped");

console.log(`captured 4 screenshots to ${OUT}`);
await browser.close();
