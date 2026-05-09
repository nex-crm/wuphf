import { app, type IpcMainInvokeEvent } from "electron";

import type { ErrResponse, GetAppVersionResponse } from "../../shared/api-contract.ts";
import { invalidRequest, validateEmptyRequest } from "./_guards.ts";

export function handleGetAppVersion(
  _event: IpcMainInvokeEvent,
  request: unknown,
): GetAppVersionResponse | ErrResponse {
  const validation = validateEmptyRequest(request, "getAppVersion");
  if (!validation.valid) {
    return invalidRequest(validation.error);
  }

  return { version: app.getVersion() };
}
