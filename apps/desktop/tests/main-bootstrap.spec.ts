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

interface MockBrokerSnapshot {
  readonly status: "unknown";
  readonly pid: null;
  readonly restartCount: 0;
}

interface MockBrokerSupervisorInstance {
  readonly start: ReturnType<typeof vi.fn<() => void>>;
  readonly stop: ReturnType<typeof vi.fn<() => Promise<void>>>;
  readonly getSnapshot: ReturnType<typeof vi.fn<() => MockBrokerSnapshot>>;
}

interface MockLogCall {
  readonly module: string;
  readonly level: "debug" | "info" | "warn" | "error";
  readonly event: string;
  readonly payload: unknown;
}

type ProcessListener = (...args: readonly unknown[]) => void;

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

const loggerMock = vi.hoisted(() => {
  const calls: MockLogCall[] = [];
  const createLogger = vi.fn((module: string) => ({
    debug: vi.fn((event: string, payload?: unknown) => {
      calls.push({ module, level: "debug", event, payload });
    }),
    info: vi.fn((event: string, payload?: unknown) => {
      calls.push({ module, level: "info", event, payload });
    }),
    warn: vi.fn((event: string, payload?: unknown) => {
      calls.push({ module, level: "warn", event, payload });
    }),
    error: vi.fn((event: string, payload?: unknown) => {
      calls.push({ module, level: "error", event, payload });
    }),
  }));

  return {
    calls,
    createLogger,
  };
});

const brokerMock = vi.hoisted(() => {
  const instances: MockBrokerSupervisorInstance[] = [];

  class BrokerSupervisor implements MockBrokerSupervisorInstance {
    readonly start = vi.fn<() => void>();
    readonly stop = vi.fn<() => Promise<void>>(() => Promise.resolve());
    readonly getSnapshot = vi.fn<() => MockBrokerSnapshot>(() => ({
      status: "unknown",
      pid: null,
      restartCount: 0,
    }));

    constructor(_config: unknown) {
      instances.push(this);
    }
  }

  return {
    BrokerSupervisor,
    instances,
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

vi.mock("../src/main/broker.ts", () => ({
  BrokerSupervisor: brokerMock.BrokerSupervisor,
}));

vi.mock("../src/main/logger.ts", () => ({
  createLogger: loggerMock.createLogger,
}));

const initialUncaughtExceptionListeners = new Set<ProcessListener>(
  process.listeners("uncaughtException") as ProcessListener[],
);
const initialUnhandledRejectionListeners = new Set<ProcessListener>(
  process.listeners("unhandledRejection") as ProcessListener[],
);

describe("main bootstrap", () => {
  const previousRendererUrl = process.env[RENDERER_URL_ENV_KEY];

  beforeEach(() => {
    vi.resetModules();
    electronMock.instances.length = 0;
    brokerMock.instances.length = 0;
    electronMock.BrowserWindow.getAllWindows.mockClear();
    electronMock.app.isPackaged = false;
    electronMock.app.whenReady.mockClear();
    electronMock.app.on.mockClear();
    electronMock.app.quit.mockClear();
    electronMock.app.exit.mockClear();
    electronMock.showErrorBox.mockClear();
    electronMock.handle.mockClear();
    electronMock.fork.mockClear();
    loggerMock.calls.length = 0;
    loggerMock.createLogger.mockClear();
    cleanupProcessListeners();
    delete process.env[RENDERER_URL_ENV_KEY];
  });

  afterEach(() => {
    cleanupProcessListeners();
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

  it("prevents every before-quit event while broker shutdown is in progress", async () => {
    await importMainBootstrap();

    const beforeQuitHandler = getBeforeQuitHandler();
    const firstQuitEvent = { preventDefault: vi.fn<() => void>() };
    const secondQuitEvent = { preventDefault: vi.fn<() => void>() };

    beforeQuitHandler(firstQuitEvent);
    beforeQuitHandler(secondQuitEvent);

    expect(firstQuitEvent.preventDefault).toHaveBeenCalledTimes(1);
    expect(secondQuitEvent.preventDefault).toHaveBeenCalledTimes(1);
    expect(getOnlyBrokerSupervisor().stop).toHaveBeenCalledTimes(1);
  });

  it("logs uncaught exceptions, unhandled rejections, and gone process signals", async () => {
    await importMainBootstrap();

    getAddedProcessListener(
      "uncaughtException",
      initialUncaughtExceptionListeners,
    )(new Error("main crashed"));
    getAddedProcessListener("unhandledRejection", initialUnhandledRejectionListeners)("rejected");
    getAppHandler("render-process-gone")(
      {},
      {},
      {
        reason: "crashed",
        exitCode: 9,
      },
    );
    getAppHandler("child-process-gone")(
      {},
      {
        type: "Utility",
        reason: "killed",
        exitCode: 15,
        serviceName: "wuphf-broker",
      },
    );

    expect(loggerMock.calls).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          module: "main",
          level: "error",
          event: "uncaught_exception",
          payload: expect.objectContaining({ error: "main crashed" }),
        }),
        {
          module: "main",
          level: "error",
          event: "unhandled_rejection",
          payload: { reason: "rejected" },
        },
        {
          module: "main",
          level: "error",
          event: "renderer_process_gone",
          payload: { reason: "crashed", exitCode: 9 },
        },
        {
          module: "main",
          level: "error",
          event: "child_process_gone",
          payload: {
            processType: "Utility",
            reason: "killed",
            exitCode: 15,
            serviceName: "wuphf-broker",
          },
        },
      ]),
    );
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

function getOnlyBrokerSupervisor(): MockBrokerSupervisorInstance {
  const instance = brokerMock.instances[0];
  if (instance === undefined) {
    throw new Error("Expected BrokerSupervisor to be constructed");
  }
  return instance;
}

function getBeforeQuitHandler(): (event: { readonly preventDefault: () => void }) => void {
  const call = electronMock.app.on.mock.calls.find(([event]) => event === "before-quit");
  if (call === undefined) {
    throw new Error("Expected before-quit handler to be registered");
  }

  const handler = call[1];
  return (event) => {
    handler(event);
  };
}

function getAppHandler(eventName: string): (...args: readonly unknown[]) => void {
  const call = electronMock.app.on.mock.calls.find(([event]) => event === eventName);
  if (call === undefined) {
    throw new Error(`Expected ${eventName} handler to be registered`);
  }

  return call[1];
}

function getAddedProcessListener(
  eventName: "uncaughtException" | "unhandledRejection",
  initialListeners: ReadonlySet<ProcessListener>,
): ProcessListener {
  const listener = processListenersFor(eventName)
    .map((candidate) => candidate as ProcessListener)
    .find((candidate) => !initialListeners.has(candidate));
  if (listener === undefined) {
    throw new Error(`Expected ${eventName} listener to be registered`);
  }

  return listener;
}

function processListenersFor(
  eventName: "uncaughtException" | "unhandledRejection",
): readonly ProcessListener[] {
  if (eventName === "uncaughtException") {
    return process.listeners("uncaughtException") as ProcessListener[];
  }

  return process.listeners("unhandledRejection") as ProcessListener[];
}

function cleanupProcessListeners(): void {
  for (const listener of process.listeners("uncaughtException")) {
    if (!initialUncaughtExceptionListeners.has(listener as ProcessListener)) {
      process.removeListener("uncaughtException", listener);
    }
  }

  for (const listener of process.listeners("unhandledRejection")) {
    if (!initialUnhandledRejectionListeners.has(listener as ProcessListener)) {
      process.removeListener("unhandledRejection", listener);
    }
  }
}
