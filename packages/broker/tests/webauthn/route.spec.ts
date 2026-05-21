import { type IncomingHttpHeaders, type OutgoingHttpHeaders, request } from "node:http";
import { isIP } from "node:net";
import type { DatabaseSync } from "node:sqlite";

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
  type ApprovalClaim,
  type ApprovalRole,
  type ApprovalScope,
  approvalClaimToJsonValue,
  approvalScopeToJsonValue,
  asAgentId,
  asApiToken,
  asApprovalClaimId,
  asApprovalTokenId,
  asCredentialHandleId,
  asCredentialScope,
  asProviderKind,
  asReceiptId,
  asSha256Hex,
  asTimestampMs,
  type CostSpikeAcknowledgementClaim,
  type CostSpikeAcknowledgementScope,
  type CredentialGrantToAgentClaim,
  type CredentialGrantToAgentScope,
  canonicalJSON,
  type EndpointAllowlistExtensionClaim,
  type EndpointAllowlistExtensionScope,
  MAX_APPROVAL_TOKEN_LIFETIME_MS,
  MAX_WEBAUTHN_ASSERTION_BYTES,
  MAX_WEBAUTHN_ASSERTION_FIELD_BYTES,
  type ReceiptCoSignClaim,
  type ReceiptCoSignScope,
  sha256Hex,
  signedApprovalTokenFromJson,
} from "@wuphf/protocol";
import { afterEach, describe, expect, it } from "vitest";

import { openDatabase, runMigrations } from "../../src/event-log/index.ts";
import { type BrokerHandle, type BrokerLogger, createBroker } from "../../src/index.ts";
import { createOpportunisticPruningWebAuthnStore } from "../../src/webauthn/prune.ts";
import { createWebAuthnStore } from "../../src/webauthn/store.ts";
import {
  type Clock,
  type RegisteredWebAuthnCredential,
  type RegisteredWebAuthnCredentialVerification,
  type WebAuthnAuthenticationVerification,
  type WebAuthnCeremony,
  WebAuthnSignCountReplayError,
  type WebAuthnStore,
  WebAuthnStoreBusyError,
  WebAuthnStoreFullError,
  WebAuthnStoreUnavailableError,
} from "../../src/webauthn/types.ts";

const token = asApiToken("test-token-with-enough-entropy-AAAAAAAAA");
const agentId = asAgentId("agent_alpha");
const otherAgentId = asAgentId("agent_beta");

let broker: BrokerHandle | null = null;
let db: DatabaseSync | null = null;

const agentScopedClaimFixtureCases: readonly [
  string,
  (targetAgentId: AgentId, role: ApprovalRole) => AgentScopedClaimFixture,
][] = [
  ["cost spike acknowledgement", costSpikeAcknowledgementFixture],
  ["endpoint allowlist extension", endpointAllowlistExtensionFixture],
  ["credential grant", credentialGrantToAgentFixture],
];

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
    expect(body.creationOptions.rp.id).toBe("localhost");
    expect(body.creationOptions.challenge).toMatch(/^[A-Za-z0-9_-]+$/);
  });

  it("allows registration challenges for broker-configured roles", async () => {
    const handle = await startBroker({
      enrollableRoles: enrollableRolesForAgent(agentId, ["host"]),
    });

    const res = await postJson(handle, "/api/webauthn/registration/challenge", {
      role: "host",
    });

    expect(res.status).toBe(200);
    const body = (await res.json()) as RegistrationChallengeResponse;
    expect(body.creationOptions.user.displayName).toBe("host");
  });

  it("rejects registration challenges for roles outside the broker enrollment policy", async () => {
    const ceremony = new FakeCeremony();
    const handle = await startBroker({
      ceremony,
      enrollableRoles: enrollableRolesForAgent(agentId, ["viewer"]),
    });

    const res = await postJson(handle, "/api/webauthn/registration/challenge", {
      role: "host",
    });

    expect(res.status).toBe(403);
    await expect(res.json()).resolves.toEqual({ error: "registration_role_not_enrollable" });
    expect(ceremony.registrationOptionCalls).toBe(0);
  });

  it.each([
    ["busy", () => new WebAuthnStoreBusyError("test busy"), 503, "store_busy", "1"],
    ["full", () => new WebAuthnStoreFullError("test full"), 507, "store_full", null],
    [
      "unavailable",
      () => new WebAuthnStoreUnavailableError("test readonly"),
      503,
      "storage_error",
      null,
    ],
  ] as const)("maps WebAuthn store %s write failures to structured HTTP errors", async (_name, errorFactory, status, error, retryAfter) => {
    const handle = await startBroker({
      wrapStore: (inner) => new SaveRegistrationChallengeFailingStore(inner, errorFactory()),
    });

    const res = await postJson(handle, "/api/webauthn/registration/challenge", {
      role: "approver",
    });

    expect(res.status).toBe(status);
    expect(res.headers.get("retry-after")).toBe(retryAfter);
    await expect(res.json()).resolves.toEqual({ error });
  });

  it("maps WebAuthn store read failures to structured HTTP errors and route logs", async () => {
    const logger = new RecordingLogger();
    const handle = await startBroker({
      logger,
      wrapStore: (inner) => new ListCredentialsForAgentFailingStore(inner),
    });

    const res = await postJson(handle, "/api/webauthn/registration/challenge", {
      role: "approver",
    });

    expect(res.status).toBe(503);
    expect(res.headers.get("retry-after")).toBe("1");
    await expect(res.json()).resolves.toEqual({ error: "store_busy" });
    expect(logger.warns).toContainEqual({
      event: "webauthn_registration_challenge_rejected",
      payload: { reason: "store_busy" },
    });
  });

  it("does not implicitly enroll trusted viewer credentials when enrollableRoles is omitted", async () => {
    const handle = await startBroker({ omitEnrollableRoles: true, trustedRoles: ["viewer"] });

    const res = await postJson(handle, "/api/webauthn/registration/challenge", {
      role: "viewer",
    });

    expect(res.status).toBe(403);
    await expect(res.json()).resolves.toEqual({ error: "registration_role_not_enrollable" });
  });

  it("rejects unknown trusted approval roles at startup", async () => {
    await expect(startBroker({ trustedRoles: ["typo" as ApprovalRole] })).rejects.toThrow(
      /trustedRoles\[0\].*valid approval role/,
    );
  });

  it("rejects unknown enrollable approval roles at startup", async () => {
    await expect(
      startBroker({ enrollableRoles: enrollableRolesForAgent(agentId, ["typo" as ApprovalRole]) }),
    ).rejects.toThrow(/enrollableRoles\[agent_alpha\]\[0\].*valid approval role/);
  });

  it("rejects explicitly trusted roles in configured enrollable roles at startup", async () => {
    await expect(
      startBroker({
        trustedRoles: ["viewer"],
        enrollableRoles: enrollableRolesForAgent(agentId, ["viewer"]),
      }),
    ).rejects.toThrow(/enrollableRoles\[agent_alpha\].*explicitly trusted role: viewer/);
  });

  it("rejects impossible WebAuthn thresholds at startup", async () => {
    await expect(
      startBroker({
        omitEnrollableRoles: true,
        trustedRoles: ["approver", "approver"],
        receiptCoSignThreshold: 2,
      }),
    ).rejects.toThrow(/receiptCoSignThreshold 2 exceeds distinct trusted approval roles 1/);
  });

  it("rejects WebAuthn allowed origins that cannot satisfy the RP ID", async () => {
    await expect(
      startBroker({
        rpId: "localhost",
        allowedOrigins: ["http://127.0.0.1:5173"],
      }),
    ).rejects.toThrow(/RP ID localhost/);
  });

  it("rejects non-origin WebAuthn allowed origin entries at startup", async () => {
    await expect(startBroker({ allowedOrigins: ["http://localhost:5173/app"] })).rejects.toThrow(
      /bare origin/,
    );
    await expect(startBroker({ allowedOrigins: ["http://user@localhost:5173"] })).rejects.toThrow(
      /userinfo/,
    );
  });

  it("rejects invalid WebAuthn challenge TTLs at startup", async () => {
    await expect(startBroker({ challengeTtlMs: 0 })).rejects.toThrow(/challengeTtlMs/);
    await expect(
      startBroker({ challengeTtlMs: MAX_APPROVAL_TOKEN_LIFETIME_MS + 1 }),
    ).rejects.toThrow(/challengeTtlMs/);
    await expect(startBroker({ challengeTtlMs: 1.5 })).rejects.toThrow(/challengeTtlMs/);
  });

  it("prunes expired WebAuthn state at startup using the injected clock", async () => {
    const clock = new FakeClock(123_456);
    const pruneCalls: PruneCall[] = [];

    await startBroker({
      clock,
      wrapStore: (inner) => new RecordingPruneStore(inner, pruneCalls),
    });

    expect(pruneCalls).toEqual([{ nowMs: 123_456, maxRows: 64 }]);
  });

  it("prunes expired WebAuthn state opportunistically after bounded write batches", async () => {
    const clock = new FakeClock(500);
    const logger = new RecordingLogger();
    const pruneCalls: PruneCall[] = [];
    const inner = new RecordingPruneStore(createTestWebAuthnStore(), pruneCalls);
    const store = createOpportunisticPruningWebAuthnStore(inner, {
      clock,
      logger,
      writeInterval: 2,
      maxRows: 3,
    });

    await store.saveRegistrationChallenge({
      challengeId: "writePruneOne",
      challenge: "writePruneOne",
      role: "approver",
      issuedToAgentId: agentId,
      createdAtMs: 1,
      expiresAtMs: 2,
    });
    clock.set(600);
    await store.saveRegistrationChallenge({
      challengeId: "writePruneTwo",
      challenge: "writePruneTwo",
      role: "approver",
      issuedToAgentId: agentId,
      createdAtMs: 1,
      expiresAtMs: 2,
    });

    expect(pruneCalls).toEqual([{ nowMs: 600, maxRows: 3 }]);
    expect(logger.infos).toContainEqual({
      event: "webauthn_expired_state_pruned",
      payload: {
        trigger: "write_interval",
        nowMs: 600,
        maxRows: 3,
        consumedTokens: 0,
        orphanChallenges: 2,
      },
    });
  });

  it("verifies registration and persists the credential role", async () => {
    const ceremony = new FakeCeremony();
    const handle = await startBroker({ ceremony });
    const challenge = await registrationChallenge(handle, "approver");

    const res = await postJson(handle, "/api/webauthn/registration/verify", {
      challengeId: challenge.challengeId,
      attestationResponse: registrationResponse("cred_approver"),
    });

    expect(res.status).toBe(200);
    expect(ceremony.registrationVerificationOrigins).toContain(packagedWebAuthnOrigin(handle));
    expect(ceremony.registrationVerificationCalls).toBe(1);
    await expect(res.json()).resolves.toEqual({
      credentialId: webAuthnCredentialId("cred_approver"),
      role: "approver",
    });
  });

  it("rejects oversized registration attestations before WebAuthn verification", async () => {
    const ceremony = new FakeCeremony();
    const handle = await startBroker({ ceremony });
    const challenge = await registrationChallenge(handle, "approver");
    const attestation = registrationResponse("cred_approver");

    const res = await postJson(handle, "/api/webauthn/registration/verify", {
      challengeId: challenge.challengeId,
      attestationResponse: {
        ...attestation,
        response: {
          ...attestation.response,
          attestationObject: "A".repeat(64 * 1024 + 1),
        },
      },
    });

    expect(res.status).toBe(400);
    const body = (await res.json()) as { readonly error: string };
    expect(body.error).toMatch(/attestationObject.*bytes/);
    expect(ceremony.registrationVerificationCalls).toBe(0);
  });

  it("logs sanitized registration verification failure details", async () => {
    const logger = new RecordingLogger();
    const ceremony = new FakeCeremony();
    ceremony.registrationVerificationFailure = new Error(
      'Unexpected registration response challenge "attacker", expected "stored"',
    );
    const handle = await startBroker({ ceremony, logger });
    const challenge = await registrationChallenge(handle, "approver");

    const res = await postJson(handle, "/api/webauthn/registration/verify", {
      challengeId: challenge.challengeId,
      attestationResponse: registrationResponse("cred_approver"),
    });

    expect(res.status).toBe(400);
    expect(logger.warns).toContainEqual({
      event: "webauthn_registration_verification_failed",
      payload: {
        reason: "challenge_mismatch",
        ceremony: "registration",
        route: "/api/webauthn/registration/verify",
        challengeType: "registration",
      },
    });
    expect(JSON.stringify(logger.warns)).not.toContain("attacker");
  });

  it("rejects registration verify when a stale challenge role is no longer enrollable", async () => {
    const enrollableRoles = new Map<AgentId, readonly ApprovalRole[]>([[agentId, ["approver"]]]);
    const handle = await startBroker({ enrollableRoles });
    const challenge = await registrationChallenge(handle, "approver");
    enrollableRoles.set(agentId, []);

    const res = await postJson(handle, "/api/webauthn/registration/verify", {
      challengeId: challenge.challengeId,
      attestationResponse: registrationResponse("cred_approver"),
    });

    expect(res.status).toBe(403);
    await expect(res.json()).resolves.toEqual({ error: "registration_role_not_enrollable" });
  });

  it("uses a browser-valid packaged WebAuthn origin and RP ID pairing", async () => {
    const ceremony = new FakeCeremony();
    const handle = await startBroker({ ceremony });
    const challenge = await registrationChallenge(handle, "approver");

    const res = await postJson(handle, "/api/webauthn/registration/verify", {
      challengeId: challenge.challengeId,
      attestationResponse: registrationResponse("cred_packaged_pairing"),
    });

    expect(res.status).toBe(200);
    const packagedOrigin = packagedWebAuthnOrigin(handle);
    const windowLoadUrl = `${packagedOrigin}/`;
    const originHost = new URL(packagedOrigin).hostname;
    const windowHost = new URL(windowLoadUrl).hostname;
    const rpId = challenge.creationOptions.rp.id;
    if (rpId === undefined) {
      throw new Error("Expected registration options to include an RP ID");
    }

    expect(ceremony.registrationVerificationOrigins).toContain(packagedOrigin);
    expect(windowHost).toBe(originHost);
    expect(rpId).toBe(originHost);
    expect(isIP(rpId)).toBe(0);
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
    expect(body.requestOptions.rpId).toBe("localhost");
    expect(body.requestOptions.allowCredentials?.map((credential) => credential.id)).toEqual([
      webAuthnCredentialId("cred_approver"),
    ]);
    const stored = await createWebAuthnStore(requiredDb()).getChallenge(body.challengeId);
    const hashes = hashClaimScopeForTest(claim, scope);
    expect(stored).toMatchObject({
      claimScopeHash: hashes.claimScopeHash,
      approvalGroupHash: hashes.approvalGroupHash,
    });
  });

  it("echoes the canonical parsed claim and scope in cosign challenge responses", async () => {
    const handle = await startBroker();
    await registerRole(handle, "approver", "cred_approver");
    const { claim, scope } = costSpikeAcknowledgementFixture(agentId, "approver");
    const rawClaim = {
      ...approvalClaimToJsonValue(claim),
      costCeilingId: "ceiling\u180b-main",
    };
    const rawScope = {
      ...approvalScopeToJsonValue(scope),
      costCeilingId: "ceiling\u180b-main",
    };

    const res = await postJson(handle, "/api/webauthn/cosign/challenge", {
      claim: rawClaim,
      scope: rawScope,
    });

    expect(res.status).toBe(200);
    const body = (await res.json()) as CosignChallengeResponse;
    expect(body.claim).toMatchObject({ costCeilingId: "ceiling-main" });
    expect(body.scope).toMatchObject({ costCeilingId: "ceiling-main" });
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
    expect(approvalToken.signature.credentialId).toBe(webAuthnCredentialId("cred_approver"));
  });

  it("rejects oversized cosign assertion fields before WebAuthn verification", async () => {
    const ceremony = new FakeCeremony();
    const handle = await startBroker({ ceremony });
    await registerRole(handle, "approver", "cred_approver");
    const challenge = await cosignChallenge(handle, "approver");
    const assertion = assertionResponse("cred_approver");

    const res = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: {
        ...assertion,
        response: {
          ...assertion.response,
          signature: "A".repeat(MAX_WEBAUTHN_ASSERTION_FIELD_BYTES + 1),
        },
      },
    });

    expect(res.status).toBe(400);
    const body = (await res.json()) as { readonly error: string };
    expect(body.error).toMatch(/signature.*bytes/);
    expect(ceremony.authenticationCalls).toBe(0);
  });

  it("rejects non-canonical cosign assertion base64url before WebAuthn verification", async () => {
    const ceremony = new FakeCeremony();
    const handle = await startBroker({ ceremony });
    await registerRole(handle, "approver", "cred_approver");
    const challenge = await cosignChallenge(handle, "approver");
    const assertion = assertionResponse("cred_approver");

    const res = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: {
        ...assertion,
        response: {
          ...assertion.response,
          signature: "A",
        },
      },
    });

    expect(res.status).toBe(400);
    const body = (await res.json()) as { readonly error: string };
    expect(body.error).toMatch(/signature.*canonical.*base64url/);
    expect(ceremony.authenticationCalls).toBe(0);
  });

  it("rejects oversized cosign assertions before WebAuthn verification", async () => {
    const ceremony = new FakeCeremony();
    const handle = await startBroker({ ceremony });
    await registerRole(handle, "approver", "cred_approver");
    const challenge = await cosignChallenge(handle, "approver");
    const assertion = assertionResponse("cred_approver");

    const res = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: {
        ...assertion,
        response: {
          ...assertion.response,
          signature: "A".repeat(MAX_WEBAUTHN_ASSERTION_BYTES - 1),
        },
      },
    });

    expect(res.status).toBe(400);
    const body = (await res.json()) as { readonly error: string };
    expect(body.error).toMatch(/WebAuthnAssertion canonical JSON bytes/);
    expect(ceremony.authenticationCalls).toBe(0);
  });

  it("logs sanitized assertion verification failure details", async () => {
    const logger = new RecordingLogger();
    const ceremony = new FakeCeremony();
    ceremony.authenticationVerificationFailure = "null";
    const handle = await startBroker({ ceremony, logger });
    await registerRole(handle, "approver", "cred_approver");
    const challenge = await cosignChallenge(handle, "approver");

    const res = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: assertionResponse("cred_approver"),
    });

    expect(res.status).toBe(400);
    expect(logger.warns).toContainEqual({
      event: "webauthn_assertion_verification_failed",
      payload: {
        reason: "bad_signature",
        ceremony: "authentication",
        route: "/api/webauthn/cosign/verify",
        challengeType: "cosign",
      },
    });
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

  it.each(
    agentScopedClaimFixtureCases,
  )("allows %s cosign challenges for the bearer-bound agent", async (_name, fixture) => {
    const handle = await startBroker();
    await registerRole(handle, "approver", "cred_approver");
    const { claim, scope } = fixture(agentId, "approver");

    const res = await postJson(handle, "/api/webauthn/cosign/challenge", {
      claim: approvalClaimToJsonValue(claim),
      scope: approvalScopeToJsonValue(scope),
    });

    expect(res.status).toBe(200);
    const body = (await res.json()) as CosignChallengeResponse;
    expect(body.requestOptions.allowCredentials?.map((credential) => credential.id)).toEqual([
      webAuthnCredentialId("cred_approver"),
    ]);
  });

  it.each(
    agentScopedClaimFixtureCases,
  )("rejects %s cosign challenges targeting another agent", async (_name, fixture) => {
    const handle = await startBroker();
    await registerRole(handle, "approver", "cred_approver");
    const { claim, scope } = fixture(otherAgentId, "approver");

    const res = await postJson(handle, "/api/webauthn/cosign/challenge", {
      claim: approvalClaimToJsonValue(claim),
      scope: approvalScopeToJsonValue(scope),
    });

    expect(res.status).toBe(403);
    await expect(res.json()).resolves.toEqual({ error: "wrong_claim_agent" });
  });

  it("rejects cosign verify for a stale cross-agent challenge", async () => {
    const handle = await startBroker();
    await registerRole(handle, "approver", "cred_approver");
    const { claim, scope } = costSpikeAcknowledgementFixture(otherAgentId, "approver");
    const hashes = hashClaimScopeForTest(claim, scope);
    const store = createWebAuthnStore(requiredDb());
    await store.saveCosignChallenge({
      challengeId: "staleCrossAgentChallenge",
      challenge: "staleCrossAgentChallenge",
      tokenId: asApprovalTokenId("01BRZ3NDEKTSV4RRFFQ69G5FA1"),
      claim,
      scope,
      claimJson: hashes.claimJson,
      scopeJson: hashes.scopeJson,
      claimScopeHash: hashes.claimScopeHash,
      approvalGroupHash: hashes.approvalGroupHash,
      issuedToAgentId: agentId,
      notBeforeMs: asTimestampMs(10),
      expiresAtMs: asTimestampMs(1_000_000),
      createdAtMs: 10,
    });

    const res = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: "staleCrossAgentChallenge",
      assertionResponse: assertionResponse("cred_approver"),
    });

    expect(res.status).toBe(403);
    await expect(res.json()).resolves.toEqual({ error: "wrong_claim_agent" });
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

  it("computes threshold outcomes from the post-consume role set", async () => {
    const delayedStoreRef: { current: DelayedConsumeStore | null } = { current: null };
    const handle = await startBroker({
      receiptCoSignThreshold: 2,
      wrapStore: (inner) => {
        const delayedStore = new DelayedConsumeStore(inner);
        delayedStoreRef.current = delayedStore;
        return delayedStore;
      },
    });
    await registerRole(handle, "approver", "cred_approver");
    await registerRole(handle, "host", "cred_host");
    const approverChallenge = await cosignChallenge(handle, "approver");
    const hostChallenge = await cosignChallenge(handle, "host");

    const approverVerify = postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: approverChallenge.challengeId,
      assertionResponse: assertionResponse("cred_approver"),
    });
    const hostVerify = postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: hostChallenge.challengeId,
      assertionResponse: assertionResponse("cred_host"),
    });
    const delayedStore = delayedStoreRef.current;
    if (delayedStore === null) {
      throw new Error("delayed store was not installed");
    }
    await waitForPendingConsumes(delayedStore, 2);
    delayedStore.releaseConsumes();

    const responses = await Promise.all([approverVerify, hostVerify]);
    expect(responses.map((res) => res.status)).toEqual([200, 200]);
    const bodies: readonly unknown[] = await Promise.all(responses.map((res) => res.json()));
    const pending = bodies.filter(isApprovalPendingBody);
    const approved = bodies.filter((body) => !isApprovalPendingBody(body));
    expect(pending).toHaveLength(1);
    const pendingBody = pending[0];
    if (pendingBody === undefined) {
      throw new Error("missing pending response body");
    }
    expect(pendingBody.requiredThreshold).toBe(2);
    expect(pendingBody.satisfiedRoles).toHaveLength(1);
    expect(approved).toHaveLength(1);
    const approvedBody = approved[0];
    if (approvedBody === undefined) {
      throw new Error("missing approved response body");
    }
    const approvalToken = signedApprovalTokenFromJson(approvedBody);
    expect(new Set([...pendingBody.satisfiedRoles, approvalToken.scope.role])).toEqual(
      new Set(["approver", "host"]),
    );
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

  it("rejects replay of a consumed token after the challenge expires", async () => {
    const clock = new FakeClock(10_000);
    const ceremony = new FakeCeremony();
    const handle = await startBroker({ ceremony, clock });
    await registerRole(handle, "approver", "cred_approver");
    const challenge = await cosignChallenge(handle, "approver");
    const first = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: assertionResponse("cred_approver"),
    });
    expect(first.status).toBe(200);
    clock.set(10_000 + 5 * 60 * 1000 + 1);

    const replay = await postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: challenge.challengeId,
      assertionResponse: "malformed replay body stays closed after expiry",
    });

    expect(replay.status).toBe(400);
    await expect(replay.json()).resolves.toEqual({ error: "challenge_expired" });
    expect(ceremony.authenticationCalls).toBe(1);
  });

  it("rejects sign-count replay before consuming a token", async () => {
    const ceremony = new FakeCeremony();
    ceremony.nextCounters.set(webAuthnCredentialId("cred_approver"), 1);
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

  it("rejects a concurrent lower sign-count verify inside the consume transaction", async () => {
    const ceremony = new ControlledAuthenticationCeremony();
    const handle = await startBroker({ ceremony });
    await registerRole(handle, "approver", "cred_approver");
    const firstChallenge = await cosignChallenge(handle, "approver");
    const secondChallenge = await cosignChallenge(handle, "approver");

    const firstVerify = postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: firstChallenge.challengeId,
      assertionResponse: assertionResponse("cred_approver"),
    });
    const secondVerify = postJson(handle, "/api/webauthn/cosign/verify", {
      challengeId: secondChallenge.challengeId,
      assertionResponse: assertionResponse("cred_approver"),
    });
    await waitForPendingAuthentications(ceremony, 2);

    ceremony.resolveAuthentication(0, 3);
    const first = await firstVerify;
    expect(first.status).toBe(200);

    ceremony.resolveAuthentication(1, 2);
    const second = await secondVerify;
    expect(second.status).toBe(400);
    await expect(second.json()).resolves.toEqual({ error: "sign_count_replay" });
  });

  it("rejects non-monotonic sign-count updates at the store contract boundary", async () => {
    const store = createTestWebAuthnStore();
    await store.saveRegistrationChallenge({
      challengeId: "registrationCounterChallenge",
      challenge: "registrationCounterChallenge",
      role: "approver",
      issuedToAgentId: agentId,
      createdAtMs: 1,
      expiresAtMs: 10_000,
    });
    await store.saveCredential({
      challengeId: "registrationCounterChallenge",
      credential: {
        credentialId: "cred_counter",
        publicKey: new Uint8Array([1, 2, 3]),
        signCount: 5,
        role: "approver",
        agentId,
        createdAtMs: 2,
      },
      consumedAtMs: 2,
    });
    const { claim, scope } = receiptCoSignFixture("approver");
    const hashes = hashClaimScopeForTest(claim, scope);
    await store.saveCosignChallenge({
      challengeId: "cosignCounterChallenge",
      challenge: "cosignCounterChallenge",
      tokenId: asApprovalTokenId("01BRZ3NDEKTSV4RRFFQ69G5FA2"),
      claim,
      scope,
      claimJson: hashes.claimJson,
      scopeJson: hashes.scopeJson,
      claimScopeHash: hashes.claimScopeHash,
      approvalGroupHash: hashes.approvalGroupHash,
      issuedToAgentId: agentId,
      notBeforeMs: asTimestampMs(3),
      expiresAtMs: asTimestampMs(10_000),
      createdAtMs: 3,
    });

    await expect(
      store.consumeCosignChallenge({
        challengeId: "cosignCounterChallenge",
        tokenId: asApprovalTokenId("01BRZ3NDEKTSV4RRFFQ69G5FA2"),
        credentialId: "cred_counter",
        newSignCount: 5,
        requiredThreshold: 1,
        approvedResponseJson: { status: "approved" },
        role: "approver",
        approvalGroupHash: hashes.approvalGroupHash,
        issuedToAgentId: agentId,
        expiresAtMs: 10_000,
        consumedAtMs: 4,
      }),
    ).rejects.toBeInstanceOf(WebAuthnSignCountReplayError);
  });
});

async function startBroker(
  options: {
    readonly clock?: Clock;
    readonly tokenAgentIds?: Map<ApiToken, AgentId>;
    readonly enrollableRoles?: ReadonlyMap<AgentId, readonly ApprovalRole[]>;
    readonly omitEnrollableRoles?: boolean;
    readonly trustedRoles?: readonly ApprovalRole[];
    readonly defaultThreshold?: number;
    readonly receiptCoSignThreshold?: number;
    readonly ceremony?: FakeCeremony;
    readonly logger?: BrokerLogger;
    readonly wrapStore?: (store: WebAuthnStore) => WebAuthnStore;
    readonly rpId?: string;
    readonly allowedOrigins?: readonly string[];
    readonly challengeTtlMs?: number;
  } = {},
): Promise<BrokerHandle> {
  db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const defaultEnrollableRoles = enrollableRolesForAgent(agentId, ["approver", "host"]);
  const configuredEnrollableRoles = options.omitEnrollableRoles
    ? undefined
    : (options.enrollableRoles ?? defaultEnrollableRoles);
  const store = options.wrapStore?.(createWebAuthnStore(db)) ?? createWebAuthnStore(db);
  const ceremony = options.ceremony ?? new FakeCeremony();
  const webauthn = {
    store,
    tokenAgentIds: options.tokenAgentIds ?? new Map([[token, agentId]]),
    ceremony,
    rpId: options.rpId ?? "localhost",
    allowedOrigins: options.allowedOrigins ?? ["http://localhost:5173"],
    ...(options.challengeTtlMs === undefined ? {} : { challengeTtlMs: options.challengeTtlMs }),
    ...(configuredEnrollableRoles === undefined
      ? {}
      : { enrollableRoles: configuredEnrollableRoles }),
    ...(options.trustedRoles === undefined ? {} : { trustedRoles: options.trustedRoles }),
    ...(options.defaultThreshold === undefined
      ? {}
      : { defaultThreshold: options.defaultThreshold }),
    ...(options.receiptCoSignThreshold === undefined
      ? {}
      : { receiptCoSignThreshold: options.receiptCoSignThreshold }),
  };
  const handle = await createBroker({
    token,
    ...(options.logger === undefined ? {} : { logger: options.logger }),
    ...(options.clock === undefined ? {} : { clock: options.clock }),
    webauthn,
  });
  ceremony.expectedOrigins = ["http://localhost:5173", packagedWebAuthnOrigin(handle)];
  broker = handle;
  return handle;
}

function packagedWebAuthnOrigin(handle: BrokerHandle): string {
  return `http://localhost:${handle.port}`;
}

function enrollableRolesForAgent(
  targetAgentId: AgentId,
  roles: readonly ApprovalRole[],
): ReadonlyMap<AgentId, readonly ApprovalRole[]> {
  return new Map([[targetAgentId, roles]]);
}

function createTestWebAuthnStore(): WebAuthnStore {
  if (db !== null) {
    throw new Error("test database is already open");
  }
  db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  return createWebAuthnStore(db);
}

function requiredDb(): DatabaseSync {
  if (db === null) {
    throw new Error("test database is not open");
  }
  return db;
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

function hashClaimScopeForTest(
  claim: ApprovalClaim,
  scope: ApprovalScope,
): {
  readonly claimScopeHash: ReturnType<typeof sha256Hex>;
  readonly approvalGroupHash: ReturnType<typeof sha256Hex>;
  readonly claimJson: string;
  readonly scopeJson: string;
} {
  const claimJson = canonicalJSON(approvalClaimToJsonValue(claim));
  const scopeJson = canonicalJSON(approvalScopeToJsonValue(scope));
  return {
    claimScopeHash: sha256Hex(`{"claim":${claimJson},"scope":${scopeJson}}`),
    approvalGroupHash: sha256Hex(`{"claim":${claimJson}}`),
    claimJson,
    scopeJson,
  };
}

function isApprovalPendingBody(value: unknown): value is {
  readonly status: "approval_pending";
  readonly satisfiedRoles: readonly ApprovalRole[];
  readonly requiredThreshold: number;
} {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const record = value as { readonly status?: unknown };
  return record.status === "approval_pending";
}

interface AgentScopedClaimFixture {
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
}

function costSpikeAcknowledgementFixture(
  targetAgentId: AgentId,
  role: ApprovalRole,
): {
  readonly claim: CostSpikeAcknowledgementClaim;
  readonly scope: CostSpikeAcknowledgementScope;
} {
  const claim: CostSpikeAcknowledgementClaim = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim-cost-spike-12"),
    kind: "cost_spike_acknowledgement",
    agentId: targetAgentId,
    costCeilingId: "ceiling-main",
    thresholdBps: 9000,
    currentMicroUsd: 80,
    ceilingMicroUsd: 100,
  };
  const scope: CostSpikeAcknowledgementScope = {
    mode: "single_use",
    claimId: claim.claimId,
    claimKind: "cost_spike_acknowledgement",
    role,
    maxUses: 1,
    agentId: claim.agentId,
    costCeilingId: claim.costCeilingId,
  };
  return { claim, scope };
}

function endpointAllowlistExtensionFixture(
  targetAgentId: AgentId,
  role: ApprovalRole,
): {
  readonly claim: EndpointAllowlistExtensionClaim;
  readonly scope: EndpointAllowlistExtensionScope;
} {
  const claim: EndpointAllowlistExtensionClaim = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim-endpoint-12"),
    kind: "endpoint_allowlist_extension",
    agentId: targetAgentId,
    providerKind: asProviderKind("openai"),
    endpointOrigin: "https://api.openai.com",
    reason: "temporary endpoint access",
  };
  const scope: EndpointAllowlistExtensionScope = {
    mode: "single_use",
    claimId: claim.claimId,
    claimKind: "endpoint_allowlist_extension",
    role,
    maxUses: 1,
    agentId: claim.agentId,
    providerKind: claim.providerKind,
    endpointOrigin: claim.endpointOrigin,
  };
  return { claim, scope };
}

function credentialGrantToAgentFixture(
  targetAgentId: AgentId,
  role: ApprovalRole,
): {
  readonly claim: CredentialGrantToAgentClaim;
  readonly scope: CredentialGrantToAgentScope;
} {
  const claim: CredentialGrantToAgentClaim = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim-credential-grant-12"),
    kind: "credential_grant_to_agent",
    granteeAgentId: targetAgentId,
    credentialHandleId: asCredentialHandleId("cred_1234567890123456789012"),
    credentialScope: asCredentialScope("openai"),
  };
  const scope: CredentialGrantToAgentScope = {
    mode: "single_use",
    claimId: claim.claimId,
    claimKind: "credential_grant_to_agent",
    role,
    maxUses: 1,
    granteeAgentId: claim.granteeAgentId,
    credentialHandleId: claim.credentialHandleId,
  };
  return { claim, scope };
}

function registrationResponse(credentialId: string): RegistrationResponseJSON {
  const id = webAuthnCredentialId(credentialId);
  return {
    id,
    rawId: id,
    response: {
      clientDataJSON: webAuthnCredentialId("clientData"),
      attestationObject: webAuthnCredentialId("attestationObject"),
    },
    clientExtensionResults: {},
    type: "public-key",
  };
}

function assertionResponse(credentialId: string): AuthenticationResponseJSON {
  const id = webAuthnCredentialId(credentialId);
  return {
    id,
    rawId: id,
    response: {
      clientDataJSON: webAuthnCredentialId("clientData"),
      authenticatorData: webAuthnCredentialId("authenticatorData"),
      signature: webAuthnCredentialId("signature"),
    },
    clientExtensionResults: {},
    type: "public-key",
  };
}

function webAuthnCredentialId(label: string): string {
  return Buffer.from(label, "utf8").toString("base64url");
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

interface LogEntry {
  readonly event: string;
  readonly payload?: Readonly<Record<string, unknown>>;
}

interface PruneCall {
  readonly nowMs: number;
  readonly maxRows: number | undefined;
}

class RecordingLogger implements BrokerLogger {
  readonly infos: LogEntry[] = [];
  readonly warns: LogEntry[] = [];
  readonly errors: LogEntry[] = [];

  info(event: string, payload?: Readonly<Record<string, unknown>>): void {
    this.infos.push({ event, ...(payload === undefined ? {} : { payload }) });
  }

  warn(event: string, payload?: Readonly<Record<string, unknown>>): void {
    this.warns.push({ event, ...(payload === undefined ? {} : { payload }) });
  }

  error(event: string, payload?: Readonly<Record<string, unknown>>): void {
    this.errors.push({ event, ...(payload === undefined ? {} : { payload }) });
  }
}

type CeremonyFailure = Error | "null";

class FakeCeremony implements WebAuthnCeremony {
  readonly nextCounters = new Map<string, number>();
  expectedOrigins: readonly string[] = [];
  registrationVerificationOrigins: readonly string[] = [];
  registrationVerificationFailure: CeremonyFailure | null = null;
  authenticationVerificationFailure: CeremonyFailure | null = null;
  registrationOptionCalls = 0;
  registrationVerificationCalls = 0;
  authenticationCalls = 0;

  async generateRegistrationOptions(args: {
    readonly rpName: string;
    readonly rpId: string;
    readonly agentId: AgentId;
    readonly role: ApprovalRole;
    readonly challenge: string;
    readonly excludeCredentialIds: readonly string[];
  }): Promise<PublicKeyCredentialCreationOptionsJSON> {
    this.registrationOptionCalls += 1;
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
    this.registrationVerificationCalls += 1;
    expect(args.expectedChallenge).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(args.expectedOrigins).toEqual(this.expectedOrigins);
    expect(args.expectedRpId).toBe("localhost");
    this.registrationVerificationOrigins = args.expectedOrigins;
    if (this.registrationVerificationFailure instanceof Error) {
      throw this.registrationVerificationFailure;
    }
    if (this.registrationVerificationFailure === "null") {
      return null;
    }
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
    expect(args.expectedOrigins).toEqual(this.expectedOrigins);
    expect(args.expectedRpId).toBe("localhost");
    expect(args.credential.id).toBe(args.response.id);
    if (this.authenticationVerificationFailure instanceof Error) {
      throw this.authenticationVerificationFailure;
    }
    if (this.authenticationVerificationFailure === "null") {
      return null;
    }
    return {
      credentialId: args.response.id,
      newSignCount: this.nextCounters.get(args.response.id) ?? 2,
      userVerified: true,
    };
  }
}

interface PendingAuthentication {
  resolve(newSignCount: number): void;
}

class ControlledAuthenticationCeremony extends FakeCeremony {
  readonly pendingAuthentications: PendingAuthentication[] = [];

  override async verifyAuthentication(args: {
    readonly response: AuthenticationResponseJSON;
    readonly expectedChallenge: string;
    readonly expectedOrigins: readonly string[];
    readonly expectedRpId: string;
    readonly credential: WebAuthnCredential;
  }): Promise<WebAuthnAuthenticationVerification | null> {
    this.authenticationCalls += 1;
    expect(args.expectedChallenge).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(args.expectedOrigins).toEqual(this.expectedOrigins);
    expect(args.expectedRpId).toBe("localhost");
    expect(args.credential.id).toBe(args.response.id);
    return new Promise<WebAuthnAuthenticationVerification>((resolveFn) => {
      this.pendingAuthentications.push({
        resolve: (newSignCount: number): void => {
          resolveFn({
            credentialId: args.response.id,
            newSignCount,
            userVerified: true,
          });
        },
      });
    });
  }

  resolveAuthentication(index: number, newSignCount: number): void {
    const pending = this.pendingAuthentications[index];
    if (pending === undefined) {
      throw new Error(`missing pending authentication at index ${index}`);
    }
    pending.resolve(newSignCount);
  }
}

class DelayedConsumeStore implements WebAuthnStore {
  private readonly consumeWaiters: Array<() => void> = [];
  private released = false;
  pendingConsumes = 0;

  constructor(private readonly inner: WebAuthnStore) {}

  saveRegistrationChallenge(
    ...args: Parameters<WebAuthnStore["saveRegistrationChallenge"]>
  ): ReturnType<WebAuthnStore["saveRegistrationChallenge"]> {
    return this.inner.saveRegistrationChallenge(...args);
  }

  saveCosignChallenge(
    ...args: Parameters<WebAuthnStore["saveCosignChallenge"]>
  ): ReturnType<WebAuthnStore["saveCosignChallenge"]> {
    return this.inner.saveCosignChallenge(...args);
  }

  getChallenge(
    ...args: Parameters<WebAuthnStore["getChallenge"]>
  ): ReturnType<WebAuthnStore["getChallenge"]> {
    return this.inner.getChallenge(...args);
  }

  pruneExpired(
    ...args: Parameters<WebAuthnStore["pruneExpired"]>
  ): ReturnType<WebAuthnStore["pruneExpired"]> {
    return this.inner.pruneExpired(...args);
  }

  listCredentialsForAgent(
    ...args: Parameters<WebAuthnStore["listCredentialsForAgent"]>
  ): ReturnType<WebAuthnStore["listCredentialsForAgent"]> {
    return this.inner.listCredentialsForAgent(...args);
  }

  listCredentialsForAgentRole(
    ...args: Parameters<WebAuthnStore["listCredentialsForAgentRole"]>
  ): ReturnType<WebAuthnStore["listCredentialsForAgentRole"]> {
    return this.inner.listCredentialsForAgentRole(...args);
  }

  getCredential(
    ...args: Parameters<WebAuthnStore["getCredential"]>
  ): ReturnType<WebAuthnStore["getCredential"]> {
    return this.inner.getCredential(...args);
  }

  saveCredential(
    ...args: Parameters<WebAuthnStore["saveCredential"]>
  ): ReturnType<WebAuthnStore["saveCredential"]> {
    return this.inner.saveCredential(...args);
  }

  getConsumedToken(
    ...args: Parameters<WebAuthnStore["getConsumedToken"]>
  ): ReturnType<WebAuthnStore["getConsumedToken"]> {
    return this.inner.getConsumedToken(...args);
  }

  listSatisfiedRoles(
    ...args: Parameters<WebAuthnStore["listSatisfiedRoles"]>
  ): ReturnType<WebAuthnStore["listSatisfiedRoles"]> {
    return this.inner.listSatisfiedRoles(...args);
  }

  async consumeCosignChallenge(
    ...args: Parameters<WebAuthnStore["consumeCosignChallenge"]>
  ): ReturnType<WebAuthnStore["consumeCosignChallenge"]> {
    this.pendingConsumes += 1;
    if (!this.released) {
      await new Promise<void>((resolveFn) => {
        this.consumeWaiters.push(resolveFn);
      });
    }
    return await this.inner.consumeCosignChallenge(...args);
  }

  releaseConsumes(): void {
    this.released = true;
    for (const resolveFn of this.consumeWaiters.splice(0)) {
      resolveFn();
    }
  }
}

class RecordingPruneStore implements WebAuthnStore {
  constructor(
    private readonly inner: WebAuthnStore,
    private readonly pruneCalls: PruneCall[],
  ) {}

  saveRegistrationChallenge(
    ...args: Parameters<WebAuthnStore["saveRegistrationChallenge"]>
  ): ReturnType<WebAuthnStore["saveRegistrationChallenge"]> {
    return this.inner.saveRegistrationChallenge(...args);
  }

  saveCosignChallenge(
    ...args: Parameters<WebAuthnStore["saveCosignChallenge"]>
  ): ReturnType<WebAuthnStore["saveCosignChallenge"]> {
    return this.inner.saveCosignChallenge(...args);
  }

  pruneExpired(
    ...args: Parameters<WebAuthnStore["pruneExpired"]>
  ): ReturnType<WebAuthnStore["pruneExpired"]> {
    this.pruneCalls.push({ nowMs: args[0].nowMs, maxRows: args[0].maxRows });
    return this.inner.pruneExpired(...args);
  }

  getChallenge(
    ...args: Parameters<WebAuthnStore["getChallenge"]>
  ): ReturnType<WebAuthnStore["getChallenge"]> {
    return this.inner.getChallenge(...args);
  }

  listCredentialsForAgent(
    ...args: Parameters<WebAuthnStore["listCredentialsForAgent"]>
  ): ReturnType<WebAuthnStore["listCredentialsForAgent"]> {
    return this.inner.listCredentialsForAgent(...args);
  }

  listCredentialsForAgentRole(
    ...args: Parameters<WebAuthnStore["listCredentialsForAgentRole"]>
  ): ReturnType<WebAuthnStore["listCredentialsForAgentRole"]> {
    return this.inner.listCredentialsForAgentRole(...args);
  }

  getCredential(
    ...args: Parameters<WebAuthnStore["getCredential"]>
  ): ReturnType<WebAuthnStore["getCredential"]> {
    return this.inner.getCredential(...args);
  }

  saveCredential(
    ...args: Parameters<WebAuthnStore["saveCredential"]>
  ): ReturnType<WebAuthnStore["saveCredential"]> {
    return this.inner.saveCredential(...args);
  }

  getConsumedToken(
    ...args: Parameters<WebAuthnStore["getConsumedToken"]>
  ): ReturnType<WebAuthnStore["getConsumedToken"]> {
    return this.inner.getConsumedToken(...args);
  }

  listSatisfiedRoles(
    ...args: Parameters<WebAuthnStore["listSatisfiedRoles"]>
  ): ReturnType<WebAuthnStore["listSatisfiedRoles"]> {
    return this.inner.listSatisfiedRoles(...args);
  }

  consumeCosignChallenge(
    ...args: Parameters<WebAuthnStore["consumeCosignChallenge"]>
  ): ReturnType<WebAuthnStore["consumeCosignChallenge"]> {
    return this.inner.consumeCosignChallenge(...args);
  }
}

class SaveRegistrationChallengeFailingStore implements WebAuthnStore {
  constructor(
    private readonly inner: WebAuthnStore,
    private readonly error: Error,
  ) {}

  async saveRegistrationChallenge(): Promise<void> {
    throw this.error;
  }

  saveCosignChallenge(
    ...args: Parameters<WebAuthnStore["saveCosignChallenge"]>
  ): ReturnType<WebAuthnStore["saveCosignChallenge"]> {
    return this.inner.saveCosignChallenge(...args);
  }

  pruneExpired(
    ...args: Parameters<WebAuthnStore["pruneExpired"]>
  ): ReturnType<WebAuthnStore["pruneExpired"]> {
    return this.inner.pruneExpired(...args);
  }

  getChallenge(
    ...args: Parameters<WebAuthnStore["getChallenge"]>
  ): ReturnType<WebAuthnStore["getChallenge"]> {
    return this.inner.getChallenge(...args);
  }

  listCredentialsForAgent(
    ...args: Parameters<WebAuthnStore["listCredentialsForAgent"]>
  ): ReturnType<WebAuthnStore["listCredentialsForAgent"]> {
    return this.inner.listCredentialsForAgent(...args);
  }

  listCredentialsForAgentRole(
    ...args: Parameters<WebAuthnStore["listCredentialsForAgentRole"]>
  ): ReturnType<WebAuthnStore["listCredentialsForAgentRole"]> {
    return this.inner.listCredentialsForAgentRole(...args);
  }

  getCredential(
    ...args: Parameters<WebAuthnStore["getCredential"]>
  ): ReturnType<WebAuthnStore["getCredential"]> {
    return this.inner.getCredential(...args);
  }

  saveCredential(
    ...args: Parameters<WebAuthnStore["saveCredential"]>
  ): ReturnType<WebAuthnStore["saveCredential"]> {
    return this.inner.saveCredential(...args);
  }

  getConsumedToken(
    ...args: Parameters<WebAuthnStore["getConsumedToken"]>
  ): ReturnType<WebAuthnStore["getConsumedToken"]> {
    return this.inner.getConsumedToken(...args);
  }

  listSatisfiedRoles(
    ...args: Parameters<WebAuthnStore["listSatisfiedRoles"]>
  ): ReturnType<WebAuthnStore["listSatisfiedRoles"]> {
    return this.inner.listSatisfiedRoles(...args);
  }

  consumeCosignChallenge(
    ...args: Parameters<WebAuthnStore["consumeCosignChallenge"]>
  ): ReturnType<WebAuthnStore["consumeCosignChallenge"]> {
    return this.inner.consumeCosignChallenge(...args);
  }
}

class ListCredentialsForAgentFailingStore implements WebAuthnStore {
  constructor(private readonly inner: WebAuthnStore) {}

  saveRegistrationChallenge(
    ...args: Parameters<WebAuthnStore["saveRegistrationChallenge"]>
  ): ReturnType<WebAuthnStore["saveRegistrationChallenge"]> {
    return this.inner.saveRegistrationChallenge(...args);
  }

  saveCosignChallenge(
    ...args: Parameters<WebAuthnStore["saveCosignChallenge"]>
  ): ReturnType<WebAuthnStore["saveCosignChallenge"]> {
    return this.inner.saveCosignChallenge(...args);
  }

  pruneExpired(
    ...args: Parameters<WebAuthnStore["pruneExpired"]>
  ): ReturnType<WebAuthnStore["pruneExpired"]> {
    return this.inner.pruneExpired(...args);
  }

  async listCredentialsForAgent(
    _agentId: AgentId,
  ): Promise<readonly RegisteredWebAuthnCredential[]> {
    throw new WebAuthnStoreBusyError("test busy read");
  }

  getChallenge(
    ...args: Parameters<WebAuthnStore["getChallenge"]>
  ): ReturnType<WebAuthnStore["getChallenge"]> {
    return this.inner.getChallenge(...args);
  }

  listCredentialsForAgentRole(
    ...args: Parameters<WebAuthnStore["listCredentialsForAgentRole"]>
  ): ReturnType<WebAuthnStore["listCredentialsForAgentRole"]> {
    return this.inner.listCredentialsForAgentRole(...args);
  }

  getCredential(
    ...args: Parameters<WebAuthnStore["getCredential"]>
  ): ReturnType<WebAuthnStore["getCredential"]> {
    return this.inner.getCredential(...args);
  }

  saveCredential(
    ...args: Parameters<WebAuthnStore["saveCredential"]>
  ): ReturnType<WebAuthnStore["saveCredential"]> {
    return this.inner.saveCredential(...args);
  }

  getConsumedToken(
    ...args: Parameters<WebAuthnStore["getConsumedToken"]>
  ): ReturnType<WebAuthnStore["getConsumedToken"]> {
    return this.inner.getConsumedToken(...args);
  }

  listSatisfiedRoles(
    ...args: Parameters<WebAuthnStore["listSatisfiedRoles"]>
  ): ReturnType<WebAuthnStore["listSatisfiedRoles"]> {
    return this.inner.listSatisfiedRoles(...args);
  }

  consumeCosignChallenge(
    ...args: Parameters<WebAuthnStore["consumeCosignChallenge"]>
  ): ReturnType<WebAuthnStore["consumeCosignChallenge"]> {
    return this.inner.consumeCosignChallenge(...args);
  }
}

async function waitForPendingAuthentications(
  ceremony: ControlledAuthenticationCeremony,
  count: number,
): Promise<void> {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (ceremony.pendingAuthentications.length >= count) return;
    await new Promise<void>((resolveFn) => setTimeout(resolveFn, 0));
  }
  throw new Error(`timed out waiting for ${count} pending authentications`);
}

async function waitForPendingConsumes(store: DelayedConsumeStore, count: number): Promise<void> {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (store.pendingConsumes >= count) return;
    await new Promise<void>((resolveFn) => setTimeout(resolveFn, 0));
  }
  throw new Error(`timed out waiting for ${count} pending consumes`);
}

interface RegistrationChallengeResponse {
  readonly challengeId: string;
  readonly creationOptions: PublicKeyCredentialCreationOptionsJSON;
}

interface CosignChallengeResponse {
  readonly challengeId: string;
  readonly requestOptions: PublicKeyCredentialRequestOptionsJSON;
  readonly claim: Readonly<Record<string, unknown>>;
  readonly scope: Readonly<Record<string, unknown>>;
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
