import { app, type IpcMainInvokeEvent } from "electron";

import type { GetAppVersionResponse } from "../../shared/api-contract.ts";
import { assertEmptyRequest } from "./_guards.ts";

export function handleGetAppVersion(
  _event: IpcMainInvokeEvent,
  request: unknown,
): GetAppVersionResponse {
  assertEmptyRequest(request, "getAppVersion");
  return { version: app.getVersion() };
}
