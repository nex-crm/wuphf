import { type IpcMainInvokeEvent, ipcMain } from "electron";

import { IPC_CHANNEL_VALUES, IpcChannel, type IpcChannelName } from "../../shared/api-contract.ts";
import type { Logger } from "../logger.ts";
import { createGetAppVersionHandler } from "./get-app-version.ts";
import { type BrokerStatusProvider, createGetBrokerStatusHandler } from "./get-broker-status.ts";
import { createGetPlatformHandler } from "./get-platform.ts";
import { createOpenExternalHandler } from "./open-external.ts";
import { createShowItemInFolderHandler } from "./show-item-in-folder.ts";

export type IpcHandler = (event: IpcMainInvokeEvent, request: unknown) => unknown;
export type IpcHandlers = Record<IpcChannelName, IpcHandler>;

export interface IpcRegistrationOptions {
  readonly logger?: Logger;
}

export function createIpcHandlers(
  brokerStatusProvider: BrokerStatusProvider,
  options: IpcRegistrationOptions = {},
): IpcHandlers {
  const handlerOptions = options.logger === undefined ? {} : { logger: options.logger };
  const handlers: IpcHandlers = {
    [IpcChannel.OpenExternal]: createOpenExternalHandler(handlerOptions),
    [IpcChannel.ShowItemInFolder]: createShowItemInFolderHandler(handlerOptions),
    [IpcChannel.GetAppVersion]: createGetAppVersionHandler(handlerOptions),
    [IpcChannel.GetPlatform]: createGetPlatformHandler(handlerOptions),
    [IpcChannel.GetBrokerStatus]: createGetBrokerStatusHandler(
      brokerStatusProvider,
      handlerOptions,
    ),
  };
  return handlers;
}

export function registerIpcHandlers(
  brokerStatusProvider: BrokerStatusProvider,
  options: IpcRegistrationOptions = {},
): void {
  const handlers = createIpcHandlers(brokerStatusProvider, options);
  const channels = Object.keys(handlers) as readonly IpcChannelName[];
  assertRegisteredChannels(channels);

  ipcMain.handle(IpcChannel.OpenExternal, handlers[IpcChannel.OpenExternal]);
  ipcMain.handle(IpcChannel.ShowItemInFolder, handlers[IpcChannel.ShowItemInFolder]);
  ipcMain.handle(IpcChannel.GetAppVersion, handlers[IpcChannel.GetAppVersion]);
  ipcMain.handle(IpcChannel.GetPlatform, handlers[IpcChannel.GetPlatform]);
  ipcMain.handle(IpcChannel.GetBrokerStatus, handlers[IpcChannel.GetBrokerStatus]);
}

export function assertRegisteredChannels(channels: readonly IpcChannelName[]): void {
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
}
