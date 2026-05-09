import { describe, expect, it, vi } from "vitest";

import { IPC_CHANNEL_VALUES } from "../src/shared/api-contract.ts";

const electronMock = vi.hoisted(() => ({
  handle: vi.fn<(channel: string, handler: unknown) => void>(),
  openExternal: vi.fn<(url: string) => Promise<void>>(() => Promise.resolve()),
  showItemInFolder: vi.fn<(path: string) => void>(),
  getVersion: vi.fn<() => string>(() => "0.0.0-test"),
}));

vi.mock("electron", () => ({
  ipcMain: {
    handle: electronMock.handle,
  },
  shell: {
    openExternal: electronMock.openExternal,
    showItemInFolder: electronMock.showItemInFolder,
  },
  app: {
    getVersion: electronMock.getVersion,
  },
  utilityProcess: {
    fork: vi.fn(),
  },
}));

describe("IPC contract coverage", () => {
  it("registerIpcHandlers callees match IPC_CHANNEL_VALUES exactly", async () => {
    const { BrokerSupervisor } = await import("../src/main/broker.ts");
    const { REGISTERED_IPC_CHANNELS, registerIpcHandlers } = await import(
      "../src/main/ipc/register-handlers.ts"
    );
    const brokerSupervisor = new BrokerSupervisor({ brokerEntryPath: "/tmp/broker-stub.js" });

    registerIpcHandlers(brokerSupervisor);

    const handledChannels = electronMock.handle.mock.calls.map(([channel]) => channel);
    expect(handledChannels.sort()).toEqual([...IPC_CHANNEL_VALUES].sort());
    expect([...REGISTERED_IPC_CHANNELS].sort()).toEqual([...IPC_CHANNEL_VALUES].sort());
  });
});
