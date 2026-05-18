// Capture the resizable sidebar + thread panel: default narrow widths,
// drag-resize states, and the new resize handle affordance.
//
// Run via:
//   web/e2e/screenshots/publish.sh resize-panes <pr-number>

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

const { browser, context, page } = await launchBrowser();
await installCommonMocks(context);

// 1. Default narrower sidebar (220px instead of the prior 260px).
await bootShell(page, { afterFlipSelector: ".sidebar" });
await shotPage(page, OUT, "01-sidebar-default-width");

// 2. Sidebar widened to ~360px via dragging the handle.
const sidebar = page.locator(".sidebar");
const sidebarBox = await sidebar.boundingBox();
if (sidebarBox) {
  const handleX = sidebarBox.x + sidebarBox.width - 1;
  const handleY = sidebarBox.y + sidebarBox.height / 2;
  await page.mouse.move(handleX, handleY);
  await page.mouse.down();
  await page.mouse.move(handleX + 140, handleY, { steps: 12 });
  await page.mouse.up();
}
await shotPage(page, OUT, "02-sidebar-dragged-wider");

// 3. Sidebar shrunk to the floor (~180px).
const sidebarBox2 = await sidebar.boundingBox();
if (sidebarBox2) {
  const handleX = sidebarBox2.x + sidebarBox2.width - 1;
  const handleY = sidebarBox2.y + sidebarBox2.height / 2;
  await page.mouse.move(handleX, handleY);
  await page.mouse.down();
  await page.mouse.move(handleX - 300, handleY, { steps: 12 });
  await page.mouse.up();
}
await shotElement(page, ".sidebar", OUT, "03-sidebar-min-width");

// 4. Close-up of the resize handle on hover.
await sidebar.evaluate((el) => {
  const handle = el.querySelector(".pane-resize-handle");
  if (handle instanceof HTMLElement) handle.focus();
});
await shotElement(page, ".sidebar", OUT, "04-sidebar-handle-focused");

await browser.close();
