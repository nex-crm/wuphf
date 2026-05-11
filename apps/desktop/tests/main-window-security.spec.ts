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
type WillFrameNavigateHandler = (event: {
  readonly preventDefault: () => void;
  readonly url: string;
}) => void;

const VITE_DEV_SERVER_URL = "http://localhost:5173/";

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
      devServerUrl: VITE_DEV_SERVER_URL,
      expectedDevServerUrl: VITE_DEV_SERVER_URL,
    });

    const instance = getOnlyWindow();
    const options = instance.options as WindowConstructorOptions;
    expect(options.webPreferences).toMatchObject({
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      webSecurity: true,
    });
    expect(instance.loadURL).toHaveBeenCalledWith(VITE_DEV_SERVER_URL);
  });

  it("rejects development renderer URLs on a different localhost port", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    expect(() =>
      createSecureWindow({
        preloadPath: "/tmp/preload.js",
        rendererIndexPath: "/tmp/index.html",
        allowDevServerUrl: true,
        devServerUrl: "http://localhost:9999/",
        expectedDevServerUrl: VITE_DEV_SERVER_URL,
      }),
    ).toThrow("Refusing to load unexpected development renderer URL: http://localhost:9999/");
  });

  it("rejects development renderer URLs without an ELECTRON_RENDERER_URL match", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    expect(() =>
      createSecureWindow({
        preloadPath: "/tmp/preload.js",
        rendererIndexPath: "/tmp/index.html",
        allowDevServerUrl: true,
        devServerUrl: VITE_DEV_SERVER_URL,
      }),
    ).toThrow("Refusing to load development renderer URL without ELECTRON_RENDERER_URL");
  });

  it("rejects invalid development renderer URL values", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    expect(() =>
      createSecureWindow({
        preloadPath: "/tmp/preload.js",
        rendererIndexPath: "/tmp/index.html",
        allowDevServerUrl: true,
        devServerUrl: "http://[",
        expectedDevServerUrl: VITE_DEV_SERVER_URL,
      }),
    ).toThrow("Invalid development renderer URL: http://[");
  });

  it("rejects loopback aliases that do not exactly match ELECTRON_RENDERER_URL", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    expect(() =>
      createSecureWindow({
        preloadPath: "/tmp/preload.js",
        rendererIndexPath: "/tmp/index.html",
        allowDevServerUrl: true,
        devServerUrl: "http://127.0.0.1:5173/",
        expectedDevServerUrl: VITE_DEV_SERVER_URL,
      }),
    ).toThrow("Refusing to load unexpected development renderer URL: http://127.0.0.1:5173/");
  });

  it("denies every new window and opens only allowlisted external schemes in the OS", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    createSecureWindow({
      preloadPath: "/tmp/preload.js",
      rendererIndexPath: "/tmp/index.html",
      allowDevServerUrl: true,
      devServerUrl: VITE_DEV_SERVER_URL,
      expectedDevServerUrl: VITE_DEV_SERVER_URL,
    });

    const handler = getWindowOpenHandler(getOnlyWindow());
    expect(handler({ url: "https://example.com/docs" })).toEqual({ action: "deny" });
    expect(electronMock.openExternal).toHaveBeenCalledWith("https://example.com/docs");

    electronMock.openExternal.mockClear();
    expect(handler({ url: "http://example.com/page" })).toEqual({ action: "deny" });
    expect(electronMock.openExternal).toHaveBeenCalledWith("http://example.com/page");

    electronMock.openExternal.mockClear();
    expect(handler({ url: "mailto:fd@example.com?subject=hi" })).toEqual({ action: "deny" });
    expect(electronMock.openExternal).toHaveBeenCalledWith("mailto:fd@example.com?subject=hi");

    electronMock.openExternal.mockClear();
    expect(handler({ url: "file:///tmp/wuphf.txt" })).toEqual({ action: "deny" });
    expect(handler({ url: "javascript:alert(1)" })).toEqual({ action: "deny" });
    expect(handler({ url: "http://[" })).toEqual({ action: "deny" });
    expect(handler({ url: "wuphf://custom" })).toEqual({ action: "deny" });
    expect(electronMock.openExternal).not.toHaveBeenCalled();
  });

  it("handles OS shell rejections from allowlisted new-window URLs", async () => {
    electronMock.openExternal.mockRejectedValueOnce(new Error("OS refused URL"));
    const { createSecureWindow } = await import("../src/main/window.ts");

    createSecureWindow({
      preloadPath: "/tmp/preload.js",
      rendererIndexPath: "/tmp/index.html",
      allowDevServerUrl: true,
      devServerUrl: VITE_DEV_SERVER_URL,
      expectedDevServerUrl: VITE_DEV_SERVER_URL,
    });

    const handler = getWindowOpenHandler(getOnlyWindow());
    expect(handler({ url: "https://example.com/docs" })).toEqual({ action: "deny" });

    await Promise.resolve();

    expect(electronMock.openExternal).toHaveBeenCalledWith("https://example.com/docs");
  });

  it("wires will-navigate and blocks navigation outside the exact renderer URL", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    createSecureWindow({
      preloadPath: "/tmp/preload.js",
      rendererIndexPath: "/tmp/index.html",
      allowDevServerUrl: true,
      devServerUrl: VITE_DEV_SERVER_URL,
      expectedDevServerUrl: VITE_DEV_SERVER_URL,
    });

    const handler = getWillNavigateHandler(getOnlyWindow());
    const exactRendererEvent = { preventDefault: vi.fn<() => void>() };
    handler(exactRendererEvent, VITE_DEV_SERVER_URL);
    expect(exactRendererEvent.preventDefault).not.toHaveBeenCalled();

    const hashRouteEvent = { preventDefault: vi.fn<() => void>() };
    handler(hashRouteEvent, "http://localhost:5173/#settings");
    expect(hashRouteEvent.preventDefault).not.toHaveBeenCalled();

    const sameOriginPathEvent = { preventDefault: vi.fn<() => void>() };
    handler(sameOriginPathEvent, "http://localhost:5173/settings");
    expect(sameOriginPathEvent.preventDefault).toHaveBeenCalledTimes(1);

    const differentPortEvent = { preventDefault: vi.fn<() => void>() };
    handler(differentPortEvent, "http://localhost:9999/");
    expect(differentPortEvent.preventDefault).toHaveBeenCalledTimes(1);

    const loopbackAliasEvent = { preventDefault: vi.fn<() => void>() };
    handler(loopbackAliasEvent, "http://127.0.0.1:5173/");
    expect(loopbackAliasEvent.preventDefault).toHaveBeenCalledTimes(1);

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
        devServerUrl: VITE_DEV_SERVER_URL,
        expectedDevServerUrl: VITE_DEV_SERVER_URL,
      }),
    ).toThrow("Refusing to load development renderer URL in packaged mode");
  });

  it("rejects non-local ELECTRON_RENDERER_URL values", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    expect(() =>
      createSecureWindow({
        preloadPath: "/tmp/preload.js",
        rendererIndexPath: "/tmp/index.html",
        allowDevServerUrl: true,
        devServerUrl: "http://192.168.0.10:5173/",
        expectedDevServerUrl: "http://192.168.0.10:5173/",
      }),
    ).toThrow("Refusing to load non-local ELECTRON_RENDERER_URL: http://192.168.0.10:5173/");
  });

  it("rejects a brokerUrl that parses but is not a loopback http URL", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    // Parses cleanly via `new URL`, fails `isLocalHttpRendererUrl` because
    // the hostname is not 127.0.0.1/localhost. This shape is the threat the
    // broker-URL gate exists to refuse: a supervisor that learned the wrong
    // origin would otherwise load `https://attacker.example.com/...` into
    // the privileged WebView.
    expect(() =>
      createSecureWindow({
        preloadPath: "/tmp/preload.js",
        rendererIndexPath: "/tmp/index.html",
        allowDevServerUrl: false,
        brokerUrl: "http://example.com:8080/",
      }),
    ).toThrow("Refusing to load non-loopback broker URL: http://example.com:8080/");
  });

  it("blocks will-redirect to a different origin while allowing same-renderer redirects", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    createSecureWindow({
      preloadPath: "/tmp/preload.js",
      rendererIndexPath: "/tmp/index.html",
      allowDevServerUrl: true,
      devServerUrl: VITE_DEV_SERVER_URL,
      expectedDevServerUrl: VITE_DEV_SERVER_URL,
    });

    const handler = getWillRedirectHandler(getOnlyWindow());

    const sameOriginEvent = { preventDefault: vi.fn<() => void>() };
    handler(sameOriginEvent, VITE_DEV_SERVER_URL);
    expect(sameOriginEvent.preventDefault).not.toHaveBeenCalled();

    const externalRedirectEvent = { preventDefault: vi.fn<() => void>() };
    handler(externalRedirectEvent, "https://attacker.example.com/exfil");
    expect(externalRedirectEvent.preventDefault).toHaveBeenCalledTimes(1);

    const differentPortEvent = { preventDefault: vi.fn<() => void>() };
    handler(differentPortEvent, "http://localhost:9999/");
    expect(differentPortEvent.preventDefault).toHaveBeenCalledTimes(1);
  });

  it("blocks will-frame-navigate to a different origin", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    createSecureWindow({
      preloadPath: "/tmp/preload.js",
      rendererIndexPath: "/tmp/index.html",
      allowDevServerUrl: true,
      devServerUrl: VITE_DEV_SERVER_URL,
      expectedDevServerUrl: VITE_DEV_SERVER_URL,
    });

    const handler = getWillFrameNavigateHandler(getOnlyWindow());

    const sameOriginFrameEvent = {
      preventDefault: vi.fn<() => void>(),
      url: VITE_DEV_SERVER_URL,
    };
    handler(sameOriginFrameEvent);
    expect(sameOriginFrameEvent.preventDefault).not.toHaveBeenCalled();

    const externalFrameEvent = {
      preventDefault: vi.fn<() => void>(),
      url: "https://attacker.example.com/iframe",
    };
    handler(externalFrameEvent);
    expect(externalFrameEvent.preventDefault).toHaveBeenCalledTimes(1);
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
  return call[1] as WillNavigateHandler;
}

function getWillRedirectHandler(instance: MockBrowserWindowInstance): WillNavigateHandler {
  const call = instance.webContents.on.mock.calls.find(([event]) => event === "will-redirect");
  if (call === undefined) {
    throw new Error("Expected will-redirect handler to be registered");
  }
  return call[1] as WillNavigateHandler;
}

function getWillFrameNavigateHandler(
  instance: MockBrowserWindowInstance,
): WillFrameNavigateHandler {
  const call = instance.webContents.on.mock.calls.find(
    ([event]) => event === "will-frame-navigate",
  );
  if (call === undefined) {
    throw new Error("Expected will-frame-navigate handler to be registered");
  }
  // The mock `on` is typed for the will-navigate two-arg shape, but
  // window.ts also passes the will-frame-navigate single-event-arg shape
  // through the same mock. Cast through unknown to bypass the structural
  // signature mismatch — the runtime callable stored by vi.fn is the
  // exact handler that window.ts registered.
  return call[1] as unknown as WillFrameNavigateHandler;
}
