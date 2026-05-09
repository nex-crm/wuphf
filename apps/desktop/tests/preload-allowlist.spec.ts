import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  IPC_CHANNEL_VALUES,
  IpcChannel,
  WUPHF_GLOBAL_KEY,
  type WuphfDesktopApi,
} from "../src/shared/api-contract.ts";

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
    expect(Object.keys(exposeCall[1] as Record<string, unknown>).sort()).toEqual([
      "getAppVersion",
      "getBrokerStatus",
      "getPlatform",
      "openExternal",
      "showItemInFolder",
    ]);
  });

  it("wires every allowlisted channel through ipcRenderer.invoke", async () => {
    const api = await loadPreloadApi();
    const calls: ReadonlyArray<{
      readonly invoke: () => Promise<unknown>;
      readonly channel: string;
      readonly request: unknown;
    }> = [
      {
        invoke: () => api.openExternal({ url: "https://example.com" }),
        channel: IpcChannel.OpenExternal,
        request: { url: "https://example.com" },
      },
      {
        invoke: () => api.showItemInFolder({ path: "/tmp/wuphf.txt" }),
        channel: IpcChannel.ShowItemInFolder,
        request: { path: "/tmp/wuphf.txt" },
      },
      {
        invoke: () => api.getAppVersion(),
        channel: IpcChannel.GetAppVersion,
        request: {},
      },
      {
        invoke: () => api.getPlatform(),
        channel: IpcChannel.GetPlatform,
        request: {},
      },
      {
        invoke: () => api.getBrokerStatus(),
        channel: IpcChannel.GetBrokerStatus,
        request: {},
      },
    ];

    const wiredChannels: string[] = [];
    for (const call of calls) {
      electronMock.invoke.mockClear();
      await call.invoke();
      expect(electronMock.invoke).toHaveBeenCalledWith(call.channel, call.request);
      wiredChannels.push(call.channel);
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
