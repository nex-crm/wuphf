import type { IpcMainInvokeEvent } from "electron";

import {
  type DesktopPlatform,
  type ErrResponse,
  type GetPlatformResponse,
  IpcChannel,
} from "../../shared/api-contract.ts";
import type { Logger } from "../logger.ts";
import { invalidRequest, validateEmptyRequest } from "./_guards.ts";
import { logIpcPayloadRejected } from "./_logging.ts";

export interface GetPlatformHandlerOptions {
  readonly logger?: Logger;
}

export function createGetPlatformHandler(
  options: GetPlatformHandlerOptions = {},
): (event: IpcMainInvokeEvent, request: unknown) => GetPlatformResponse | ErrResponse {
  return function getPlatformHandler(
    _event: IpcMainInvokeEvent,
    request: unknown,
  ): GetPlatformResponse | ErrResponse {
    const validation = validateEmptyRequest(request, "getPlatform");
    if (!validation.valid) {
      logIpcPayloadRejected(options.logger, IpcChannel.GetPlatform, "invalid_request");
      return invalidRequest(validation.error);
    }

    return {
      platform: narrowPlatform(process.platform),
      arch: process.arch,
    };
  };
}

export const handleGetPlatform = createGetPlatformHandler();

export function narrowPlatform(platform: NodeJS.Platform): DesktopPlatform {
  switch (platform) {
    case "aix":
    case "android":
    case "darwin":
    case "freebsd":
    case "haiku":
    case "linux":
    case "openbsd":
    case "sunos":
    case "win32":
    case "cygwin":
    case "netbsd":
      return platform;
    default: {
      const exhaustive: never = platform;
      throw new Error(`Unknown platform: ${String(exhaustive)}`);
    }
  }
}
