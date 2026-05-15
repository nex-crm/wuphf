import { type IncomingHttpHeaders, type OutgoingHttpHeaders, request } from "node:http";

import type {
  AuthenticationResponseJSON,
  PublicKeyCredentialCreationOptionsJSON,
  PublicKeyCredentialRequestOptionsJSON,
  RegistrationResponseJSON,
  WebAuthnCredential,
} from "@simplewebauthn/server";
import {
  type AgentId,
  type ApiToken,
  type ApprovalRole,
  approvalClaimToJsonValue,
  approvalScopeToJsonValue,
  asAgentId,
  asApiToken,
  asApprovalClaimId,
  asReceiptId,
  asSha256Hex,
  type ReceiptCoSignClaim,
  type ReceiptCoSignScope,
  signedApprovalTokenFromJson,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";
import { afterEach, describe, expect, it } from "vitest";

import { openDatabase, runMigrations } from "../../src/event-log/index.ts";
import { type BrokerHandle, createBroker } from "../../src/index.ts";
import { createWebAuthnStore } from "../../src/webauthn/store.ts";
import type {
  Clock,
  RegisteredWebAuthnCredentialVerification,
  WebAuthnAuthenticationVerification,
  WebAuthnCeremony,
} from "../../src/webauthn/types.ts";

const token = asApiToken("test-token-with-enough-entropy-AAAAAAAAA");
const agentId = asAgentId("agent_alpha");
const otherAgentId = asAgentId("agent_beta");

let broker: BrokerHandle | null = null;
let db: Database.Database | null = null;

describe("WebAuthn routes", () => {
  afterEach(async () => {
    if (broker !== null) {
      await broker.stop();
      broker = null;
    }
    if (db !== null) {
      db.close();
      db = null;
    }
  });

  it.each([
    "/api/webauthn/registration/challenge",
    "/api/webauthn/registration/verify",
    "/api/webauthn/cosign/challenge",
    "/api/webauthn/cosign/verify",
  ])("requires bearer auth for POST %s", async (path) => {
    const handle = await startBroker();

    const res = await fetch(`${handle.url}${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });

    expect(res.status).toBe(401);
  });

  it.each([
    "/api/webauthn/registration/challenge",
    "/api/webauthn/registration/verify",
    "/api/webauthn/cosign/challenge",
    "/api/webauthn/cosign/verify",
  ])("inherits the loopback DNS-rebinding guard for POST %s", async (path) => {
    const handle = await startBroker();

    const res = await rawRequest({
      port: handle.port,
      path,
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        Host: "evil.example.com",
        "Content-Type": "application/json",
      },
      body: "{}",
    });

    expect(res.status).toBe(403);
    expect(res.body).toMatch(/^loopback_/);
  });

  it("starts a standalone registration challenge", async () => {
    const handle = await startBroker();

    const res = await postJson(handle, "/api/webauthn/registration/challenge", {
      role: "approver",
    });

    expect(res.status).toBe(200);
    const body = (await res.json()) as RegistrationChallengeResponse;
    expect(body.challengeId).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(body.options.rp.id).toBe("localhost");
    expect(body.options.challenge).toMatch(/^[A-Za-z0-9_-]+$/);
  });

  it("verifies registration and persists the credential role", async () => {
    const handle = await startBroker();
    const challenge = await registrationChallenge(handle, "approver");

    const res = await postJson(handle, "/api/webauthn/registration/verify", {
      challengeId: challenge.challengeId,
      attestationResponse: registrationResponse("cred_approver"),
    });

    expect(res.status).toBe(200);
    await expect(res.json()).resolves.toEqual({
      credentialId: "cred_approver",
      role: "approver",
    });
  });

  it("starts a cosign challenge bound to a protocol-parsed claim and scope", async () => {
    const handle = await startBroker();
    await registerRole(handle, "approver", "cred_approver");
    const { claim, scope } = receiptCoSignFixture("approver");

    const res = await postJson(handle, "/api/webauthn/cosign/challenge", {
      claim: approvalClaimToJsonValue(claim),
      scope: approvalScopeToJsonValue(scope),
    });

    expect(res.status).toBe(200);
    const body = (await res.json()) as CosignChallengeResponse;
    expect(body.challengeId).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(body.options.rpId).toBe("localhost");
    expect(body.options.allowCredentials?.map((credential) => credential.id)).toEqual([
      "cred_approver",
    ]);
  });

  it("verifies a cosign assertion and returns a protocol SignedApprovalToken", async () => {
    const handle = await startBroker();
    await registerRole(handle, "approver", "cred_approver");
    const challenge = await cosignChallenge(handle, "approver");

    const res = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: assertionResponse("cred_approver"),
    });

    expect(res.status).toBe(200);
    const tokenBody: unknown = await res.json();
    const approvalToken = signedApprovalTokenFromJson(tokenBody);
    expect(approvalToken.issuedTo).toBe(agentId);
    expect(approvalToken.scope.role).toBe("approver");
    expect(approvalToken.signature.credentialId).toBe("cred_approver");
  });

  it("rejects expired cosign challenges without sleeping", async () => {
    const clock = new FakeClock(10_000);
    const handle = await startBroker({ clock });
    await registerRole(handle, "approver", "cred_approver");
    const challenge = await cosignChallenge(handle, "approver");
    clock.set(10_000 + 5 * 60 * 1000 + 1);

    const res = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: assertionResponse("cred_approver"),
    });

    expect(res.status).toBe(400);
    await expect(res.json()).resolves.toEqual({ error: "challenge_expired" });
  });

  it("rejects reused registration challenges", async () => {
    const handle = await startBroker();
    const challenge = await registrationChallenge(handle, "approver");
    const first = await postJson(handle, "/api/webauthn/registration/verify", {
      challengeId: challenge.challengeId,
      attestationResponse: registrationResponse("cred_approver"),
    });
    expect(first.status).toBe(200);

    const second = await postJson(handle, "/api/webauthn/registration/verify", {
      challengeId: challenge.challengeId,
      attestationResponse: registrationResponse("cred_other"),
    });

    expect(second.status).toBe(409);
    await expect(second.json()).resolves.toEqual({ error: "challenge_consumed" });
  });

  it("rejects cosign challenges whose scope is not bound to the claim target", async () => {
    const handle = await startBroker();
    await registerRole(handle, "approver", "cred_approver");
    const { claim, scope } = receiptCoSignFixture("approver");

    const res = await postJson(handle, "/api/webauthn/cosign/challenge", {
      claim: approvalClaimToJsonValue(claim),
      scope: approvalScopeToJsonValue({
        mode: scope.mode,
        claimId: scope.claimId,
        claimKind: scope.claimKind,
        role: scope.role,
        maxUses: scope.maxUses,
        receiptId: asReceiptId("01BRZ3NDEKTSV4RRFFQ69G5FB1"),
        frozenArgsHash: scope.frozenArgsHash,
      }),
    });

    expect(res.status).toBe(400);
    const body = (await res.json()) as { readonly error: string };
    expect(body.error).toMatch(/receiptId/);
  });

  it("rejects a cosign assertion presented by a different bearer-bound agent", async () => {
    const tokenAgentIds = new Map([[token, agentId]]);
    const handle = await startBroker({ tokenAgentIds });
    await registerRole(handle, "approver", "cred_approver");
    const challenge = await cosignChallenge(handle, "approver");
    tokenAgentIds.set(token, otherAgentId);

    const res = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: assertionResponse("cred_approver"),
    });

    expect(res.status).toBe(403);
    await expect(res.json()).resolves.toEqual({ error: "wrong_issued_to_agent" });
  });

  it("rejects credentials registered to the wrong role for a challenge", async () => {
    const handle = await startBroker();
    await registerRole(handle, "approver", "cred_approver");
    await registerRole(handle, "host", "cred_host");
    const challenge = await cosignChallenge(handle, "approver");

    const res = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: assertionResponse("cred_host"),
    });

    expect(res.status).toBe(403);
    await expect(res.json()).resolves.toEqual({ error: "wrong_credential_role" });
  });

  it("returns approval_pending until the role threshold is met", async () => {
    const handle = await startBroker({ receiptCoSignThreshold: 2 });
    await registerRole(handle, "approver", "cred_approver");
    const approverChallenge = await cosignChallenge(handle, "approver");

    const pending = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: approverChallenge.challengeId,
      assertionResponse: assertionResponse("cred_approver"),
    });

    expect(pending.status).toBe(200);
    await expect(pending.json()).resolves.toEqual({
      status: "approval_pending",
      satisfiedRoles: ["approver"],
      requiredThreshold: 2,
    });

    await registerRole(handle, "host", "cred_host");
    const hostChallenge = await cosignChallenge(handle, "host");
    const approved = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: hostChallenge.challengeId,
      assertionResponse: assertionResponse("cred_host"),
    });

    expect(approved.status).toBe(200);
    const approvalToken = signedApprovalTokenFromJson(await approved.json());
    expect(approvalToken.scope.role).toBe("host");
  });

  it("returns the same consumed token outcome on idempotent replay", async () => {
    const ceremony = new FakeCeremony();
    const handle = await startBroker({ ceremony });
    await registerRole(handle, "approver", "cred_approver");
    const challenge = await cosignChallenge(handle, "approver");

    const first = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: assertionResponse("cred_approver"),
    });
    const firstJson: unknown = await first.json();

    const second = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: "malformed replay body is ignored after token consumption",
    });

    expect(second.status).toBe(200);
    await expect(second.json()).resolves.toEqual(firstJson);
    expect(ceremony.authenticationCalls).toBe(1);
  });

  it("rejects sign-count replay before consuming a token", async () => {
    const ceremony = new FakeCeremony();
    ceremony.nextCounters.set("cred_approver", 1);
    const handle = await startBroker({ ceremony });
    await registerRole(handle, "approver", "cred_approver");
    const challenge = await cosignChallenge(handle, "approver");

    const res = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: assertionResponse("cred_approver"),
    });

    expect(res.status).toBe(400);
    await expect(res.json()).resolves.toEqual({ error: "sign_count_replay" });
  });
});

async function startBroker(
  options: {
    readonly clock?: Clock;
    readonly tokenAgentIds?: Map<ApiToken, AgentId>;
    readonly receiptCoSignThreshold?: number;
    readonly ceremony?: FakeCeremony;
  } = {},
): Promise<BrokerHandle> {
  db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const webauthn = {
    store: createWebAuthnStore(db),
    tokenAgentIds: options.tokenAgentIds ?? new Map([[token, agentId]]),
    ceremony: options.ceremony ?? new FakeCeremony(),
    rpId: "localhost",
    allowedOrigins: ["http://localhost:5173"],
    ...(options.receiptCoSignThreshold === undefined
      ? {}
      : { receiptCoSignThreshold: options.receiptCoSignThreshold }),
  };
  const handle = await createBroker({
    token,
    ...(options.clock === undefined ? {} : { clock: options.clock }),
    webauthn,
  });
  broker = handle;
  return handle;
}

async function registrationChallenge(
  handle: BrokerHandle,
  role: ApprovalRole,
): Promise<RegistrationChallengeResponse> {
  const res = await postJson(handle, "/api/webauthn/registration/challenge", { role });
  expect(res.status).toBe(200);
  return (await res.json()) as RegistrationChallengeResponse;
}

async function registerRole(
  handle: BrokerHandle,
  role: ApprovalRole,
  credentialId: string,
): Promise<void> {
  const challenge = await registrationChallenge(handle, role);
  const res = await postJson(handle, "/api/webauthn/registration/verify", {
    challengeId: challenge.challengeId,
    attestationResponse: registrationResponse(credentialId),
  });
  expect(res.status).toBe(200);
}

async function cosignChallenge(
  handle: BrokerHandle,
  role: ApprovalRole,
): Promise<CosignChallengeResponse> {
  const { claim, scope } = receiptCoSignFixture(role);
  const res = await postJson(handle, "/api/webauthn/cosign/challenge", {
    claim: approvalClaimToJsonValue(claim),
    scope: approvalScopeToJsonValue(scope),
  });
  expect(res.status).toBe(200);
  return (await res.json()) as CosignChallengeResponse;
}

function receiptCoSignFixture(role: ApprovalRole): {
  readonly claim: ReceiptCoSignClaim;
  readonly scope: ReceiptCoSignScope;
} {
  const claim: ReceiptCoSignClaim = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim-branch-12"),
    kind: "receipt_co_sign",
    receiptId: asReceiptId("01BRZ3NDEKTSV4RRFFQ69G5FA0"),
    frozenArgsHash: asSha256Hex("a".repeat(64)),
    riskClass: "high",
  };
  const scope: ReceiptCoSignScope = {
    mode: "single_use",
    claimId: claim.claimId,
    claimKind: "receipt_co_sign",
    role,
    maxUses: 1,
    receiptId: claim.receiptId,
    frozenArgsHash: claim.frozenArgsHash,
  };
  return { claim, scope };
}

function registrationResponse(credentialId: string): RegistrationResponseJSON {
  return {
    id: credentialId,
    rawId: credentialId,
    response: {
      clientDataJSON: "clientData",
      attestationObject: "attestationObject",
    },
    clientExtensionResults: {},
    type: "public-key",
  };
}

function assertionResponse(credentialId: string): AuthenticationResponseJSON {
  return {
    id: credentialId,
    rawId: credentialId,
    response: {
      clientDataJSON: "clientData",
      authenticatorData: "authenticatorData",
      signature: "signature",
    },
    clientExtensionResults: {},
    type: "public-key",
  };
}

async function postJson(handle: BrokerHandle, path: string, body: unknown): Promise<Response> {
  return await fetch(`${handle.url}${path}`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });
}

class FakeClock implements Clock {
  constructor(private value: number) {}

  now(): number {
    return this.value;
  }

  set(value: number): void {
    this.value = value;
  }
}

class FakeCeremony implements WebAuthnCeremony {
  readonly nextCounters = new Map<string, number>();
  authenticationCalls = 0;

  async generateRegistrationOptions(args: {
    readonly rpName: string;
    readonly rpId: string;
    readonly agentId: AgentId;
    readonly role: ApprovalRole;
    readonly challenge: string;
    readonly excludeCredentialIds: readonly string[];
  }): Promise<PublicKeyCredentialCreationOptionsJSON> {
    return {
      rp: { name: args.rpName, id: args.rpId },
      user: {
        id: `${args.agentId}:${args.role}`,
        name: `${args.agentId}:${args.role}`,
        displayName: args.role,
      },
      challenge: args.challenge,
      pubKeyCredParams: [{ alg: -7, type: "public-key" }],
      excludeCredentials: args.excludeCredentialIds.map((id) => ({ id, type: "public-key" })),
    };
  }

  async verifyRegistration(args: {
    readonly response: RegistrationResponseJSON;
    readonly expectedChallenge: string;
    readonly expectedOrigins: readonly string[];
    readonly expectedRpId: string;
  }): Promise<RegisteredWebAuthnCredentialVerification | null> {
    expect(args.expectedChallenge).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(args.expectedOrigins).toEqual(["http://localhost:5173"]);
    expect(args.expectedRpId).toBe("localhost");
    return {
      credentialId: args.response.id,
      publicKey: new Uint8Array([1, 2, 3]),
      signCount: 1,
    };
  }

  async generateAuthenticationOptions(args: {
    readonly rpId: string;
    readonly challenge: string;
    readonly allowCredentialIds: readonly string[];
  }): Promise<PublicKeyCredentialRequestOptionsJSON> {
    return {
      rpId: args.rpId,
      challenge: args.challenge,
      allowCredentials: args.allowCredentialIds.map((id) => ({ id, type: "public-key" })),
      userVerification: "required",
    };
  }

  async verifyAuthentication(args: {
    readonly response: AuthenticationResponseJSON;
    readonly expectedChallenge: string;
    readonly expectedOrigins: readonly string[];
    readonly expectedRpId: string;
    readonly credential: WebAuthnCredential;
  }): Promise<WebAuthnAuthenticationVerification | null> {
    this.authenticationCalls += 1;
    expect(args.expectedChallenge).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(args.expectedOrigins).toEqual(["http://localhost:5173"]);
    expect(args.expectedRpId).toBe("localhost");
    expect(args.credential.id).toBe(args.response.id);
    return {
      credentialId: args.response.id,
      newSignCount: this.nextCounters.get(args.response.id) ?? 2,
      userVerified: true,
    };
  }
}

interface RegistrationChallengeResponse {
  readonly challengeId: string;
  readonly options: PublicKeyCredentialCreationOptionsJSON;
}

interface CosignChallengeResponse {
  readonly challengeId: string;
  readonly options: PublicKeyCredentialRequestOptionsJSON;
}

interface RawResponse {
  readonly status: number;
  readonly body: string;
  readonly headers: IncomingHttpHeaders;
}

function rawRequest(args: {
  readonly port: number;
  readonly path: string;
  readonly method: string;
  readonly headers: OutgoingHttpHeaders;
  readonly body: string;
}): Promise<RawResponse> {
  return new Promise((resolveFn, rejectFn) => {
    const req = request(
      {
        host: "127.0.0.1",
        port: args.port,
        path: args.path,
        method: args.method,
        headers: args.headers,
      },
      (response) => {
        const chunks: Buffer[] = [];
        response.on("data", (chunk: Buffer) => chunks.push(chunk));
        response.on("end", () =>
          resolveFn({
            status: response.statusCode ?? 0,
            body: Buffer.concat(chunks).toString("utf8"),
            headers: response.headers,
          }),
        );
      },
    );
    req.on("error", rejectFn);
    req.write(args.body);
    req.end();
  });
}
