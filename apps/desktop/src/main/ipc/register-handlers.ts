import { type IpcMainInvokeEvent, ipcMain } from "electron";

import { IPC_CHANNEL_VALUES, IpcChannel, type IpcChannelName } from "../../shared/api-contract.ts";
import type { BrokerSupervisor } from "../broker.ts";
import { handleGetAppVersion } from "./get-app-version.ts";
import { handleGetBrokerStatus } from "./get-broker-status.ts";
import { handleGetPlatform } from "./get-platform.ts";
import { handleOpenExternal } from "./open-external.ts";
import { handleShowItemInFolder } from "./show-item-in-folder.ts";

export type IpcHandler = (event: IpcMainInvokeEvent, request: unknown) => unknown;
export type IpcHandlers = Record<IpcChannelName, IpcHandler>;

export function createIpcHandlers(brokerSupervisor: BrokerSupervisor): IpcHandlers {
  const handlers: IpcHandlers = {
    [IpcChannel.OpenExternal]: handleOpenExternal,
    [IpcChannel.ShowItemInFolder]: handleShowItemInFolder,
    [IpcChannel.GetAppVersion]: handleGetAppVersion,
    [IpcChannel.GetPlatform]: handleGetPlatform,
    [IpcChannel.GetBrokerStatus]: (event, request) =>
      handleGetBrokerStatus(brokerSupervisor, event, request),
  };
  return handlers;
}

export function registerIpcHandlers(brokerSupervisor: BrokerSupervisor): void {
  const handlers = createIpcHandlers(brokerSupervisor);
  const channels = Object.keys(handlers) as readonly IpcChannelName[];
  assertRegisteredChannels(channels);

  ipcMain.handle(IpcChannel.OpenExternal, handlers[IpcChannel.OpenExternal]);
  ipcMain.handle(IpcChannel.ShowItemInFolder, handlers[IpcChannel.ShowItemInFolder]);
  ipcMain.handle(IpcChannel.GetAppVersion, handlers[IpcChannel.GetAppVersion]);
  ipcMain.handle(IpcChannel.GetPlatform, handlers[IpcChannel.GetPlatform]);
  ipcMain.handle(IpcChannel.GetBrokerStatus, handlers[IpcChannel.GetBrokerStatus]);
}

function assertRegisteredChannels(channels: readonly IpcChannelName[]): void {
  const registered = new Set(channels);
  const allowed = new Set(IPC_CHANNEL_VALUES);

  for (const channel of channels) {
    if (!allowed.has(channel)) {
      throw new Error(`Registered IPC channel is not allowlisted: ${channel}`);
    }
  }

  for (const channel of IPC_CHANNEL_VALUES) {
    if (!registered.has(channel)) {
      throw new Error(`Allowlisted IPC channel is missing a handler: ${channel}`);
    }
  }

  if (registered.size !== allowed.size) {
    throw new Error("Registered IPC channels must match the allowlist exactly");
  }
}
