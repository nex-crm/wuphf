import nodePath from "node:path";
import { type IpcMainInvokeEvent, shell } from "electron";

import {
  errResponse,
  IpcChannel,
  okResponse,
  type ShowItemInFolderResponse,
} from "../../shared/api-contract.ts";
import type { Logger, LogPayload } from "../logger.ts";
import { assertMaxStringLength, invalidRequest, isExactObject } from "./_guards.ts";
import { logIpcPayloadRejected } from "./_logging.ts";

const MAX_PATH_BYTES = 32_768;

interface ValidShowItemInFolderRequest {
  readonly path: string;
}

export interface ShowItemInFolderHandlerOptions {
  readonly logger?: Logger;
}

export function createShowItemInFolderHandler(
  options: ShowItemInFolderHandlerOptions = {},
): (event: IpcMainInvokeEvent, request: unknown) => ShowItemInFolderResponse {
  return function showItemInFolderHandler(
    _event: IpcMainInvokeEvent,
    request: unknown,
  ): ShowItemInFolderResponse {
    if (!isShowItemInFolderRequest(request)) {
      logRejection(options.logger, "invalid_request");
      return invalidRequest("showItemInFolder expects exactly one string field: path");
    }

    const sizeValidation = assertMaxStringLength(request.path, MAX_PATH_BYTES, "path");
    if (!sizeValidation.valid) {
      logRejection(options.logger, "oversized_path", {
        payloadBytes: Buffer.byteLength(request.path, "utf8"),
      });
      return invalidRequest(sizeValidation.error);
    }

    const normalizedPath = nodePath.normalize(request.path);
    if (request.path.includes("\0") || normalizedPath.includes("\0")) {
      logRejection(options.logger, "nul_byte");
      return errResponse("Path must not contain NUL bytes");
    }

    // Reject Windows network and device paths BEFORE the absolute-path
    // check, because POSIX `path.isAbsolute()` does not recognize
    // `\\server\share` or `\\?\UNC\...` and would otherwise reject them
    // with the wrong reason. The payload is renderer-controlled and the
    // production app ships for Windows from the same release pipeline,
    // so this guard runs unconditionally rather than gating on the host
    // OS where the IPC happens to be processed: `\\server\share\file`
    // (NTLM credential leak via SMB / relay), `\\?\UNC\...` (DOS-device
    // long-path UNC), and `\\.\Device` (raw device) all match the same
    // leading double-separator pattern.
    if (
      isWindowsNetworkOrDevicePath(request.path) ||
      isWindowsNetworkOrDevicePath(normalizedPath)
    ) {
      logRejection(options.logger, "windows_unc_or_device_path");
      return errResponse("Path must not be a Windows network or device path");
    }

    if (!nodePath.isAbsolute(normalizedPath)) {
      logRejection(options.logger, "non_absolute_path");
      return errResponse("Path must be absolute");
    }

    if (hasParentTraversalSegment(request.path) || hasParentTraversalSegment(normalizedPath)) {
      logRejection(options.logger, "parent_traversal");
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
  };
}

export const handleShowItemInFolder = createShowItemInFolderHandler();

function isShowItemInFolderRequest(request: unknown): request is ValidShowItemInFolderRequest {
  return (
    isExactObject(request, ["path"]) &&
    typeof (request as { readonly path?: unknown }).path === "string"
  );
}

function hasParentTraversalSegment(value: string): boolean {
  return value.split(/[\\/]+/).some((segment) => segment === "..");
}

function isWindowsNetworkOrDevicePath(value: string): boolean {
  // Match \\server\share, //server/share, \\?\UNC\..., \\.\Device, etc.
  // A leading double-separator on Windows always means UNC or DOS-device.
  return /^[\\/]{2}/.test(value);
}

function logRejection(logger: Logger | undefined, reason: string, payload: LogPayload = {}): void {
  logIpcPayloadRejected(logger, IpcChannel.ShowItemInFolder, reason, payload);
}
