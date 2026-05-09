import nodePath from "node:path";
import { type IpcMainInvokeEvent, shell } from "electron";

import type { ShowItemInFolderResponse } from "../../shared/api-contract.ts";
import { invalidRequest, isExactObject } from "./_guards.ts";

interface ValidShowItemInFolderRequest {
  readonly path: string;
}

export function handleShowItemInFolder(
  _event: IpcMainInvokeEvent,
  request: unknown,
): ShowItemInFolderResponse {
  if (!isShowItemInFolderRequest(request)) {
    return invalidRequest("showItemInFolder expects exactly one string field: path");
  }

  if (!nodePath.isAbsolute(request.path)) {
    return { ok: false, error: "Path must be absolute" };
  }

  shell.showItemInFolder(request.path);
  return { ok: true };
}

function isShowItemInFolderRequest(request: unknown): request is ValidShowItemInFolderRequest {
  return (
    isExactObject(request, ["path"]) &&
    typeof (request as { readonly path?: unknown }).path === "string"
  );
}
