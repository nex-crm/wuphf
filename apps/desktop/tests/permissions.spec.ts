import type { Session, WebContents } from "electron";
import { describe, expect, it, vi } from "vitest";

import { installSessionPermissionPolicy } from "../src/main/permissions.ts";

type ElectronPermissionRequestHandler = Parameters<Session["setPermissionRequestHandler"]>[0];
type ElectronPermissionCheckHandler = Parameters<Session["setPermissionCheckHandler"]>[0];
type CapturedPermissionRequestHandler = (
  webContents: WebContents | null,
  permission: string,
  callback: (granted: boolean) => void,
  details?: unknown,
) => void;
type CapturedPermissionCheckHandler = (
  webContents: WebContents | null,
  permission: string,
  requestingOrigin: string,
  details?: unknown,
) => boolean;
type TestLogPayload = Readonly<Record<string, string | number | boolean | null>>;

describe("installSessionPermissionPolicy", () => {
  it("falls back through details, webContents URL, and null origins for requests", () => {
    const harness = createPermissionHarness();
    const logger = createLogger();

    installSessionPermissionPolicy(harness.session, { logger });

    const detailsGrant = vi.fn<(granted: boolean) => void>();
    harness.requestHandler(
      null,
      "publickey-credentials-create",
      detailsGrant,
      requestDetails("http://localhost:5273/passkey"),
    );
    expect(detailsGrant).toHaveBeenCalledWith(true);

    const webContentsGrant = vi.fn<(granted: boolean) => void>();
    harness.requestHandler(
      webContentsWithUrl("http://127.0.0.1:7891/"),
      "publickey-credentials-get",
      webContentsGrant,
      requestDetails("%%%"),
    );
    expect(webContentsGrant).toHaveBeenCalledWith(true);

    const nullOriginGrant = vi.fn<(granted: boolean) => void>();
    harness.requestHandler(null, "publickey-credentials-get", nullOriginGrant, {});
    expect(nullOriginGrant).toHaveBeenCalledWith(false);
    expect(logger.warn).toHaveBeenCalledWith("permission_request_denied", {
      reason: "publickey-credentials-get",
    });
  });

  it("requires WebAuthn checks to be main-frame loopback HTTP origins", () => {
    const harness = createPermissionHarness();
    const logger = createLogger();

    installSessionPermissionPolicy(harness.session, { logger });

    expect(
      harness.checkHandler(
        null,
        "publickey-credentials-get",
        "",
        requestDetails("http://localhost:5273/passkey"),
      ),
    ).toBe(true);
    expect(
      harness.checkHandler(
        webContentsWithUrl("http://127.0.0.1:7891/"),
        "publickey-credentials-get",
        "not a url",
        {},
      ),
    ).toBe(true);
    expect(
      harness.checkHandler(null, "publickey-credentials-get", "", {
        isMainFrame: false,
        requestingUrl: "http://localhost:5273/passkey",
      }),
    ).toBe(false);
    expect(harness.checkHandler(null, "publickey-credentials-create", "", {})).toBe(false);
    expect(harness.checkHandler(null, "clipboard-read", "http://localhost:5273", {})).toBe(false);

    expect(logger.warn).toHaveBeenCalledWith("permission_check_denied", {
      reason: "publickey-credentials-get",
    });
    expect(logger.warn).toHaveBeenCalledWith("permission_check_denied", {
      reason: "publickey-credentials-create",
    });
    expect(logger.warn).toHaveBeenCalledWith("permission_check_denied", {
      reason: "clipboard-read",
    });
  });
});

function createPermissionHarness(): {
  readonly session: Session;
  requestHandler: CapturedPermissionRequestHandler;
  checkHandler: CapturedPermissionCheckHandler;
} {
  let requestHandler: CapturedPermissionRequestHandler | null = null;
  let checkHandler: CapturedPermissionCheckHandler | null = null;
  const session = {
    setPermissionRequestHandler(handler: ElectronPermissionRequestHandler): void {
      requestHandler = handler as unknown as CapturedPermissionRequestHandler;
    },
    setPermissionCheckHandler(handler: ElectronPermissionCheckHandler): void {
      checkHandler = handler as unknown as CapturedPermissionCheckHandler;
    },
  } as unknown as Session;

  return {
    session,
    get requestHandler(): CapturedPermissionRequestHandler {
      if (requestHandler === null) {
        throw new Error("permission request handler was not installed");
      }
      return requestHandler;
    },
    get checkHandler(): CapturedPermissionCheckHandler {
      if (checkHandler === null) {
        throw new Error("permission check handler was not installed");
      }
      return checkHandler;
    },
  };
}

function createLogger() {
  return {
    info: vi.fn<(event: string, payload?: TestLogPayload) => void>(),
    warn: vi.fn<(event: string, payload?: TestLogPayload) => void>(),
    error: vi.fn<(event: string, payload?: TestLogPayload) => void>(),
  };
}

function webContentsWithUrl(url: string): WebContents {
  return {
    getURL: () => url,
  } as unknown as WebContents;
}

function requestDetails(requestingUrl: string): { readonly requestingUrl: string } {
  return { requestingUrl };
}
