import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { parseBootstrap } from "../src/renderer/bootstrap.ts";
import type { GetBrokerStatusResponse, WuphfDesktopApi } from "../src/shared/api-contract.ts";

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

const VALID_BOOTSTRAP_TOKEN = "A".repeat(16);
const VALID_BOOTSTRAP_URL = "http://127.0.0.1:54321";

describe("parseBootstrap", () => {
  it("rejects accessor and inherited bootstrap properties", () => {
    expect(() =>
      parseBootstrap({
        get token() {
          return "anything";
        },
        broker_url: VALID_BOOTSTRAP_URL,
      }),
    ).toThrow("api-token response: token: must be a data property");

    expect(() =>
      parseBootstrap({
        token: VALID_BOOTSTRAP_TOKEN,
        get broker_url() {
          return VALID_BOOTSTRAP_URL;
        },
      }),
    ).toThrow("api-token response: broker_url: must be a data property");

    const proto = { token: "x", broker_url: "y" };
    const obj: object = Object.create(proto) as object;

    expect(() => parseBootstrap(obj)).toThrow("api-token response: token: is required");
  });

  it("rejects descriptor traps that make bootstrap values unreachable", () => {
    const throwingRecord = new Proxy(
      { token: VALID_BOOTSTRAP_TOKEN, broker_url: VALID_BOOTSTRAP_URL },
      {
        getOwnPropertyDescriptor(target, property) {
          if (property === "token") {
            throw new Error("token descriptor unreachable");
          }
          return Reflect.getOwnPropertyDescriptor(target, property);
        },
      },
    );

    expect(() => parseBootstrap(throwingRecord)).toThrow("token descriptor unreachable");
  });

  it("rejects array-shaped bootstrap records", () => {
    const arrayBootstrap = Object.assign([], {
      token: VALID_BOOTSTRAP_TOKEN,
      broker_url: VALID_BOOTSTRAP_URL,
    });

    expect(() => parseBootstrap(arrayBootstrap)).toThrow("api-token response is not an object");
  });
});

describe("renderer broker bootstrap probe", () => {
  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("probes again after a broker restart publishes a new URL", async () => {
    const firstBrokerUrl = "http://127.0.0.1:54321";
    const secondBrokerUrl = "http://127.0.0.1:54322";
    let brokerUrl: string | null = firstBrokerUrl;
    let restartCount = 0;
    const getBrokerStatus = vi.fn<WuphfDesktopApi["getBrokerStatus"]>(() =>
      Promise.resolve(createBrokerStatus(brokerUrl, restartCount)),
    );
    const fetchMock = vi.fn<typeof fetch>((input) => {
      const url = String(input);
      if (url.endsWith("/api-token")) {
        const responseBrokerUrl = url.slice(0, -"/api-token".length);
        return Promise.resolve(
          jsonResponse({
            token: VALID_BOOTSTRAP_TOKEN,
            broker_url: responseBrokerUrl,
          }),
        );
      }
      if (url.endsWith("/api/health")) {
        return Promise.resolve(jsonResponse({ ok: true }));
      }
      return Promise.reject(new Error(`unexpected fetch ${url}`));
    });
    const { module } = await importRendererHarness({
      api: { getBrokerStatus },
      fetch: fetchMock,
    });

    await flushRendererTasks();
    expect(fetchMock).toHaveBeenCalledWith(`${firstBrokerUrl}/api-token`);

    fetchMock.mockClear();
    brokerUrl = null;
    restartCount = 1;
    await module.refreshBrokerStatus();
    await flushRendererTasks();
    expect(fetchMock).not.toHaveBeenCalled();

    brokerUrl = secondBrokerUrl;
    restartCount = 2;
    await module.refreshBrokerStatus();
    await flushRendererTasks();

    expect(fetchMock).toHaveBeenCalledWith(`${secondBrokerUrl}/api-token`);
    expect(fetchMock).toHaveBeenCalledWith(`${secondBrokerUrl}/api/health`, {
      headers: { Authorization: `Bearer ${VALID_BOOTSTRAP_TOKEN}` },
    });
  });
});

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

    // Parses cleanly via `new URL`, fails `isBrokerUrl` because the
    // hostname is not 127.0.0.1/localhost. This shape is the threat the
    // broker-URL gate exists to refuse: a supervisor that learned the
    // wrong origin would otherwise load `https://attacker.example.com/...`
    // into the privileged WebView.
    expect(() =>
      createSecureWindow({
        preloadPath: "/tmp/preload.js",
        rendererIndexPath: "/tmp/index.html",
        allowDevServerUrl: false,
        brokerUrl: "http://example.com:8080/",
      }),
    ).toThrow("Refusing to load non-loopback broker URL: http://example.com:8080/");
  });

  it("rejects a loopback brokerUrl with userinfo, non-root path, query, or fragment", async () => {
    const { createSecureWindow } = await import("../src/main/window.ts");

    // Pass-3 triangulation (types lens MEDIUM): `isLocalHttpRendererUrl`
    // accepts these shapes because protocol+host+port pass. The full
    // BrokerUrl brand (`@wuphf/protocol#isBrokerUrl`) rejects them so
    // `${"u"}:${"p"}@127.0.0.1:54321`, `/api-token`, encoded dot segments,
    // `?x=1`, and `#frag` can't be smuggled past the broker-URL gate into
    // `loadURL`.
    const cases = [
      `http://${"u"}:${"p"}@127.0.0.1:54321/`,
      "http://127.0.0.1:54321/", // pass-5 tightening: bare canonical form is sole accepted shape
      "http://127.0.0.1:54321/api-token",
      "http://127.0.0.1:54321/%2e%2e",
      "http://127.0.0.1:54321?x=1",
      "http://127.0.0.1:54321#frag",
    ];
    for (const brokerUrl of cases) {
      expect(() =>
        createSecureWindow({
          preloadPath: "/tmp/preload.js",
          rendererIndexPath: "/tmp/index.html",
          allowDevServerUrl: false,
          brokerUrl,
        }),
      ).toThrow(`Refusing to load non-loopback broker URL: ${brokerUrl}`);
    }
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

interface RendererElementStub {
  className: string;
  textContent: string;
  type: string;
  readonly append: ReturnType<typeof vi.fn<(...children: RendererElementStub[]) => void>>;
  readonly addEventListener: ReturnType<typeof vi.fn<(event: string, handler: () => void) => void>>;
}

interface RendererDocumentStub {
  title: string;
  readonly querySelector: ReturnType<
    typeof vi.fn<(selector: string) => RendererElementStub | null>
  >;
  readonly createElement: ReturnType<typeof vi.fn<(tagName: string) => RendererElementStub>>;
}

type RendererMainModule = typeof import("../src/renderer/main.ts");
type FetchMock = ReturnType<typeof vi.fn<typeof fetch>>;

interface RendererImportOptions {
  readonly api?: Partial<WuphfDesktopApi>;
  readonly fetch?: FetchMock;
}

interface RendererHarness {
  readonly module: RendererMainModule;
  readonly api: WuphfDesktopApi;
  readonly fetchMock: FetchMock;
}

async function importRendererHarness(
  options: RendererImportOptions = {},
): Promise<RendererHarness> {
  vi.resetModules();
  vi.useFakeTimers();
  const root = createRendererElementStub();
  const documentStub: RendererDocumentStub = {
    title: "",
    querySelector: vi.fn<(selector: string) => RendererElementStub | null>(() => root),
    createElement: vi.fn<(tagName: string) => RendererElementStub>(() =>
      createRendererElementStub(),
    ),
  };
  const defaultApi: WuphfDesktopApi = {
    openExternal: vi.fn<WuphfDesktopApi["openExternal"]>(() => Promise.resolve({ ok: true })),
    showItemInFolder: vi.fn<WuphfDesktopApi["showItemInFolder"]>(() =>
      Promise.resolve({ ok: true }),
    ),
    getAppVersion: vi.fn<WuphfDesktopApi["getAppVersion"]>(() =>
      Promise.resolve({ version: "test" }),
    ),
    getPlatform: vi.fn<WuphfDesktopApi["getPlatform"]>(() =>
      Promise.resolve({ platform: "linux", arch: "x64" }),
    ),
    getBrokerStatus: vi.fn<WuphfDesktopApi["getBrokerStatus"]>(() =>
      Promise.resolve({ status: "dead", pid: null, restartCount: 0, brokerUrl: null }),
    ),
  };
  const api: WuphfDesktopApi = { ...defaultApi, ...options.api };
  const fetchMock = options.fetch ?? vi.fn<typeof fetch>();

  vi.stubGlobal("document", documentStub);
  vi.stubGlobal("window", {
    wuphf: api,
    location: { origin: "http://localhost:5173" },
  });
  vi.stubGlobal("fetch", fetchMock);

  return {
    module: await import("../src/renderer/main.ts"),
    api,
    fetchMock,
  };
}

function createRendererElementStub(): RendererElementStub {
  return {
    className: "",
    textContent: "",
    type: "",
    append: vi.fn<(...children: RendererElementStub[]) => void>(),
    addEventListener: vi.fn<(event: string, handler: () => void) => void>(),
  };
}

function createBrokerStatus(
  brokerUrl: string | null,
  restartCount: number,
): GetBrokerStatusResponse {
  return {
    status: brokerUrl === null ? "dead" : "alive",
    pid: brokerUrl === null ? null : 1234,
    restartCount,
    brokerUrl,
  };
}

function jsonResponse(value: unknown): Response {
  return {
    ok: true,
    status: 200,
    json: () => Promise.resolve(value),
  } as Response;
}

async function flushRendererTasks(): Promise<void> {
  for (let index = 0; index < 10; index += 1) {
    await Promise.resolve();
  }
}
