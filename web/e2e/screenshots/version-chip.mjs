// Capture the seven canned states for the StatusBar version chip + modal.
// Worked example for web/e2e/screenshots/lib.mjs — copy this file when
// adding screenshot specs for a new feature.
//
// Run via the wrapper:
//   web/e2e/screenshots/publish.sh version-chip <pr-number>
//
// The wrapper boots vite, sets WUPHF_SCREENSHOTS_OUT, runs this file,
// pushes the PNGs to `screenshots/pr-<n>` (orphan branch), and embeds
// raw URLs into the PR body.

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

const UP_TO_DATE = {
  current: "0.83.5",
  latest: "0.83.5",
  upgrade_available: false,
  is_dev_build: false,
  upgrade_command: "npm install -g wuphf@latest",
  install_method: "global",
  install_command: "npm install -g wuphf@latest",
};

const OUTDATED = {
  current: "0.83.5",
  latest: "0.84.0",
  upgrade_available: true,
  is_dev_build: false,
  upgrade_command: "npm install -g wuphf@latest",
  install_method: "global",
  install_command: "npm install -g wuphf@latest",
  compare_url: "https://github.com/nex-crm/wuphf/compare/v0.83.5...v0.84.0",
};

const { browser, context, page } = await launchBrowser();

async function shotModal(name) {
  // Capture the whole overlay so the dimmed backdrop frames the card.
  await page.locator(".help-overlay .version-modal").waitFor({
    state: "visible",
    timeout: 5_000,
  });
  await shotPage(page, OUT, name);
}

// 1. Status bar — up to date (green dot).
await installCommonMocks(context, { upgradeCheck: UP_TO_DATE });
await bootShell(page, { afterFlipSelector: ".status-bar-version" });
await shotElement(page, ".status-bar", OUT, "01-statusbar-up-to-date");

// 2. Status bar — update available (amber dot).
await installCommonMocks(context, { upgradeCheck: OUTDATED });
await bootShell(page, { afterFlipSelector: ".status-bar-version" });
await shotElement(page, ".status-bar", OUT, "02-statusbar-update-available");

// 3. Modal — up to date (default actions visible).
await installCommonMocks(context, { upgradeCheck: UP_TO_DATE });
await bootShell(page, { afterFlipSelector: ".status-bar-version" });
await page.locator(".status-bar-version").click();
await shotModal("03-modal-up-to-date");
await page.keyboard.press("Escape");

// 4. Modal — update available.
await installCommonMocks(context, { upgradeCheck: OUTDATED });
await bootShell(page, { afterFlipSelector: ".status-bar-version" });
await page.locator(".status-bar-version").click();
await shotModal("04-modal-update-available");
await page.keyboard.press("Escape");

// 5. Modal — install complete (success path).
await installCommonMocks(context, {
  upgradeCheck: OUTDATED,
  upgradeRun: {
    ok: true,
    install_method: "global",
    command: "npm install -g wuphf@latest",
    output: "added 1 package, removed 1 package, and changed 0 packages in 6s",
  },
});
await bootShell(page, { afterFlipSelector: ".status-bar-version" });
await page.locator(".status-bar-version").click();
await page.locator(".version-modal").waitFor();
await page.locator("button:has-text('Force update')").click();
await page
  .getByRole("heading", { name: "Install complete" })
  .waitFor({ timeout: 10_000 });
await shotModal("05-modal-install-complete");
await page.keyboard.press("Escape");

// 6. Modal — install failed (with retry command).
await installCommonMocks(context, {
  upgradeCheck: OUTDATED,
  upgradeRun: {
    ok: false,
    install_method: "global",
    command: "npm install -g wuphf@latest",
    error:
      "EACCES: permission denied, mkdir '/usr/local/lib/node_modules/wuphf'",
    output:
      "npm error code EACCES\nnpm error syscall mkdir\nnpm error path /usr/local/lib/node_modules/wuphf\nnpm error errno -13\n",
  },
});
await bootShell(page, { afterFlipSelector: ".status-bar-version" });
await page.locator(".status-bar-version").click();
await page.locator(".version-modal").waitFor();
await page.locator("button:has-text('Force update')").click();
await page
  .getByRole("heading", { name: "Install failed" })
  .waitFor({ timeout: 10_000 });
await shotModal("06-modal-install-failed");
await page.keyboard.press("Escape");

// 7. Modal — restart broker error inline.
await installCommonMocks(context, {
  upgradeCheck: UP_TO_DATE,
  brokerRestart: async (r) =>
    r.fulfill({ status: 502, body: "broker socket closed" }),
});
await bootShell(page, { afterFlipSelector: ".status-bar-version" });
await page.locator(".status-bar-version").click();
await page.locator(".version-modal").waitFor();
await page.locator("button:has-text('Restart broker')").click();
await page
  .locator("text=Couldn't restart broker")
  .waitFor({ timeout: 10_000 });
await shotModal("07-modal-restart-error");

console.log(`captured 7 screenshots to ${OUT}`);
await browser.close();
