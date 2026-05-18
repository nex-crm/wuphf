import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { BrokerLogger } from "@wuphf/broker";
import { afterEach, describe, expect, it } from "vitest";

import {
  type DesktopBrokerRuntime,
  startDesktopBrokerFromEnv,
  WEBAUTHN_STORE_PATH_ENV,
} from "../src/main/broker-entry-runtime.ts";

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
  });

  it("mounts WebAuthn registration routes with the desktop operator policy", async () => {
    tempDir = mkdtempSync(join(tmpdir(), "wuphf-desktop-webauthn-"));
    runtime = await startDesktopBrokerFromEnv({
      env: {
        [WEBAUTHN_STORE_PATH_ENV]: join(tempDir, "webauthn.sqlite"),
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
