import { pathToFileURL } from "node:url";
import { BrowserWindow, shell } from "electron";

const ALLOWED_EXTERNAL_PROTOCOLS = new Set(["https:", "http:", "mailto:"]);
const RENDERER_DEV_URL_ENV_KEY = "ELECTRON_RENDERER_URL";

export interface CreateSecureWindowConfig {
  readonly preloadPath: string;
  readonly rendererIndexPath: string;
  readonly allowDevServerUrl: boolean;
  readonly devServerUrl?: string;
  readonly expectedDevServerUrl?: string;
}

export function createSecureWindow(config: CreateSecureWindowConfig): BrowserWindow {
  const rendererUrl = resolveRendererUrl(config);
  const browserWindow = new BrowserWindow({
    width: 880,
    height: 540,
    minWidth: 520,
    minHeight: 360,
    title: "WUPHF v1 desktop shell",
    webPreferences: {
      preload: config.preloadPath,
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      webSecurity: true,
    },
  });

  browserWindow.webContents.setWindowOpenHandler((details) => {
    if (isAllowedExternalUrl(details.url)) {
      void shell.openExternal(details.url).catch(() => undefined);
    }
    return { action: "deny" };
  });

  browserWindow.webContents.on("will-navigate", (event, targetUrl) => {
    if (!isAllowedRendererNavigation(targetUrl, rendererUrl)) {
      event.preventDefault();
    }
  });

  if (new URL(rendererUrl).protocol === "file:") {
    void browserWindow.loadFile(config.rendererIndexPath);
  } else {
    void browserWindow.loadURL(rendererUrl);
  }
  return browserWindow;
}

function resolveRendererUrl(config: CreateSecureWindowConfig): string {
  if (typeof config.devServerUrl === "string" && config.devServerUrl.length > 0) {
    if (!config.allowDevServerUrl) {
      throw new Error("Refusing to load development renderer URL in packaged mode");
    }

    const expectedDevServerUrl = resolveExpectedDevServerUrl(config.expectedDevServerUrl);
    const devServerUrl = parseUrl(config.devServerUrl, "development renderer URL").toString();
    if (devServerUrl !== expectedDevServerUrl) {
      throw new Error(
        `Refusing to load unexpected development renderer URL: ${config.devServerUrl}`,
      );
    }
    return devServerUrl;
  }

  return pathToFileURL(config.rendererIndexPath).toString();
}

function isAllowedRendererNavigation(targetUrl: string, rendererUrl: string): boolean {
  let parsedTargetUrl: URL;
  let parsedRendererUrl: URL;
  try {
    parsedTargetUrl = new URL(targetUrl);
    parsedRendererUrl = new URL(rendererUrl);
  } catch {
    return false;
  }

  if (parsedRendererUrl.protocol === "file:" || parsedRendererUrl.protocol === "http:") {
    return stripUrlHash(parsedTargetUrl) === stripUrlHash(parsedRendererUrl);
  }

  return false;
}

function stripUrlHash(value: URL): string {
  const withoutHash = new URL(value.toString());
  withoutHash.hash = "";
  return withoutHash.toString();
}

function isAllowedExternalUrl(value: string): boolean {
  try {
    const parsedUrl = new URL(value);
    return ALLOWED_EXTERNAL_PROTOCOLS.has(parsedUrl.protocol);
  } catch {
    return false;
  }
}

function resolveExpectedDevServerUrl(value: string | undefined): string {
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(
      `Refusing to load development renderer URL without ${RENDERER_DEV_URL_ENV_KEY}`,
    );
  }

  const expectedDevServerUrl = parseUrl(value, RENDERER_DEV_URL_ENV_KEY);
  if (!isLocalHttpRendererUrl(expectedDevServerUrl)) {
    throw new Error(`Refusing to load non-local ${RENDERER_DEV_URL_ENV_KEY}: ${value}`);
  }

  return expectedDevServerUrl.toString();
}

function parseUrl(value: string, label: string): URL {
  try {
    return new URL(value);
  } catch {
    throw new Error(`Invalid ${label}: ${value}`);
  }
}

function isLocalHttpRendererUrl(value: URL): boolean {
  return (
    value.protocol === "http:" &&
    (value.hostname === "localhost" || value.hostname === "127.0.0.1") &&
    value.port.length > 0
  );
}
