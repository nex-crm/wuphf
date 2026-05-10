import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  IPC_CHANNEL_VALUES,
  IpcChannel,
  type IpcChannelName,
  WUPHF_GLOBAL_KEY,
  type WuphfDesktopApi,
} from "../src/shared/api-contract.ts";

type PreloadVerb = keyof WuphfDesktopApi;

interface PreloadVerbCase {
  readonly verb: PreloadVerb;
  readonly channel: IpcChannelName;
  readonly request: unknown;
  readonly response: unknown;
  readonly invoke: (api: WuphfDesktopApi) => Promise<unknown>;
}

const PRELOAD_VERBS = [
  "openExternal",
  "showItemInFolder",
  "getAppVersion",
  "getPlatform",
  "getBrokerStatus",
] satisfies readonly PreloadVerb[];

const APP_DATA_MARKERS = [
  "~/.wuphf",
  "receipt",
  "receipts",
  "projection",
  "projections",
  "broker-token",
  "brokerToken",
] as const;

const electronMock = vi.hoisted(() => ({
  exposeInMainWorld: vi.fn<(key: string, api: unknown) => void>(),
  invoke: vi.fn<(channel: string, request: unknown) => Promise<unknown>>(),
}));

vi.mock("electron", () => ({
  contextBridge: {
    exposeInMainWorld: electronMock.exposeInMainWorld,
  },
  ipcRenderer: {
    invoke: electronMock.invoke,
  },
}));

describe("preload allowlist", () => {
  beforeEach(() => {
    vi.resetModules();
    electronMock.exposeInMainWorld.mockClear();
    electronMock.invoke.mockReset();
    electronMock.invoke.mockResolvedValue({});
  });

  it("exposes only the wuphf global", async () => {
    await import("../src/preload/preload.ts");

    expect(electronMock.exposeInMainWorld).toHaveBeenCalledTimes(1);
    const exposeCall = electronMock.exposeInMainWorld.mock.calls[0];
    if (exposeCall === undefined) {
      throw new Error("Expected contextBridge.exposeInMainWorld to be called");
    }

    expect(exposeCall[0]).toBe(WUPHF_GLOBAL_KEY);
    expect(Object.keys(exposeCall[1] as Record<string, unknown>).sort()).toEqual(
      [...PRELOAD_VERBS].sort(),
    );
  });

  it.each(PRELOAD_VERBS)("exposes %s as a function", async (verb) => {
    const api = await loadPreloadApi();

    expect(api[verb]).toEqual(expect.any(Function));
  });

  it.each(
    createPreloadVerbCases(),
  )("$verb behavior uses the matching IPC channel", async (testCase) => {
    const api = await loadPreloadApi();
    electronMock.invoke.mockResolvedValueOnce(testCase.response);

    await expect(testCase.invoke(api)).resolves.toEqual(testCase.response);

    expect(electronMock.invoke).toHaveBeenCalledWith(testCase.channel, testCase.request);
  });

  it.each(
    createPreloadVerbCases(),
  )("$verb security does not request app data", async (testCase) => {
    const api = await loadPreloadApi();
    await testCase.invoke(api);

    const invokeCall = electronMock.invoke.mock.calls[0];
    if (invokeCall === undefined) {
      throw new Error("Expected preload verb to call ipcRenderer.invoke");
    }

    const serialized = JSON.stringify(invokeCall) ?? "";
    for (const marker of APP_DATA_MARKERS) {
      expect(serialized.includes(marker), `${testCase.verb} must not request ${marker}`).toBe(
        false,
      );
    }
  });

  it("wires every allowlisted channel through ipcRenderer.invoke", async () => {
    const api = await loadPreloadApi();
    const wiredChannels: string[] = [];

    for (const testCase of createPreloadVerbCases()) {
      electronMock.invoke.mockClear();
      await testCase.invoke(api);
      expect(electronMock.invoke).toHaveBeenCalledWith(testCase.channel, testCase.request);
      wiredChannels.push(testCase.channel);
    }

    expect(wiredChannels.sort()).toEqual([...IPC_CHANNEL_VALUES].sort());
  });
});

async function loadPreloadApi(): Promise<WuphfDesktopApi> {
  await import("../src/preload/preload.ts");
  const exposeCall = electronMock.exposeInMainWorld.mock.calls[0];
  if (exposeCall === undefined) {
    throw new Error("Expected preload to expose an API");
  }
  return exposeCall[1] as WuphfDesktopApi;
}

function createPreloadVerbCases(): readonly PreloadVerbCase[] {
  return [
    {
      verb: "openExternal",
      channel: IpcChannel.OpenExternal,
      request: { url: "https://example.com" },
      response: { ok: true },
      invoke: (api) => api.openExternal({ url: "https://example.com" }),
    },
    {
      verb: "showItemInFolder",
      channel: IpcChannel.ShowItemInFolder,
      request: { path: "/tmp/wuphf.txt" },
      response: { ok: true },
      invoke: (api) => api.showItemInFolder({ path: "/tmp/wuphf.txt" }),
    },
    {
      verb: "getAppVersion",
      channel: IpcChannel.GetAppVersion,
      request: {},
      response: { version: "0.0.0-test" },
      invoke: (api) => api.getAppVersion(),
    },
    {
      verb: "getPlatform",
      channel: IpcChannel.GetPlatform,
      request: {},
      response: { platform: "darwin", arch: "arm64" },
      invoke: (api) => api.getPlatform(),
    },
    {
      verb: "getBrokerStatus",
      channel: IpcChannel.GetBrokerStatus,
      request: {},
      response: { status: "alive", pid: 1234, restartCount: 0 },
      invoke: (api) => api.getBrokerStatus(),
    },
  ];
}
