// Capture screenshots of the Decision Inbox + Decision Packet
// (Lane G of the multi-agent control loop) in their populated states.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh multi-agent-control-loop <pr-number>

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

const { browser, context, page, errors } = await launchBrowser({
  viewport: { width: 1480, height: 980 },
});

await installCommonMocks(context);

// 1. Inbox — populated, three rows needing decision visible.
await page.goto(`${process.env.BASE_URL ?? "http://localhost:5273"}/#/inbox`, {
  waitUntil: "load",
});
await page.waitForSelector(".status-bar", { timeout: 15_000 });
await page.evaluate(async () => {
  const m = await import("/src/stores/app.ts");
  m.useAppStore.setState({
    brokerConnected: true,
    onboardingComplete: true,
  });
});
await page.waitForSelector('[data-testid="decision-inbox"]', {
  timeout: 10_000,
});
// Wait for the populated row layout to settle.
await page.waitForSelector(".inbox-row", { timeout: 10_000 });
await page.waitForTimeout(400);
await shotPage(page, OUT, "01-inbox-populated");
await shotElement(page, ".inbox-main", OUT, "02-inbox-rows");

// 2. Decision Packet — populated, with the locked 3-column layout.
await page.goto(
  `${process.env.BASE_URL ?? "http://localhost:5273"}/#/task/task-2741`,
  { waitUntil: "load" },
);
await page.waitForSelector(".packet-shell", { timeout: 10_000 });
await page.waitForSelector(".packet-grade", { timeout: 10_000 });
await page.waitForTimeout(400);
await shotPage(page, OUT, "03-packet-populated");
await shotElement(page, ".packet-center", OUT, "04-packet-center");
await shotElement(page, ".packet-right", OUT, "05-packet-actions");

console.log(`captured 5 screenshots to ${OUT}`);
if (errors.length > 0) {
  console.error("page errors during capture:", errors);
}
await browser.close();
