import type { IpcMainInvokeEvent } from "electron";

import type { DesktopPlatform, GetPlatformResponse } from "../../shared/api-contract.ts";
import { assertEmptyRequest } from "./_guards.ts";

export function handleGetPlatform(
  _event: IpcMainInvokeEvent,
  request: unknown,
): GetPlatformResponse {
  assertEmptyRequest(request, "getPlatform");
  return {
    platform: process.platform as DesktopPlatform,
    arch: process.arch,
  };
}
