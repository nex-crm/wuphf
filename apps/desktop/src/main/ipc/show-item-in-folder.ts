import nodePath from "node:path";
import { type IpcMainInvokeEvent, shell } from "electron";

import {
  errResponse,
  okResponse,
  type ShowItemInFolderResponse,
} from "../../shared/api-contract.ts";
import { assertMaxStringLength, invalidRequest, isExactObject } from "./_guards.ts";

const MAX_PATH_BYTES = 32_768;

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

  const sizeValidation = assertMaxStringLength(request.path, MAX_PATH_BYTES, "path");
  if (!sizeValidation.valid) {
    return invalidRequest(sizeValidation.error);
  }

  const normalizedPath = nodePath.normalize(request.path);
  if (request.path.includes("\0") || normalizedPath.includes("\0")) {
    return errResponse("Path must not contain NUL bytes");
  }

  if (!nodePath.isAbsolute(normalizedPath)) {
    return errResponse("Path must be absolute");
  }

  if (hasParentTraversalSegment(request.path) || hasParentTraversalSegment(normalizedPath)) {
    return errResponse("Path must not contain parent traversal segments");
  }

  try {
    // Electron returns void here; only synchronous OS/shell failures can be surfaced.
    shell.showItemInFolder(normalizedPath);
    return okResponse();
  } catch (error) {
    return errResponse(
      error instanceof Error ? error.message : "Failed to reveal path in OS file manager",
    );
  }
}

function isShowItemInFolderRequest(request: unknown): request is ValidShowItemInFolderRequest {
  return (
    isExactObject(request, ["path"]) &&
    typeof (request as { readonly path?: unknown }).path === "string"
  );
}

function hasParentTraversalSegment(value: string): boolean {
  return value.split(/[\\/]+/).some((segment) => segment === "..");
}
