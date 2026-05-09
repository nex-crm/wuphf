import { contextBridge, ipcRenderer } from "electron";

import {
  type GetAppVersionRequest,
  type GetAppVersionResponse,
  type GetBrokerStatusRequest,
  type GetBrokerStatusResponse,
  type GetPlatformRequest,
  type GetPlatformResponse,
  IpcChannel,
  type OpenExternalRequest,
  type OpenExternalResponse,
  type ShowItemInFolderRequest,
  type ShowItemInFolderResponse,
  WUPHF_GLOBAL_KEY,
  type WuphfDesktopApi,
} from "../shared/api-contract.ts";

function invoke<Request, Response>(channel: string, request: Request): Promise<Response> {
  return ipcRenderer.invoke(channel, request) as Promise<Response>;
}

const api: WuphfDesktopApi = {
  openExternal: (request: OpenExternalRequest): Promise<OpenExternalResponse> =>
    invoke<OpenExternalRequest, OpenExternalResponse>(IpcChannel.OpenExternal, request),
  showItemInFolder: (request: ShowItemInFolderRequest): Promise<ShowItemInFolderResponse> =>
    invoke<ShowItemInFolderRequest, ShowItemInFolderResponse>(IpcChannel.ShowItemInFolder, request),
  getAppVersion: (): Promise<GetAppVersionResponse> =>
    invoke<GetAppVersionRequest, GetAppVersionResponse>(IpcChannel.GetAppVersion, {}),
  getPlatform: (): Promise<GetPlatformResponse> =>
    invoke<GetPlatformRequest, GetPlatformResponse>(IpcChannel.GetPlatform, {}),
  getBrokerStatus: (): Promise<GetBrokerStatusResponse> =>
    invoke<GetBrokerStatusRequest, GetBrokerStatusResponse>(IpcChannel.GetBrokerStatus, {}),
};

contextBridge.exposeInMainWorld(WUPHF_GLOBAL_KEY, api);
