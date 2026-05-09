import { pathToFileURL } from "node:url";
import { BrowserWindow, shell } from "electron";

const ALLOWED_EXTERNAL_PROTOCOLS = new Set(["https:", "http:", "mailto:"]);

export interface CreateSecureWindowConfig {
  readonly preloadPath: string;
  readonly rendererIndexPath: string;
  readonly allowDevServerUrl: boolean;
  readonly devServerUrl?: string;
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
      void shell.openExternal(details.url);
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

    const parsedDevUrl = new URL(config.devServerUrl);
    if (!isLocalDevRendererUrl(parsedDevUrl)) {
      throw new Error(`Refusing to load non-local renderer URL: ${config.devServerUrl}`);
    }
    return parsedDevUrl.toString();
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

  if (parsedRendererUrl.protocol === "file:") {
    return stripFileUrlRoutingState(parsedTargetUrl) === stripFileUrlRoutingState(parsedRendererUrl);
  }

  return parsedTargetUrl.origin === parsedRendererUrl.origin;
}

function stripFileUrlRoutingState(value: URL): string {
  return `${value.origin}${value.pathname}`;
}

function isAllowedExternalUrl(value: string): boolean {
  try {
    const parsedUrl = new URL(value);
    return ALLOWED_EXTERNAL_PROTOCOLS.has(parsedUrl.protocol);
  } catch {
    return false;
  }
}

function isLocalDevRendererUrl(value: URL): boolean {
  return (
    value.protocol === "http:" &&
    (value.hostname === "localhost" || value.hostname === "127.0.0.1") &&
    value.port.length > 0
  );
}
