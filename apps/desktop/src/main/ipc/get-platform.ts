import type { IpcMainInvokeEvent } from "electron";

import type { DesktopPlatform, ErrResponse, GetPlatformResponse } from "../../shared/api-contract.ts";
import { invalidRequest, validateEmptyRequest } from "./_guards.ts";

export function handleGetPlatform(
  _event: IpcMainInvokeEvent,
  request: unknown,
): GetPlatformResponse | ErrResponse {
  const validation = validateEmptyRequest(request, "getPlatform");
  if (!validation.valid) {
    return invalidRequest(validation.error);
  }

  return {
    platform: process.platform as DesktopPlatform,
    arch: process.arch,
  };
}
