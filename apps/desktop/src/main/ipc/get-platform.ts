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
    platform: narrowPlatform(process.platform),
    arch: process.arch,
  };
}

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
