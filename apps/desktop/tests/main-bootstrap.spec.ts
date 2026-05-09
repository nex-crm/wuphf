import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const RENDERER_URL_ENV_KEY = "ELECTRON_RENDERER_URL";

interface MockBrowserWindowInstance {
  readonly loadURL: ReturnType<typeof vi.fn<(url: string) => Promise<void>>>;
  readonly loadFile: ReturnType<typeof vi.fn<(path: string) => Promise<void>>>;
  readonly webContents: {
    readonly setWindowOpenHandler: ReturnType<typeof vi.fn<() => void>>;
    readonly on: ReturnType<typeof vi.fn<() => void>>;
  };
}

interface MockUtilityProcess {
  readonly pid: number;
  readonly on: ReturnType<typeof vi.fn<() => void>>;
  readonly once: ReturnType<typeof vi.fn<() => void>>;
  readonly off: ReturnType<typeof vi.fn<() => void>>;
  readonly postMessage: ReturnType<typeof vi.fn<(message: unknown) => void>>;
  readonly kill: ReturnType<typeof vi.fn<() => boolean>>;
}

const electronMock = vi.hoisted(() => {
  const instances: MockBrowserWindowInstance[] = [];

  class BrowserWindow {
    static readonly getAllWindows = vi.fn<() => MockBrowserWindowInstance[]>(() => instances);

    readonly loadURL = vi.fn<(url: string) => Promise<void>>(() => Promise.resolve());
    readonly loadFile = vi.fn<(path: string) => Promise<void>>(() => Promise.resolve());
    readonly webContents = {
      setWindowOpenHandler: vi.fn<() => void>(),
      on: vi.fn<() => void>(),
    };

    constructor(_options: unknown) {
      instances.push(this);
    }
  }

  function createUtilityProcess(): MockUtilityProcess {
    return {
      pid: 7777,
      on: vi.fn<() => void>(),
      once: vi.fn<() => void>(),
      off: vi.fn<() => void>(),
      postMessage: vi.fn<(message: unknown) => void>(),
      kill: vi.fn<() => boolean>(() => true),
    };
  }

  return {
    app: {
      isPackaged: false,
      whenReady: vi.fn<() => Promise<void>>(() => Promise.resolve()),
      on: vi.fn<(event: string, handler: (...args: readonly unknown[]) => void) => void>(),
      quit: vi.fn<() => void>(),
      exit: vi.fn<(code: number) => void>(),
    },
    BrowserWindow,
    instances,
    showErrorBox: vi.fn<(title: string, content: string) => void>(),
    handle: vi.fn<(channel: string, handler: unknown) => void>(),
    openExternal: vi.fn<(url: string) => Promise<void>>(() => Promise.resolve()),
    showItemInFolder: vi.fn<(path: string) => void>(),
    getVersion: vi.fn<() => string>(() => "0.0.0-test"),
    fork: vi.fn(() => createUtilityProcess()),
  };
});

vi.mock("electron", () => ({
  app: electronMock.app,
  BrowserWindow: electronMock.BrowserWindow,
  dialog: {
    showErrorBox: electronMock.showErrorBox,
  },
  ipcMain: {
    handle: electronMock.handle,
  },
  shell: {
    openExternal: electronMock.openExternal,
    showItemInFolder: electronMock.showItemInFolder,
  },
  utilityProcess: {
    fork: electronMock.fork,
  },
}));

describe("main bootstrap", () => {
  const previousRendererUrl = process.env[RENDERER_URL_ENV_KEY];

  beforeEach(() => {
    vi.resetModules();
    electronMock.instances.length = 0;
    electronMock.BrowserWindow.getAllWindows.mockClear();
    electronMock.app.isPackaged = false;
    electronMock.app.whenReady.mockClear();
    electronMock.app.on.mockClear();
    electronMock.app.quit.mockClear();
    electronMock.app.exit.mockClear();
    electronMock.showErrorBox.mockClear();
    electronMock.handle.mockClear();
    electronMock.fork.mockClear();
    delete process.env[RENDERER_URL_ENV_KEY];
  });

  afterEach(() => {
    if (previousRendererUrl === undefined) {
      delete process.env[RENDERER_URL_ENV_KEY];
      return;
    }
    process.env[RENDERER_URL_ENV_KEY] = previousRendererUrl;
  });

  it("loads the packaged renderer file when app.isPackaged is true", async () => {
    electronMock.app.isPackaged = true;
    process.env[RENDERER_URL_ENV_KEY] = "http://localhost:5173/";

    await importMainBootstrap();

    const window = getOnlyWindow();
    expect(window.loadFile).toHaveBeenCalledWith(expect.stringContaining("index.html"));
    expect(window.loadURL).not.toHaveBeenCalled();
  });

  it("loads ELECTRON_RENDERER_URL when app.isPackaged is false", async () => {
    electronMock.app.isPackaged = false;
    process.env[RENDERER_URL_ENV_KEY] = "http://localhost:5173/";

    await importMainBootstrap();

    const window = getOnlyWindow();
    expect(window.loadURL).toHaveBeenCalledWith("http://localhost:5173/");
    expect(window.loadFile).not.toHaveBeenCalled();
  });
});

async function importMainBootstrap(): Promise<void> {
  await import("../src/main/index.ts");
  await vi.waitFor(() => {
    expect(electronMock.instances).toHaveLength(1);
  });
}

function getOnlyWindow(): MockBrowserWindowInstance {
  const instance = electronMock.instances[0];
  if (instance === undefined) {
    throw new Error("Expected BrowserWindow to be constructed");
  }
  return instance;
}
