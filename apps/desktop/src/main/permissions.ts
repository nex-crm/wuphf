import type { Session } from "electron";

import type { Logger } from "./logger.ts";

export interface PermissionDenyConfig {
  readonly logger?: Logger;
}

export function installSessionPermissionDenyAll(
  electronSession: Session,
  config: PermissionDenyConfig = {},
): void {
  const logger = config.logger;

  // permission is logged through the existing safe `reason` channel: the
  // allowlist intentionally rejects free-form fields, and sender-supplied
  // permission strings would otherwise widen the log surface.
  electronSession.setPermissionRequestHandler((_webContents, permission, callback) => {
    logger?.warn("permission_request_denied", { reason: permission });
    callback(false);
  });

  electronSession.setPermissionCheckHandler((_webContents, permission) => {
    logger?.warn("permission_check_denied", { reason: permission });
    return false;
  });
}
