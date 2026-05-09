import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { app, BrowserWindow, dialog } from "electron";

import { BrokerSupervisor } from "./broker.ts";
import { registerIpcHandlers } from "./ipc/register-handlers.ts";
import { selectRendererDevServerUrl } from "./renderer-dev-url.ts";
import { createSecureWindow } from "./window.ts";

const currentDir = dirname(fileURLToPath(import.meta.url));
const brokerEntryPath = join(currentDir, "broker-stub-entry.js");
const preloadPath = join(currentDir, "../preload/preload.js");
const rendererIndexPath = join(currentDir, "../renderer/index.html");

const brokerSupervisor = new BrokerSupervisor({
  brokerEntryPath,
  onFatal: (reason) => {
    dialog.showErrorBox("WUPHF broker failed", reason);
  },
});
let brokerShutdownStarted = false;

app
  .whenReady()
  .then(() => {
    try {
      registerIpcHandlers(brokerSupervisor);
    } catch (error) {
      dialog.showErrorBox(
        "WUPHF IPC registration failed",
        error instanceof Error ? error.message : "Unknown IPC registration error",
      );
      app.exit(1);
      return;
    }

    createMainWindow();
    brokerSupervisor.start();

    app.on("activate", () => {
      if (BrowserWindow.getAllWindows().length === 0) {
        createMainWindow();
      }
    });
  })
  .catch((error: unknown) => {
    dialog.showErrorBox(
      "WUPHF failed to start",
      error instanceof Error ? error.message : "Unknown startup error",
    );
    app.quit();
  });

app.on("before-quit", (event) => {
  event.preventDefault();
  if (brokerShutdownStarted) {
    return;
  }
  brokerShutdownStarted = true;
  void brokerSupervisor.stop().finally(() => {
    app.exit(0);
  });
});

app.on("will-quit", () => {
  void brokerSupervisor.stop();
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

function createMainWindow(): void {
  const env = process.env as NodeJS.ProcessEnv & { readonly ELECTRON_RENDERER_URL?: string };
  const devServerUrl = selectRendererDevServerUrl(env, app.isPackaged);
  createSecureWindow({
    preloadPath,
    rendererIndexPath,
    allowDevServerUrl: !app.isPackaged,
    ...(typeof devServerUrl === "string" ? { devServerUrl } : {}),
  });
}
