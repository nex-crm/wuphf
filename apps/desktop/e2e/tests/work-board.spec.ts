import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { _electron as electron, expect, test } from "@playwright/test";

const desktopRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const mainEntry = resolve(desktopRoot, "out/main/index.js");
const rendererDist = resolve(desktopRoot, "out/renderer");

// Proves the work-board route boots end-to-end. Navigates to
// `/#/threads` after the renderer mounts, asserts each of the four
// kanban columns renders, and verifies the empty-state copy that an
// empty broker produces.
//
// The board partitioning + thread cards are exercised by unit tests
// against fixtures; this spec is the integration check that the route
// is wired up, the api client reaches the broker, and the protocol
// codec accepts the live `ThreadListResponse` shape (which would catch
// a wire/contract drift the unit tests cannot — they always speak the
// fixture's snake_case shape).
//
// CURRENTLY BLOCKED: the desktop's `broker-entry-runtime` does not yet
// wire the threads subsystem into `createBroker`, so `GET
// /api/v1/threads` returns 404 in the running app. Threads need a
// shared SQLite handle (`createThreadSubsystem(db, eventLog,
// receiptStore)`), which is blocked by the better-sqlite3 ↔ Electron
// 42 ABI gap tracked in commit 2b3df9b4 / PR #923. When that wiring
// lands (its own focused PR), this spec flips from `fixme` back to
// `test`.
test.fixme(
  "work-board route mounts and renders the four kanban columns",
  async () => {
    const app = await electron.launch({
      args: [mainEntry],
      env: {
        ...process.env,
        WUPHF_RECEIPT_STORE_PATH: "",
        WUPHF_WEBAUTHN_STORE_PATH: "",
        WUPHF_RENDERER_DIST: rendererDist,
      },
    });

    try {
      const page = await app.firstWindow();
      await expect(page.getByText("Ready", { exact: true }).first()).toBeVisible({
        timeout: 20_000,
      });

      // Hash-router navigation. Evaluated in the browser context so
      // `globalThis` is the DOM Window with `location`, not Playwright's
      // Page type.
      await page.evaluate(() => {
        globalThis.location.hash = "#/threads";
      });

      await expect(page.getByRole("region", { name: /needs me/i })).toBeVisible();
      await expect(page.getByRole("region", { name: /running/i })).toBeVisible();
      await expect(page.getByRole("region", { name: /review/i })).toBeVisible();
      await expect(page.getByRole("region", { name: /done/i })).toBeVisible();

      await expect(page.getByText(/Nothing waiting on you/i)).toBeVisible();
      await expect(page.getByText("0 threads")).toBeVisible();
    } finally {
      await app.close();
    }
  },
);
