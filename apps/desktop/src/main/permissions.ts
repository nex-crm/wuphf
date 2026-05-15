import type { Session, WebContents } from "electron";

import type { Logger } from "./logger.ts";

const WEBAUTHN_PERMISSIONS = new Set(["publickey-credentials-create", "publickey-credentials-get"]);

export interface PermissionPolicyConfig {
  readonly logger?: Logger;
}

export function installSessionPermissionPolicy(
  electronSession: Session,
  config: PermissionPolicyConfig = {},
): void {
  const logger = config.logger;

  // permission is logged through the existing safe `reason` channel: the
  // allowlist intentionally rejects free-form fields, and sender-supplied
  // permission strings would otherwise widen the log surface.
  electronSession.setPermissionRequestHandler((webContents, permission, callback, details) => {
    if (
      WEBAUTHN_PERMISSIONS.has(permission) &&
      isAllowedWebAuthnPermission(permission, requestOrigin(webContents, details), details)
    ) {
      callback(true);
      return;
    }
    logger?.warn("permission_request_denied", { reason: permission });
    callback(false);
  });

  electronSession.setPermissionCheckHandler(
    (webContents, permission, requestingOrigin, details) => {
      if (
        WEBAUTHN_PERMISSIONS.has(permission) &&
        isAllowedWebAuthnPermission(
          permission,
          checkOrigin(webContents, requestingOrigin, details),
          details,
        )
      ) {
        return true;
      }
      logger?.warn("permission_check_denied", { reason: permission });
      return false;
    },
  );
}

function isAllowedWebAuthnPermission(
  permission: string,
  origin: string | null,
  details: unknown,
): boolean {
  return (
    WEBAUTHN_PERMISSIONS.has(permission) &&
    origin !== null &&
    isLoopbackHttpOrigin(origin) &&
    isMainFrame(details)
  );
}

function requestOrigin(webContents: WebContents | null, details: unknown): string | null {
  const detailsOrigin = originFromUrl(recordString(details, "requestingUrl"));
  if (detailsOrigin !== null) return detailsOrigin;
  if (webContents === null) return null;
  return originFromUrl(webContents.getURL());
}

function checkOrigin(
  webContents: WebContents | null,
  requestingOrigin: string,
  details: unknown,
): string | null {
  const explicitOrigin = originFromUrl(requestingOrigin);
  if (explicitOrigin !== null) return explicitOrigin;
  const detailsOrigin = originFromUrl(recordString(details, "requestingUrl"));
  if (detailsOrigin !== null) return detailsOrigin;
  if (webContents === null) return null;
  return originFromUrl(webContents.getURL());
}

function originFromUrl(value: string | undefined): string | null {
  if (value === undefined || value.length === 0) return null;
  try {
    return new URL(value).origin;
  } catch {
    return null;
  }
}

function isLoopbackHttpOrigin(origin: string): boolean {
  try {
    const parsed = new URL(origin);
    return (
      parsed.protocol === "http:" &&
      parsed.port.length > 0 &&
      (parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost")
    );
  } catch {
    return false;
  }
}

function isMainFrame(details: unknown): boolean {
  const value = recordBoolean(details, "isMainFrame");
  return value !== false;
}

function recordString(value: unknown, key: string): string | undefined {
  if (!isRecord(value)) return undefined;
  const field = value[key];
  return typeof field === "string" ? field : undefined;
}

function recordBoolean(value: unknown, key: string): boolean | undefined {
  if (!isRecord(value)) return undefined;
  const field = value[key];
  return typeof field === "boolean" ? field : undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
