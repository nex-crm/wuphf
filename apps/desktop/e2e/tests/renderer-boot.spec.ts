import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { _electron as electron, expect, test } from "@playwright/test";

const desktopRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const mainEntry = resolve(desktopRoot, "out/main/index.js");
const rendererDist = resolve(desktopRoot, "out/renderer");

// Proof-of-life test: the renderer mounts, talks to the broker
// subprocess over loopback HTTP, and displays a value that could only
// have come from the back end.
//
// "Displays a value that could only have come from the back end" is
// load-bearing here. The broker binds an ephemeral port (`port: 0`),
// so the URL `http://localhost:<port>` shown in the "Broker" card is
// determined at runtime by the kernel, not by any constant in
// `apps/desktop/src/renderer/`. Asserting that the renderer paints
// `http://localhost:<some non-zero port>` proves the entire round
// trip:
//
//   1. Electron main starts the broker utility process
//   2. Broker binds 127.0.0.1:0, the OS picks a port
//   3. Broker reports the URL back to main via parentPort
//   4. Main creates the BrowserWindow at `${brokerUrl}/`
//   5. Renderer loads HTML/JS from the broker's static handler
//   6. `BrokerBootstrapProvider` fetches `/api-token` → bearer + URL
//   7. Fetches `/api/health` with the bearer → 200
//   8. Renders the URL string in the DOM
//
// Reverting any of steps 1–8 makes this test fail.
test("renderer displays broker URL fetched from the broker subprocess", async () => {
  const app = await electron.launch({
    args: [mainEntry],
    env: {
      ...process.env,
      // In-memory store override: the broker's SQLite native module
      // is incompatible with Electron 42's V8 ABI at the time of
      // writing (see commit 2b3df9b4). The renderer foundation does
      // not exercise receipts/webauthn anyway, so dropping persistence
      // is safe for this test.
      WUPHF_RECEIPT_STORE_PATH: "",
      WUPHF_WEBAUTHN_STORE_PATH: "",
      // Tell the broker where the prebuilt renderer assets live so
      // it serves them at `${brokerUrl}/` (the production load path,
      // same-origin with `/api-token`).
      WUPHF_RENDERER_DIST: rendererDist,
    },
  });

  try {
    const window = await app.firstWindow();
    // `Ready` is the StatusBadge label `BrokerBootstrapProvider`
    // renders after `loadBrokerBootstrap` resolves. There are several
    // "Ready" texts in the tree (sidebar pill, broker card badge);
    // `getByRole` filters down to the badge.
    await expect(window.getByText("Ready", { exact: true }).first()).toBeVisible({
      timeout: 20_000,
    });

    // The DOM should now carry the broker URL. Match the ephemeral
    // shape — `http://localhost:<port>` with port ∈ [1, 65535] — so
    // a stubbed/hardcoded value would not satisfy the assertion.
    const brokerUrlLocator = window.locator("text=/^http:\\/\\/localhost:[1-9][0-9]{0,4}$/");
    await expect(brokerUrlLocator).toBeVisible();

    const brokerUrlText = (await brokerUrlLocator.textContent())?.trim() ?? "";
    expect(brokerUrlText).toMatch(/^http:\/\/localhost:[1-9][0-9]{0,4}$/);
    const port = Number.parseInt(brokerUrlText.split(":").at(-1) ?? "0", 10);
    expect(port).toBeGreaterThan(0);
    expect(port).toBeLessThan(65_536);

    // Cross-check with main: the URL the renderer shows must equal
    // what the supervisor reports through the IPC bridge. If they
    // diverge, the renderer is painting stale state. The cast is
    // local because the Playwright Page doesn't know about the
    // contextBridge allowlist injected by the preload script.
    const ipcBrokerUrl = await window.evaluate<string | null>(async () => {
      interface BridgeWindow {
        readonly wuphf: {
          getBrokerStatus(): Promise<{ readonly brokerUrl: string | null }>;
        };
      }
      const bridge = window as unknown as BridgeWindow;
      return (await bridge.wuphf.getBrokerStatus()).brokerUrl;
    });
    expect(ipcBrokerUrl).toBe(brokerUrlText);
  } finally {
    await app.close();
  }
});
