import { ipcMain } from "electron";

import { IPC_CHANNEL_VALUES, IpcChannel, type IpcChannelName } from "../../shared/api-contract.ts";
import type { BrokerSupervisor } from "../broker.ts";
import { handleGetAppVersion } from "./get-app-version.ts";
import { handleGetBrokerStatus } from "./get-broker-status.ts";
import { handleGetPlatform } from "./get-platform.ts";
import { handleOpenExternal } from "./open-external.ts";
import { handleShowItemInFolder } from "./show-item-in-folder.ts";

export const REGISTERED_IPC_CHANNELS = [
  IpcChannel.OpenExternal,
  IpcChannel.ShowItemInFolder,
  IpcChannel.GetAppVersion,
  IpcChannel.GetPlatform,
  IpcChannel.GetBrokerStatus,
] as const satisfies readonly IpcChannelName[];

assertRegisteredChannels(REGISTERED_IPC_CHANNELS);

export function registerIpcHandlers(brokerSupervisor: BrokerSupervisor): void {
  ipcMain.handle(IpcChannel.OpenExternal, handleOpenExternal);
  ipcMain.handle(IpcChannel.ShowItemInFolder, handleShowItemInFolder);
  ipcMain.handle(IpcChannel.GetAppVersion, handleGetAppVersion);
  ipcMain.handle(IpcChannel.GetPlatform, handleGetPlatform);
  ipcMain.handle(IpcChannel.GetBrokerStatus, (event, request) =>
    handleGetBrokerStatus(brokerSupervisor, event, request),
  );
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
