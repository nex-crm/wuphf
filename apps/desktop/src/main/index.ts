import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { app, BrowserWindow, dialog, session } from "electron";

import { BrokerSupervisor } from "./broker.ts";
import { registerIpcHandlers } from "./ipc/register-handlers.ts";
import { createLogger, type LogPayload } from "./logger.ts";
import { installSessionPermissionDenyAll } from "./permissions.ts";
import { selectRendererDevServerUrl } from "./renderer-dev-url.ts";
import { createSecureWindow } from "./window.ts";

const currentDir = dirname(fileURLToPath(import.meta.url));
const brokerEntryPath = join(currentDir, "broker-stub-entry.js");
const preloadPath = join(currentDir, "../preload/preload.js");
const rendererIndexPath = join(currentDir, "../renderer/index.html");

const logger = createLogger("main");
const brokerLogger = createLogger("broker");
const ipcLogger = createLogger("ipc");

const brokerSupervisor = new BrokerSupervisor({
  brokerEntryPath,
  logger: brokerLogger,
  onFatal: (reason) => {
    logger.error("broker_fatal_dialog", { reason });
    dialog.showErrorBox("WUPHF broker failed", reason);
  },
});
let brokerShutdownStarted = false;

process.on("uncaughtException", (error) => {
  logger.error("uncaught_exception", errorPayload(error));
});

process.on("unhandledRejection", (reason) => {
  logger.error("unhandled_rejection", { reason: String(reason) });
});

app.on("render-process-gone", (_event, _webContents, details) => {
  logger.error("renderer_process_gone", rendererProcessGonePayload(details));
});

app.on("child-process-gone", (_event, details) => {
  logger.error("child_process_gone", childProcessGonePayload(details));
});

app
  .whenReady()
  .then(() => {
    logger.info("app_when_ready", { isPackaged: app.isPackaged });

    // Electron approves permission requests by default. Install a deny-all
    // pair on the default session BEFORE any BrowserWindow is created so
    // hostile renderer JS cannot request media/geolocation/notifications/
    // clipboard/displayCapture outside the IPC allowlist.
    installSessionPermissionDenyAll(session.defaultSession, { logger });

    try {
      registerIpcHandlers(brokerSupervisor, { logger: ipcLogger });
      logger.info("ipc_handlers_registered");
    } catch (error) {
      logger.error("ipc_registration_failed", errorPayload(error));
      dialog.showErrorBox(
        "WUPHF IPC registration failed",
        error instanceof Error ? error.message : "Unknown IPC registration error",
      );
      app.exit(1);
      return;
    }

    createMainWindow();
    logger.info("broker_start_requested");
    brokerSupervisor.start();

    app.on("activate", () => {
      logger.info("app_activate", { windowCount: BrowserWindow.getAllWindows().length });
      if (BrowserWindow.getAllWindows().length === 0) {
        createMainWindow();
      }
    });
  })
  .catch((error: unknown) => {
    logger.error("app_start_failed", errorPayload(error));
    dialog.showErrorBox(
      "WUPHF failed to start",
      error instanceof Error ? error.message : "Unknown startup error",
    );
    app.quit();
  });

app.on("before-quit", (event) => {
  event.preventDefault();
  logger.info("app_before_quit", { alreadyStopping: brokerShutdownStarted });
  if (brokerShutdownStarted) {
    return;
  }
  brokerShutdownStarted = true;
  void brokerSupervisor.stop().finally(() => {
    app.exit(0);
  });
});

app.on("will-quit", () => {
  logger.info("app_will_quit");
  void brokerSupervisor.stop();
});

app.on("window-all-closed", () => {
  logger.info("app_window_all_closed", { platform: process.platform });
  if (process.platform !== "darwin") {
    app.quit();
  }
});

function createMainWindow(): void {
  const env = process.env as NodeJS.ProcessEnv & { readonly ELECTRON_RENDERER_URL?: string };
  const devServerUrl = selectRendererDevServerUrl(env, app.isPackaged);
  logger.info("main_window_create_requested", {
    isPackaged: app.isPackaged,
    rendererKind: typeof devServerUrl === "string" ? "dev" : "file",
  });
  createSecureWindow({
    preloadPath,
    rendererIndexPath,
    allowDevServerUrl: !app.isPackaged,
    ...(typeof devServerUrl === "string"
      ? { devServerUrl, expectedDevServerUrl: devServerUrl }
      : {}),
  });
  logger.info("main_window_created", { windowCount: BrowserWindow.getAllWindows().length });
}

function errorPayload(error: unknown): LogPayload {
  if (!(error instanceof Error)) {
    return { error: String(error) };
  }

  if (typeof error.stack === "string") {
    return { error: error.message, stack: error.stack };
  }

  return { error: error.message };
}

function rendererProcessGonePayload(details: unknown): LogPayload {
  return {
    reason: stringField(details, "reason"),
    exitCode: numberField(details, "exitCode"),
  };
}

function childProcessGonePayload(details: unknown): LogPayload {
  return {
    processType: stringField(details, "type"),
    reason: stringField(details, "reason"),
    exitCode: numberField(details, "exitCode"),
    serviceName: stringField(details, "serviceName"),
  };
}

function stringField(value: unknown, key: string): string | null {
  if (typeof value !== "object" || value === null || !Object.hasOwn(value, key)) {
    return null;
  }

  const field = (value as Readonly<Record<string, unknown>>)[key];
  return typeof field === "string" ? field : null;
}

function numberField(value: unknown, key: string): number | null {
  if (typeof value !== "object" || value === null || !Object.hasOwn(value, key)) {
    return null;
  }

  const field = (value as Readonly<Record<string, unknown>>)[key];
  return typeof field === "number" ? field : null;
}
