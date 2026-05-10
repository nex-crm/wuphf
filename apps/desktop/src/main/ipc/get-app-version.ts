import { app, type IpcMainInvokeEvent } from "electron";

import {
  type ErrResponse,
  type GetAppVersionResponse,
  IpcChannel,
} from "../../shared/api-contract.ts";
import type { Logger } from "../logger.ts";
import { invalidRequest, validateEmptyRequest } from "./_guards.ts";
import { logIpcPayloadRejected } from "./_logging.ts";

export interface GetAppVersionHandlerOptions {
  readonly logger?: Logger;
}

export function createGetAppVersionHandler(
  options: GetAppVersionHandlerOptions = {},
): (event: IpcMainInvokeEvent, request: unknown) => GetAppVersionResponse | ErrResponse {
  return function getAppVersionHandler(
    _event: IpcMainInvokeEvent,
    request: unknown,
  ): GetAppVersionResponse | ErrResponse {
    const validation = validateEmptyRequest(request, "getAppVersion");
    if (!validation.valid) {
      logIpcPayloadRejected(options.logger, IpcChannel.GetAppVersion, "invalid_request");
      return invalidRequest(validation.error);
    }

    return { version: app.getVersion() };
  };
}

export const handleGetAppVersion = createGetAppVersionHandler();
