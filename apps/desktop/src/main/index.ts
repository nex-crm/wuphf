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
const brokerEntryPath = join(currentDir, "broker-entry.js");
const preloadPath = join(currentDir, "../preload/preload.js");
const rendererIndexPath = join(currentDir, "../renderer/index.html");
const rendererDistDir = join(currentDir, "../renderer");
const RENDERER_DIST_ENV = "WUPHF_RENDERER_DIST";
const DEV_RENDERER_ORIGIN_ENV = "WUPHF_DEV_RENDERER_ORIGIN";
const ELECTRON_RENDERER_URL_ENV = "ELECTRON_RENDERER_URL";
const RECEIPT_STORE_PATH_ENV = "WUPHF_RECEIPT_STORE_PATH";

const logger = createLogger("main");
const brokerLogger = createLogger("broker");
const ipcLogger = createLogger("ipc");

// In packaged builds the broker serves the renderer bundle directly so the
// BrowserWindow can load `${brokerUrl}/` (same-origin loopback). In dev,
// electron-vite owns the renderer, so we leave the env unset and let the
// broker static handler 404 on `/` — the dev server is the renderer source
// of truth there.
if (app.isPackaged) {
  process.env[RENDERER_DIST_ENV] = rendererDistDir;
}
// In dev mode the renderer is loaded from `ELECTRON_RENDERER_URL` (a Vite
// dev server, typically `http://localhost:5173`). The renderer's
// bootstrap probe still fetches `/api-token` from the broker, which is a
// cross-origin request the new Origin gate would reject. Plumb the dev
// origin through to the broker subprocess so it can add it to the
// /api-token trusted-origins allowlist (and only there — `/api/*` still
// require bearer auth and are unaffected by this).
const devRendererOrigin = devOriginFromEnv(process.env[ELECTRON_RENDERER_URL_ENV]);
if (!app.isPackaged && devRendererOrigin !== null) {
  process.env[DEV_RENDERER_ORIGIN_ENV] = devRendererOrigin;
}

function devOriginFromEnv(value: string | undefined): string | null {
  if (typeof value !== "string" || value.length === 0) return null;
  try {
    return new URL(value).origin;
  } catch {
    return null;
  }
}

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

    // Branch 6: the broker utility process opens a durable, SQLite
    // event-log-backed ReceiptStore at `<userData>/event-log.sqlite`
    // when this env var is set; without it the broker falls back to
    // the in-memory store. We set it unconditionally for the
    // packaged + dev paths so receipts persist across restarts.
    // `app.getPath("userData")` is safe inside `whenReady`.
    process.env[RECEIPT_STORE_PATH_ENV] = join(app.getPath("userData"), "event-log.sqlite");

    logger.info("broker_start_requested");
    brokerSupervisor.start();

    // Wait for the broker to bind a port before we load the renderer. In
    // packaged mode the BrowserWindow loads from `${brokerUrl}/`, so the
    // listener has to be live first; in dev mode we still wait so the
    // renderer can read `getBrokerStatus().brokerUrl` synchronously after
    // mount instead of polling.
    void brokerSupervisor
      .whenReady()
      .then(() => {
        logger.info("broker_ready_received");
        createMainWindow();
      })
      .catch((err: unknown) => {
        logger.error("broker_ready_wait_failed", errorPayload(err));
        // The fatal-reason dialog from `onFatal` already informed the user;
        // we just refuse to open a window pointing at a dead broker.
      });

    // On broker restart, the new fork binds a fresh ephemeral loopback
    // port. Existing BrowserWindows were loaded from the OLD origin and
    // its `window.location.origin` is now pointing at a dead listener —
    // any subsequent same-origin `/api/*` fetch would fail. Destroy and
    // recreate broker-pinned windows so the renderer re-binds to the new
    // origin. The first ready (no existing windows) is a no-op here; the
    // whenReady().then path above handles that case.
    brokerSupervisor.subscribeReady(() => {
      rebuildBrokerPinnedWindows();
    });

    app.on("activate", () => {
      logger.info("app_activate", { windowCount: BrowserWindow.getAllWindows().length });
      if (BrowserWindow.getAllWindows().length === 0) {
        const snapshot = brokerSupervisor.getSnapshot();
        if (snapshot.brokerUrl !== null) {
          createMainWindow();
        } else {
          logger.warn("app_activate_skipped_no_broker_url");
        }
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

// Registry of windows whose `webContents.getURL()` is pinned to a broker
// origin. We can't introspect the will-navigate gate's anchored URL from
// outside, so we record window kind at creation time. WeakSet so a window
// that gets destroyed and GC'd doesn't leak through this set.
//
// Future auxiliary windows (preferences, auth dialogs, devtools spawned
// outside the broker origin) won't be added to this set, so they survive
// broker restarts.
const brokerPinnedWindows = new WeakSet<BrowserWindow>();

function rebuildBrokerPinnedWindows(): void {
  // Iterate all live windows but rebuild only the ones we registered as
  // broker-pinned. `getAllWindows()` is the only way to enumerate; the
  // WeakSet filters.
  const live = BrowserWindow.getAllWindows();
  const pinned = live.filter((w) => brokerPinnedWindows.has(w) && !w.isDestroyed());
  if (pinned.length === 0) {
    // Either first ready (no windows yet — whenReady().then() handles it)
    // or all open windows are auxiliary (preferences, etc.) — leave them.
    return;
  }
  logger.info("broker_restart_window_rebuild", { windowCount: pinned.length });
  for (const window of pinned) {
    brokerPinnedWindows.delete(window);
    window.destroy();
  }
  createMainWindow();
}

function createMainWindow(): void {
  const env = process.env as NodeJS.ProcessEnv & { readonly ELECTRON_RENDERER_URL?: string };
  const devServerUrl = selectRendererDevServerUrl(env, app.isPackaged);
  const brokerUrl = brokerSupervisor.getSnapshot().brokerUrl;
  // Renderer source priority:
  //   1. dev server URL (electron-vite dev) when present and unpackaged.
  //   2. broker URL (packaged: broker serves the bundle).
  //   3. file:// fallback (e.g. test/preview without a live broker yet).
  const rendererKind: "dev" | "broker" | "file" =
    typeof devServerUrl === "string"
      ? "dev"
      : typeof brokerUrl === "string" && brokerUrl.length > 0
        ? "broker"
        : "file";
  logger.info("main_window_create_requested", {
    isPackaged: app.isPackaged,
    rendererKind,
  });
  const window = createSecureWindow({
    preloadPath,
    rendererIndexPath,
    allowDevServerUrl: !app.isPackaged,
    ...(rendererKind === "dev" && typeof devServerUrl === "string"
      ? { devServerUrl, expectedDevServerUrl: devServerUrl }
      : {}),
    ...(rendererKind === "broker" && typeof brokerUrl === "string" ? { brokerUrl } : {}),
  });
  // Register windows whose origin is pinned to the broker so we know to
  // rebuild THEM (not auxiliary windows) when the broker restarts on a
  // new ephemeral port. Dev-server and file:// windows are not pinned;
  // they don't lose connectivity when the broker rebinds.
  if (rendererKind === "broker") {
    brokerPinnedWindows.add(window);
  }
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
