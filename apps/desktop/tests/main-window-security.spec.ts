import { beforeEach, describe, expect, it, vi } from "vitest";

interface MockBrowserWindowInstance {
  readonly options: unknown;
  readonly loadURL: ReturnType<typeof vi.fn<(url: string) => Promise<void>>>;
  readonly loadFile: ReturnType<typeof vi.fn<(path: string) => Promise<void>>>;
  readonly webContents: {
    readonly setWindowOpenHandler: ReturnType<typeof vi.fn<(handler: WindowOpenHandler) => void>>;
    readonly on: ReturnType<typeof vi.fn<(event: string, handler: WillNavigateHandler) => void>>;
  };
}

type WindowOpenHandler = (details: { readonly url: string }) => { readonly action: "deny" };
type WillNavigateHandler = (
  event: { readonly preventDefault: () => void },
  targetUrl: string,
) => void;

interface WindowConstructorOptions {
  readonly webPreferences?: {
    readonly sandbox?: unknown;
    readonly contextIsolation?: unknown;
    readonly nodeIntegration?: unknown;
    readonly webSecurity?: unknown;
  };
}

const electronMock = vi.hoisted(() => {
  const instances: MockBrowserWindowInstance[] = [];

  class BrowserWindow {
    readonly options: unknown;
    readonly loadURL = vi.fn<(url: string) => Promise<void>>(() => Promise.resolve());
    readonly loadFile = vi.fn<(path: string) => Promise<void>>(() => Promise.resolve());
    readonly webContents = {
      setWindowOpenHandler: vi.fn<(handler: WindowOpenHandler) => void>(),
      on: vi.fn<(event: string, handler: WillNavigateHandler) => void>(),
    };

    constructor(options: unknown) {
      this.options = options;
      instances.push(this);
    }
  }

  return {
    BrowserWindow,
    instances,
    openExternal: vi.fn<(url: string) => Promise<void>>(() => Promise.resolve()),
  };
});

vi.mock("electron", () => ({
  BrowserWindow: electronMock.BrowserWindow,
  shell: {
    openExternal: electronMock.openExternal,
  },
}));

describe("createSecureWindow", () => {
  beforeEach(() => {
    vi.resetModules();
    electronMock.instances.length = 0;
    electronMock.openExternal.mockClear();
  });

  it("constructs a BrowserWindow with strict webPreferences", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    createSecureWindow({
      preloadPath: "/tmp/preload.js",
      rendererIndexPath: "/tmp/index.html",
      allowDevServerUrl: true,
      devServerUrl: "http://localhost:5173/",
    });

    const instance = getOnlyWindow();
    const options = instance.options as WindowConstructorOptions;
    expect(options.webPreferences).toMatchObject({
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      webSecurity: true,
    });
    expect(instance.loadURL).toHaveBeenCalledWith("http://localhost:5173/");
  });

  it("allows 127.0.0.1 development renderer URLs", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    createSecureWindow({
      preloadPath: "/tmp/preload.js",
      rendererIndexPath: "/tmp/index.html",
      allowDevServerUrl: true,
      devServerUrl: "http://127.0.0.1:5173/",
    });

    expect(getOnlyWindow().loadURL).toHaveBeenCalledWith("http://127.0.0.1:5173/");
  });

  it("denies every new window and opens only allowlisted external schemes in the OS", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    createSecureWindow({
      preloadPath: "/tmp/preload.js",
      rendererIndexPath: "/tmp/index.html",
      allowDevServerUrl: true,
      devServerUrl: "http://localhost:5173/",
    });

    const handler = getWindowOpenHandler(getOnlyWindow());
    expect(handler({ url: "https://example.com/docs" })).toEqual({ action: "deny" });
    expect(electronMock.openExternal).toHaveBeenCalledWith("https://example.com/docs");

    electronMock.openExternal.mockClear();
    expect(handler({ url: "file:///tmp/wuphf.txt" })).toEqual({ action: "deny" });
    expect(handler({ url: "javascript:alert(1)" })).toEqual({ action: "deny" });
    expect(handler({ url: "http://[" })).toEqual({ action: "deny" });
    expect(handler({ url: "wuphf://custom" })).toEqual({ action: "deny" });
    expect(electronMock.openExternal).not.toHaveBeenCalled();
  });

  it("wires will-navigate and blocks navigation outside the renderer origin", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    createSecureWindow({
      preloadPath: "/tmp/preload.js",
      rendererIndexPath: "/tmp/index.html",
      allowDevServerUrl: true,
      devServerUrl: "http://localhost:5173/",
    });

    const handler = getWillNavigateHandler(getOnlyWindow());
    const sameOriginEvent = { preventDefault: vi.fn<() => void>() };
    handler(sameOriginEvent, "http://localhost:5173/settings");
    expect(sameOriginEvent.preventDefault).not.toHaveBeenCalled();

    const externalEvent = { preventDefault: vi.fn<() => void>() };
    handler(externalEvent, "https://example.com/");
    expect(externalEvent.preventDefault).toHaveBeenCalledTimes(1);

    const invalidEvent = { preventDefault: vi.fn<() => void>() };
    handler(invalidEvent, "http://[");
    expect(invalidEvent.preventDefault).toHaveBeenCalledTimes(1);
  });

  it("allows same file renderer hash navigation", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    createSecureWindow({
      preloadPath: "/tmp/preload.js",
      rendererIndexPath: "/tmp/index.html",
      allowDevServerUrl: true,
    });

    const handler = getWillNavigateHandler(getOnlyWindow());
    expect(getOnlyWindow().loadFile).toHaveBeenCalledWith("/tmp/index.html");

    const sameFileHashEvent = { preventDefault: vi.fn<() => void>() };
    handler(sameFileHashEvent, "file:///tmp/index.html#about");

    expect(sameFileHashEvent.preventDefault).not.toHaveBeenCalled();
  });

  it("rejects development renderer URLs when packaged mode disallows them", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    expect(() =>
      createSecureWindow({
        preloadPath: "/tmp/preload.js",
        rendererIndexPath: "/tmp/index.html",
        allowDevServerUrl: false,
        devServerUrl: "http://localhost:5173/",
      }),
    ).toThrow("Refusing to load development renderer URL in packaged mode");
  });

  it("rejects non-local development renderer URLs", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    expect(() =>
      createSecureWindow({
        preloadPath: "/tmp/preload.js",
        rendererIndexPath: "/tmp/index.html",
        allowDevServerUrl: true,
        devServerUrl: "http://192.168.0.10:5173/",
      }),
    ).toThrow("Refusing to load non-local renderer URL: http://192.168.0.10:5173/");
  });
});

function getOnlyWindow(): MockBrowserWindowInstance {
  const instance = electronMock.instances[0];
  if (instance === undefined) {
    throw new Error("Expected BrowserWindow to be constructed");
  }
  return instance;
}

function getWindowOpenHandler(instance: MockBrowserWindowInstance): WindowOpenHandler {
  const call = instance.webContents.setWindowOpenHandler.mock.calls[0];
  if (call === undefined) {
    throw new Error("Expected setWindowOpenHandler to be called");
  }
  return call[0];
}

function getWillNavigateHandler(instance: MockBrowserWindowInstance): WillNavigateHandler {
  const call = instance.webContents.on.mock.calls.find(([event]) => event === "will-navigate");
  if (call === undefined) {
    throw new Error("Expected will-navigate handler to be registered");
  }
  return call[1];
}
