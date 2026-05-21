import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { _electron as electron, expect, test } from "@playwright/test";

const desktopRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const mainEntry = resolve(desktopRoot, "out/main/index.js");
const rendererDist = resolve(desktopRoot, "out/renderer");

// Proves the packaged renderer reaches the SQLite-backed thread route and renders live broker data.
test("work-board route mounts and renders the four kanban columns", async () => {
  const tempDir = mkdtempSync(join(tmpdir(), "wuphf-work-board-"));
  let app: Awaited<ReturnType<typeof electron.launch>> | null = null;

  try {
    app = await electron.launch({
      args: [mainEntry],
      env: {
        ...process.env,
        WUPHF_RECEIPT_STORE_PATH: join(tempDir, "event-log.sqlite"),
        WUPHF_WEBAUTHN_STORE_PATH: "",
        WUPHF_RENDERER_DIST: rendererDist,
      },
    });

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
    await expect(page.getByText("1 thread")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
  } finally {
    if (app !== null) {
      await app.close();
    }
    rmSync(tempDir, { recursive: true, force: true });
  }
});
