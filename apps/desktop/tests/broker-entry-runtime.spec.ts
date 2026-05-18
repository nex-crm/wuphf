import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { BrokerLogger } from "@wuphf/broker";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  DEV_RENDERER_ORIGIN_ENV,
  type DesktopBrokerRuntime,
  RECEIPT_STORE_PATH_ENV,
  RENDERER_DIST_ENV,
  startDesktopBrokerFromEnv,
  WEBAUTHN_STORE_PATH_ENV,
} from "../src/main/broker-entry-runtime.ts";

const sqliteMock = vi.hoisted(() => {
  const stores: Array<{ path: string; closeCalls: number; close(): void }> = [];
  const open = vi.fn((config: { readonly path: string }) => {
    const store = {
      path: config.path,
      closeCalls: 0,
      close(): void {
        this.closeCalls += 1;
      },
    };
    stores.push(store);
    return store;
  });
  return { open, stores };
});

const webauthnMock = vi.hoisted(() => {
  const stores: Array<{
    path: string;
    closeCalls: number;
    savedRegistrationChallenges: Array<{
      readonly role: string;
      readonly issuedToAgentId: string;
    }>;
    startupPruneCalls: number;
    close(): void;
    saveRegistrationChallenge(args: {
      readonly role: string;
      readonly issuedToAgentId: string;
    }): Promise<void>;
    pruneExpired(): Promise<{ readonly consumedTokens: number; readonly orphanChallenges: number }>;
    listCredentialsForAgent(): Promise<readonly []>;
    listCredentialsForAgentRole(): Promise<readonly []>;
    getChallenge(): Promise<null>;
    getCredential(): Promise<null>;
    saveCosignChallenge(): Promise<void>;
    saveCredential(): Promise<void>;
    getConsumedToken(): Promise<null>;
    listSatisfiedRoles(): Promise<readonly []>;
    consumeCosignChallenge(): Promise<never>;
  }> = [];
  const open = vi.fn((config: { readonly path: string }) => {
    const store = {
      path: config.path,
      closeCalls: 0,
      savedRegistrationChallenges: [] as Array<{
        readonly role: string;
        readonly issuedToAgentId: string;
      }>,
      startupPruneCalls: 0,
      close(): void {
        this.closeCalls += 1;
      },
      async saveRegistrationChallenge(args: {
        readonly role: string;
        readonly issuedToAgentId: string;
      }): Promise<void> {
        this.savedRegistrationChallenges.push(args);
      },
      async pruneExpired(): Promise<{
        readonly consumedTokens: number;
        readonly orphanChallenges: number;
      }> {
        this.startupPruneCalls += 1;
        return { consumedTokens: 0, orphanChallenges: 0 };
      },
      async listCredentialsForAgent(): Promise<readonly []> {
        return [];
      },
      async listCredentialsForAgentRole(): Promise<readonly []> {
        return [];
      },
      async getChallenge(): Promise<null> {
        return null;
      },
      async getCredential(): Promise<null> {
        return null;
      },
      async saveCosignChallenge(): Promise<void> {
        throw new Error("cosign challenge storage is not used by this desktop test");
      },
      async saveCredential(): Promise<void> {
        throw new Error("credential storage is not used by this desktop test");
      },
      async getConsumedToken(): Promise<null> {
        return null;
      },
      async listSatisfiedRoles(): Promise<readonly []> {
        return [];
      },
      async consumeCosignChallenge(): Promise<never> {
        throw new Error("cosign storage is not used by this desktop test");
      },
    };
    stores.push(store);
    return store;
  });
  return { open, stores };
});

vi.mock("@wuphf/broker/sqlite", () => ({
  SqliteReceiptStore: {
    open: sqliteMock.open,
  },
}));

vi.mock("@wuphf/broker/webauthn", () => ({
  SqliteWebAuthnStore: {
    open: webauthnMock.open,
  },
  WEBAUTHN_RP_ID: "localhost",
  WEBAUTHN_RP_NAME: "WUPHF",
}));

const logger: BrokerLogger = {
  info: () => undefined,
  warn: () => undefined,
  error: () => undefined,
};

let runtime: DesktopBrokerRuntime | null = null;
let tempDir: string | null = null;

describe("desktop broker entry runtime", () => {
  afterEach(async () => {
    if (runtime !== null) {
      await runtime.close();
      runtime = null;
    }
    if (tempDir !== null) {
      rmSync(tempDir, { recursive: true, force: true });
      tempDir = null;
    }
    sqliteMock.open.mockClear();
    sqliteMock.stores.length = 0;
    webauthnMock.open.mockClear();
    webauthnMock.stores.length = 0;
  });

  it("mounts WebAuthn registration routes with the desktop operator policy", async () => {
    tempDir = mkdtempSync(join(tmpdir(), "wuphf-desktop-webauthn-"));
    const receiptStorePath = join(tempDir, "receipts.sqlite");
    const webauthnStorePath = join(tempDir, "webauthn.sqlite");
    runtime = await startDesktopBrokerFromEnv({
      env: {
        [DEV_RENDERER_ORIGIN_ENV]: "http://localhost:5273",
        [RECEIPT_STORE_PATH_ENV]: receiptStorePath,
        [WEBAUTHN_STORE_PATH_ENV]: webauthnStorePath,
      },
      logger,
    });

    const res = await fetch(`${runtime.broker.url}/api/webauthn/registration/challenge`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${runtime.broker.token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ role: "approver" }),
    });

    expect(res.status).toBe(200);
    const body = (await res.json()) as RegistrationChallengeResponse;
    expect(body.challengeId).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(body.creationOptions.rp.id).toBe("localhost");
    expect(body.creationOptions.user.displayName).toBe("approver for operator");

    expect(sqliteMock.open).toHaveBeenCalledWith({ path: receiptStorePath });
    expect(webauthnMock.open).toHaveBeenCalledWith({ path: webauthnStorePath });
    expect(webauthnMock.stores[0]?.startupPruneCalls).toBe(1);
    expect(webauthnMock.stores[0]?.savedRegistrationChallenges).toEqual([
      expect.objectContaining({
        role: "approver",
        issuedToAgentId: "operator",
      }),
    ]);
  });

  it("starts without optional stores and leaves WebAuthn routes unmounted", async () => {
    runtime = await startDesktopBrokerFromEnv({ env: {}, logger });

    const res = await fetch(`${runtime.broker.url}/api/webauthn/registration/challenge`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${runtime.broker.token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ role: "approver" }),
    });

    expect(res.status).toBe(404);
    expect(sqliteMock.open).not.toHaveBeenCalled();
    expect(webauthnMock.open).not.toHaveBeenCalled();
  });

  it("rejects missing packaged renderer directories before opening stores", async () => {
    await expect(
      startDesktopBrokerFromEnv({
        env: {
          [RENDERER_DIST_ENV]: "/tmp/wuphf-renderer-dist-that-does-not-exist",
          [WEBAUTHN_STORE_PATH_ENV]: "/tmp/webauthn.sqlite",
        },
        logger,
      }),
    ).rejects.toThrow(/renderer dist directory does not exist/);

    expect(sqliteMock.open).not.toHaveBeenCalled();
    expect(webauthnMock.open).not.toHaveBeenCalled();
  });
});

interface RegistrationChallengeResponse {
  readonly challengeId: string;
  readonly creationOptions: {
    readonly rp: {
      readonly id: string;
    };
    readonly user: {
      readonly displayName: string;
    };
  };
}
