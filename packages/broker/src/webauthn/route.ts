import { randomBytes } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";

import type {
  AuthenticationExtensionsClientOutputs,
  AuthenticationResponseJSON,
  AuthenticatorAttachment,
  AuthenticatorTransportFuture,
  RegistrationResponseJSON,
  WebAuthnCredential,
} from "@simplewebauthn/server";
import {
  type AgentId,
  type ApprovalClaim,
  type ApprovalRole,
  type ApprovalScope,
  type ApprovalTokenId,
  approvalClaimFromJson,
  approvalClaimToJsonValue,
  approvalScopeFromJson,
  approvalScopeToJsonValue,
  asApprovalTokenId,
  asTimestampMs,
  canonicalJSON,
  type JsonValue,
  MAX_APPROVAL_TOKEN_LIFETIME_MS,
  type SignedApprovalToken,
  sha256Hex,
  signedApprovalTokenToJsonValue,
} from "@wuphf/protocol";

import { agentIdForBearer } from "../auth.ts";
import {
  type CosignChallengeRecord,
  type RegisteredWebAuthnCredential,
  thresholdForClaimKind,
  type WebAuthnRouteDeps,
} from "./types.ts";

const MAX_WEBAUTHN_ROUTE_BODY_BYTES = 256 * 1024;
const ALLOW_WEBAUTHN_ROUTE = "POST";
const BASE64URL_RE = /^[A-Za-z0-9_-]+$/;
const TOKEN_ID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const APPROVAL_ROLE_SET: ReadonlySet<string> = new Set(["viewer", "approver", "host"]);

const REGISTRATION_CHALLENGE_KEYS: ReadonlySet<string> = new Set(["role"]);
const REGISTRATION_VERIFY_KEYS: ReadonlySet<string> = new Set([
  "challengeId",
  "attestationResponse",
]);
const COSIGN_CHALLENGE_KEYS: ReadonlySet<string> = new Set(["claim", "scope"]);
const COSIGN_VERIFY_KEYS: ReadonlySet<string> = new Set(["challengeId", "assertionResponse"]);
const CREDENTIAL_RESPONSE_KEYS: ReadonlySet<string> = new Set([
  "id",
  "rawId",
  "response",
  "authenticatorAttachment",
  "clientExtensionResults",
  "type",
]);
const ATTESTATION_RESPONSE_KEYS: ReadonlySet<string> = new Set([
  "clientDataJSON",
  "attestationObject",
  "authenticatorData",
  "transports",
  "publicKeyAlgorithm",
  "publicKey",
]);
const ASSERTION_RESPONSE_KEYS: ReadonlySet<string> = new Set([
  "clientDataJSON",
  "authenticatorData",
  "signature",
  "userHandle",
]);
const EXTENSION_RESULTS_KEYS: ReadonlySet<string> = new Set([
  "appid",
  "credProps",
  "hmacCreateSecret",
]);
const CRED_PROPS_KEYS: ReadonlySet<string> = new Set(["rk"]);
const AUTHENTICATOR_TRANSPORT_SET: ReadonlySet<string> = new Set([
  "ble",
  "cable",
  "hybrid",
  "internal",
  "nfc",
  "smart-card",
  "usb",
]);

export async function handleWebAuthnRoute(
  req: IncomingMessage,
  res: ServerResponse,
  pathname: string,
  deps: WebAuthnRouteDeps,
): Promise<boolean> {
  if (!pathname.startsWith("/api/webauthn/")) return false;
  if (!isKnownWebAuthnPath(pathname)) return false;
  if (req.method !== "POST") {
    methodNotAllowed(res);
    return true;
  }
  const agentId = agentIdForBearer(req, deps.tokenAgentIds);
  if (agentId === null) {
    deps.logger.warn("webauthn_rejected", { reason: "no_bearer_binding", route: pathname });
    writeJson(res, 403, { error: "webauthn_not_authorized" });
    return true;
  }

  if (pathname === "/api/webauthn/registration/challenge") {
    await handleRegistrationChallenge(req, res, agentId, deps);
    return true;
  }
  if (pathname === "/api/webauthn/registration/verify") {
    await handleRegistrationVerify(req, res, agentId, deps);
    return true;
  }
  if (pathname === "/api/webauthn/cosign/challenge") {
    await handleCosignChallenge(req, res, agentId, deps);
    return true;
  }
  if (pathname === "/api/webauthn/cosign/verify") {
    await handleCosignVerify(req, res, agentId, deps);
    return true;
  }
  return false;
}

async function handleRegistrationChallenge(
  req: IncomingMessage,
  res: ServerResponse,
  agentId: AgentId,
  deps: WebAuthnRouteDeps,
): Promise<void> {
  let body: RegistrationChallengeBody;
  try {
    body = registrationChallengeBodyFromJson(await readJsonBody(req));
  } catch (err) {
    writeJson(res, 400, { error: messageOf(err) });
    return;
  }

  const nowMs = readClock(deps);
  const expiresAtMs = safeExpiryMs(nowMs, deps.challengeTtlMs);
  const challengeId = randomBase64Url(32);
  const challenge = randomBase64Url(32);
  const existingCredentials = await deps.store.listCredentialsForAgent(agentId);
  const options = await deps.ceremony.generateRegistrationOptions({
    rpName: deps.rpName,
    rpId: deps.rpId,
    agentId,
    role: body.role,
    challenge,
    excludeCredentialIds: existingCredentials.map((credential) => credential.credentialId),
  });

  await deps.store.saveRegistrationChallenge({
    challengeId,
    challenge,
    role: body.role,
    issuedToAgentId: agentId,
    createdAtMs: nowMs,
    expiresAtMs,
  });
  writeJson(res, 200, { challengeId, options });
}

async function handleRegistrationVerify(
  req: IncomingMessage,
  res: ServerResponse,
  agentId: AgentId,
  deps: WebAuthnRouteDeps,
): Promise<void> {
  let body: RegistrationVerifyBody;
  try {
    body = registrationVerifyBodyFromJson(await readJsonBody(req));
  } catch (err) {
    writeJson(res, 400, { error: messageOf(err) });
    return;
  }

  const challenge = await deps.store.getChallenge(body.challengeId);
  if (challenge === null || challenge.type !== "registration") {
    writeJson(res, 404, { error: "challenge_not_found" });
    return;
  }
  if (challenge.issuedToAgentId !== agentId) {
    writeJson(res, 403, { error: "wrong_issued_to_agent" });
    return;
  }
  if (challenge.consumedAtMs !== null) {
    writeJson(res, 409, { error: "challenge_consumed" });
    return;
  }
  const nowMs = readClock(deps);
  if (challenge.expiresAtMs <= nowMs) {
    writeJson(res, 400, { error: "challenge_expired" });
    return;
  }

  let verification: Awaited<ReturnType<WebAuthnRouteDeps["ceremony"]["verifyRegistration"]>>;
  try {
    verification = await deps.ceremony.verifyRegistration({
      response: body.attestationResponse,
      expectedChallenge: challenge.challenge,
      expectedOrigins: deps.allowedOrigins,
      expectedRpId: deps.rpId,
    });
  } catch {
    deps.logger.warn("webauthn_registration_verification_failed");
    writeJson(res, 400, { error: "registration_verification_failed" });
    return;
  }
  if (verification === null) {
    writeJson(res, 400, { error: "registration_verification_failed" });
    return;
  }
  const existing = await deps.store.getCredential(verification.credentialId);
  if (existing !== null) {
    writeJson(res, 409, { error: "credential_already_registered" });
    return;
  }

  await deps.store.saveCredential({
    challengeId: challenge.challengeId,
    credential: {
      credentialId: verification.credentialId,
      publicKey: verification.publicKey,
      signCount: verification.signCount,
      role: challenge.role,
      agentId,
      createdAtMs: nowMs,
    },
    consumedAtMs: nowMs,
  });
  writeJson(res, 200, { credentialId: verification.credentialId, role: challenge.role });
}

async function handleCosignChallenge(
  req: IncomingMessage,
  res: ServerResponse,
  agentId: AgentId,
  deps: WebAuthnRouteDeps,
): Promise<void> {
  let body: CosignChallengeBody;
  try {
    body = cosignChallengeBodyFromJson(await readJsonBody(req));
    assertClaimScopeBinding(body.claim, body.scope);
  } catch (err) {
    writeJson(res, 400, { error: messageOf(err) });
    return;
  }

  if (!deps.trustedRoles.includes(body.scope.role)) {
    writeJson(res, 403, { error: "untrusted_approval_role" });
    return;
  }
  const credentials = await deps.store.listCredentialsForAgentRole({
    agentId,
    role: body.scope.role,
  });
  if (credentials.length === 0) {
    writeJson(res, 409, { error: "no_registered_credentials_for_role" });
    return;
  }

  const nowMs = readClock(deps);
  const expiresAtMs = asTimestampMs(safeExpiryMs(nowMs, deps.challengeTtlMs));
  const notBeforeMs = asTimestampMs(nowMs);
  const challengeId = randomBase64Url(32);
  const challenge = randomBase64Url(32);
  const tokenId = generateApprovalTokenId();
  const hashes = hashClaimScope(body.claim, body.scope);
  const options = await deps.ceremony.generateAuthenticationOptions({
    rpId: deps.rpId,
    challenge,
    allowCredentialIds: credentials.map((credential) => credential.credentialId),
  });

  await deps.store.saveCosignChallenge({
    challengeId,
    challenge,
    tokenId,
    claim: body.claim,
    scope: body.scope,
    claimScopeHash: hashes.claimScopeHash,
    approvalGroupHash: hashes.approvalGroupHash,
    issuedToAgentId: agentId,
    notBeforeMs,
    expiresAtMs,
    createdAtMs: nowMs,
  });
  writeJson(res, 200, { challengeId, options });
}

async function handleCosignVerify(
  req: IncomingMessage,
  res: ServerResponse,
  agentId: AgentId,
  deps: WebAuthnRouteDeps,
): Promise<void> {
  let envelope: CosignVerifyEnvelope;
  try {
    envelope = cosignVerifyEnvelopeFromJson(await readJsonBody(req));
  } catch (err) {
    writeJson(res, 400, { error: messageOf(err) });
    return;
  }

  const challenge = await deps.store.getChallenge(envelope.challengeId);
  if (challenge === null || challenge.type !== "cosign") {
    writeJson(res, 404, { error: "challenge_not_found" });
    return;
  }
  if (challenge.issuedToAgentId !== agentId) {
    writeJson(res, 403, { error: "wrong_issued_to_agent" });
    return;
  }
  const replay = await deps.store.getConsumedToken(challenge.tokenId);
  if (replay !== null) {
    writeJson(res, 200, replay.responseJson);
    return;
  }
  if (challenge.consumedAtMs !== null) {
    writeJson(res, 409, { error: "challenge_consumed" });
    return;
  }
  const nowMs = readClock(deps);
  if (challenge.expiresAtMs <= nowMs) {
    writeJson(res, 400, { error: "challenge_expired" });
    return;
  }

  let assertionResponse: AuthenticationResponseJSON;
  try {
    assertionResponse = authenticationResponseFromJson(
      envelope.assertionResponse,
      "cosignVerify.assertionResponse",
    );
  } catch (err) {
    writeJson(res, 400, { error: messageOf(err) });
    return;
  }

  const credential = await deps.store.getCredential(assertionResponse.id);
  if (credential === null) {
    writeJson(res, 403, { error: "unknown_credential" });
    return;
  }
  if (credential.agentId !== agentId) {
    writeJson(res, 403, { error: "wrong_credential_agent" });
    return;
  }
  if (credential.role !== challenge.scope.role) {
    writeJson(res, 403, { error: "wrong_credential_role" });
    return;
  }
  if (!deps.trustedRoles.includes(credential.role)) {
    writeJson(res, 403, { error: "untrusted_approval_role" });
    return;
  }

  let verification: Awaited<ReturnType<WebAuthnRouteDeps["ceremony"]["verifyAuthentication"]>>;
  try {
    verification = await deps.ceremony.verifyAuthentication({
      response: assertionResponse,
      expectedChallenge: challenge.challenge,
      expectedOrigins: deps.allowedOrigins,
      expectedRpId: deps.rpId,
      credential: credentialForSimpleWebAuthn(credential),
    });
  } catch {
    deps.logger.warn("webauthn_assertion_verification_failed");
    writeJson(res, 400, { error: "assertion_verification_failed" });
    return;
  }
  if (verification === null) {
    writeJson(res, 400, { error: "assertion_verification_failed" });
    return;
  }
  if (verification.credentialId !== credential.credentialId) {
    writeJson(res, 400, { error: "credential_id_mismatch" });
    return;
  }
  if (!verification.userVerified) {
    writeJson(res, 400, { error: "user_verification_required" });
    return;
  }
  if (credential.signCount > 0 && verification.newSignCount <= credential.signCount) {
    writeJson(res, 400, { error: "sign_count_replay" });
    return;
  }

  const responseJson = await buildCosignResponseJson({
    challenge,
    assertionResponse,
    credentialRole: credential.role,
    newSignCount: verification.newSignCount,
    agentId,
    nowMs,
    deps,
  });
  const outcome = isApprovalPendingResponse(responseJson) ? "approval_pending" : "approved";
  const consumedReplay = await deps.store.consumeCosignChallenge({
    challengeId: challenge.challengeId,
    tokenId: challenge.tokenId,
    credentialId: credential.credentialId,
    newSignCount: verification.newSignCount,
    outcome,
    responseJson,
    role: credential.role,
    approvalGroupHash: challenge.approvalGroupHash,
    issuedToAgentId: agentId,
    expiresAtMs: challenge.expiresAtMs,
    consumedAtMs: nowMs,
  });
  writeJson(res, 200, consumedReplay?.responseJson ?? responseJson);
}

async function buildCosignResponseJson(args: {
  readonly challenge: CosignChallengeRecord;
  readonly assertionResponse: AuthenticationResponseJSON;
  readonly credentialRole: ApprovalRole;
  readonly newSignCount: number;
  readonly agentId: AgentId;
  readonly nowMs: number;
  readonly deps: WebAuthnRouteDeps;
}): Promise<JsonValue> {
  const priorRoles = await args.deps.store.listSatisfiedRoles({
    approvalGroupHash: args.challenge.approvalGroupHash,
    issuedToAgentId: args.agentId,
    nowMs: args.nowMs,
  });
  const satisfiedRoles = sortedUniqueRoles([...priorRoles, args.credentialRole]);
  const requiredThreshold = thresholdForClaimKind(args.challenge.claim.kind, args.deps);
  if (satisfiedRoles.length < requiredThreshold) {
    return {
      status: "approval_pending",
      satisfiedRoles,
      requiredThreshold,
    };
  }

  const token: SignedApprovalToken = {
    schemaVersion: 1,
    tokenId: args.challenge.tokenId,
    claim: args.challenge.claim,
    scope: args.challenge.scope,
    notBefore: args.challenge.notBeforeMs,
    expiresAt: args.challenge.expiresAtMs,
    issuedTo: args.agentId,
    signature: {
      credentialId: args.assertionResponse.id,
      authenticatorData: args.assertionResponse.response.authenticatorData,
      clientDataJson: args.assertionResponse.response.clientDataJSON,
      signature: args.assertionResponse.response.signature,
      ...(args.assertionResponse.response.userHandle === undefined
        ? {}
        : { userHandle: args.assertionResponse.response.userHandle }),
    },
  };
  return jsonValue(signedApprovalTokenToJsonValue(token));
}

function credentialForSimpleWebAuthn(credential: RegisteredWebAuthnCredential): WebAuthnCredential {
  return {
    id: credential.credentialId,
    publicKey: credential.publicKey,
    counter: credential.signCount,
  };
}

function hashClaimScope(
  claim: ApprovalClaim,
  scope: ApprovalScope,
): {
  readonly claimScopeHash: ReturnType<typeof sha256Hex>;
  readonly approvalGroupHash: ReturnType<typeof sha256Hex>;
} {
  const claimJson = approvalClaimToJsonValue(claim);
  const scopeJson = approvalScopeToJsonValue(scope);
  return {
    claimScopeHash: sha256Hex(canonicalJSON({ claim: claimJson, scope: scopeJson })),
    approvalGroupHash: sha256Hex(canonicalJSON({ claim: claimJson })),
  };
}

interface RegistrationChallengeBody {
  readonly role: ApprovalRole;
}

interface RegistrationVerifyBody {
  readonly challengeId: string;
  readonly attestationResponse: RegistrationResponseJSON;
}

interface CosignChallengeBody {
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
}

interface CosignVerifyEnvelope {
  readonly challengeId: string;
  readonly assertionResponse: unknown;
}

function registrationChallengeBodyFromJson(value: unknown): RegistrationChallengeBody {
  const record = requireRecord(value, "registrationChallenge");
  assertKnownKeys(record, "registrationChallenge", REGISTRATION_CHALLENGE_KEYS);
  return {
    role: roleFromJson(requiredString(record, "role", "registrationChallenge")),
  };
}

function registrationVerifyBodyFromJson(value: unknown): RegistrationVerifyBody {
  const record = requireRecord(value, "registrationVerify");
  assertKnownKeys(record, "registrationVerify", REGISTRATION_VERIFY_KEYS);
  return {
    challengeId: base64UrlStringFromJson(
      requiredString(record, "challengeId", "registrationVerify"),
      "registrationVerify.challengeId",
    ),
    attestationResponse: registrationResponseFromJson(
      requiredField(record, "attestationResponse", "registrationVerify"),
      "registrationVerify.attestationResponse",
    ),
  };
}

function cosignChallengeBodyFromJson(value: unknown): CosignChallengeBody {
  const record = requireRecord(value, "cosignChallenge");
  assertKnownKeys(record, "cosignChallenge", COSIGN_CHALLENGE_KEYS);
  return {
    claim: approvalClaimFromJson(requiredField(record, "claim", "cosignChallenge")),
    scope: approvalScopeFromJson(requiredField(record, "scope", "cosignChallenge")),
  };
}

function cosignVerifyEnvelopeFromJson(value: unknown): CosignVerifyEnvelope {
  const record = requireRecord(value, "cosignVerify");
  assertKnownKeys(record, "cosignVerify", COSIGN_VERIFY_KEYS);
  return {
    challengeId: base64UrlStringFromJson(
      requiredString(record, "challengeId", "cosignVerify"),
      "cosignVerify.challengeId",
    ),
    assertionResponse: requiredField(record, "assertionResponse", "cosignVerify"),
  };
}

function registrationResponseFromJson(value: unknown, path: string): RegistrationResponseJSON {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, CREDENTIAL_RESPONSE_KEYS);
  const response = requireRecord(requiredField(record, "response", path), `${path}.response`);
  assertKnownKeys(response, `${path}.response`, ATTESTATION_RESPONSE_KEYS);
  const id = base64UrlStringFromJson(requiredString(record, "id", path), `${path}.id`);
  const rawId = base64UrlStringFromJson(requiredString(record, "rawId", path), `${path}.rawId`);
  if (rawId !== id) {
    throw new Error(`${path}.rawId: must match id`);
  }
  const authenticatorAttachment = optionalAuthenticatorAttachment(
    record,
    "authenticatorAttachment",
    path,
  );
  const authenticatorData = optionalBase64UrlString(
    response,
    "authenticatorData",
    `${path}.response`,
  );
  const transports = optionalTransports(response, `${path}.response`);
  const publicKeyAlgorithm = optionalSafeInteger(
    response,
    "publicKeyAlgorithm",
    `${path}.response`,
  );
  const publicKey = optionalBase64UrlString(response, "publicKey", `${path}.response`);
  return {
    id,
    rawId,
    response: {
      clientDataJSON: base64UrlStringFromJson(
        requiredString(response, "clientDataJSON", `${path}.response`),
        `${path}.response.clientDataJSON`,
      ),
      attestationObject: base64UrlStringFromJson(
        requiredString(response, "attestationObject", `${path}.response`),
        `${path}.response.attestationObject`,
      ),
      ...(authenticatorData === undefined ? {} : { authenticatorData }),
      ...(transports === undefined ? {} : { transports }),
      ...(publicKeyAlgorithm === undefined ? {} : { publicKeyAlgorithm }),
      ...(publicKey === undefined ? {} : { publicKey }),
    },
    ...(authenticatorAttachment === undefined ? {} : { authenticatorAttachment }),
    clientExtensionResults: extensionResultsFromJson(
      requiredField(record, "clientExtensionResults", path),
      `${path}.clientExtensionResults`,
    ),
    type: publicKeyTypeFromJson(requiredString(record, "type", path), `${path}.type`),
  };
}

function authenticationResponseFromJson(value: unknown, path: string): AuthenticationResponseJSON {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, CREDENTIAL_RESPONSE_KEYS);
  const response = requireRecord(requiredField(record, "response", path), `${path}.response`);
  assertKnownKeys(response, `${path}.response`, ASSERTION_RESPONSE_KEYS);
  const authenticatorAttachment = optionalAuthenticatorAttachment(
    record,
    "authenticatorAttachment",
    path,
  );
  const id = base64UrlStringFromJson(requiredString(record, "id", path), `${path}.id`);
  const rawId = base64UrlStringFromJson(requiredString(record, "rawId", path), `${path}.rawId`);
  if (rawId !== id) {
    throw new Error(`${path}.rawId: must match id`);
  }
  const userHandle = optionalBase64UrlString(response, "userHandle", `${path}.response`);
  return {
    id,
    rawId,
    response: {
      clientDataJSON: base64UrlStringFromJson(
        requiredString(response, "clientDataJSON", `${path}.response`),
        `${path}.response.clientDataJSON`,
      ),
      authenticatorData: base64UrlStringFromJson(
        requiredString(response, "authenticatorData", `${path}.response`),
        `${path}.response.authenticatorData`,
      ),
      signature: base64UrlStringFromJson(
        requiredString(response, "signature", `${path}.response`),
        `${path}.response.signature`,
      ),
      ...(userHandle === undefined ? {} : { userHandle }),
    },
    ...(authenticatorAttachment === undefined ? {} : { authenticatorAttachment }),
    clientExtensionResults: extensionResultsFromJson(
      requiredField(record, "clientExtensionResults", path),
      `${path}.clientExtensionResults`,
    ),
    type: publicKeyTypeFromJson(requiredString(record, "type", path), `${path}.type`),
  };
}

function assertClaimScopeBinding(claim: ApprovalClaim, scope: ApprovalScope): void {
  if (claim.claimId !== scope.claimId) {
    throw new Error("cosignChallenge.scope.claimId: must match claim.claimId");
  }
  if (claim.kind !== scope.claimKind) {
    throw new Error("cosignChallenge.scope.claimKind: must match claim.kind");
  }
  switch (claim.kind) {
    case "cost_spike_acknowledgement":
      if (scope.claimKind !== claim.kind) return;
      assertSame(scope.agentId, claim.agentId, "agentId");
      assertSame(scope.costCeilingId, claim.costCeilingId, "costCeilingId");
      return;
    case "endpoint_allowlist_extension":
      if (scope.claimKind !== claim.kind) return;
      assertSame(scope.agentId, claim.agentId, "agentId");
      assertSame(scope.providerKind, claim.providerKind, "providerKind");
      assertSame(scope.endpointOrigin, claim.endpointOrigin, "endpointOrigin");
      return;
    case "credential_grant_to_agent":
      if (scope.claimKind !== claim.kind) return;
      assertSame(scope.granteeAgentId, claim.granteeAgentId, "granteeAgentId");
      assertSame(scope.credentialHandleId, claim.credentialHandleId, "credentialHandleId");
      return;
    case "receipt_co_sign":
      if (scope.claimKind !== claim.kind) return;
      assertSame(scope.receiptId, claim.receiptId, "receiptId");
      assertSame(scope.writeId, claim.writeId, "writeId");
      assertSame(scope.frozenArgsHash, claim.frozenArgsHash, "frozenArgsHash");
      return;
  }
}

function assertSame(left: unknown, right: unknown, field: string): void {
  if (left !== right) {
    throw new Error(`cosignChallenge.scope.${field}: must match claim.${field}`);
  }
}

function readClock(deps: WebAuthnRouteDeps): number {
  const nowMs = deps.clock.now();
  if (!Number.isSafeInteger(nowMs) || nowMs < 0) {
    throw new Error(`webauthn clock returned invalid timestamp: ${nowMs}`);
  }
  return nowMs;
}

function safeExpiryMs(nowMs: number, ttlMs: number): number {
  if (!Number.isSafeInteger(ttlMs) || ttlMs <= 0 || ttlMs > MAX_APPROVAL_TOKEN_LIFETIME_MS) {
    throw new Error(`webauthn challenge TTL must be 1..${MAX_APPROVAL_TOKEN_LIFETIME_MS} ms`);
  }
  const expiresAtMs = nowMs + ttlMs;
  if (!Number.isSafeInteger(expiresAtMs)) {
    throw new Error("webauthn challenge expiry is outside the safe integer range");
  }
  return expiresAtMs;
}

function sortedUniqueRoles(values: readonly ApprovalRole[]): readonly ApprovalRole[] {
  return [...new Set(values)].sort(compareRoles);
}

function compareRoles(a: ApprovalRole, b: ApprovalRole): number {
  return roleOrder(a) - roleOrder(b);
}

function roleOrder(role: ApprovalRole): number {
  return role === "viewer" ? 0 : role === "approver" ? 1 : 2;
}

function isApprovalPendingResponse(value: JsonValue): boolean {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const record = value as { readonly status?: JsonValue };
  return record.status === "approval_pending";
}

function jsonValue(value: unknown): JsonValue {
  canonicalJSON(value);
  return value as JsonValue;
}

async function readJsonBody(req: IncomingMessage): Promise<unknown> {
  return JSON.parse(await readBody(req, MAX_WEBAUTHN_ROUTE_BODY_BYTES)) as unknown;
}

async function readBody(req: IncomingMessage, maxBytes: number): Promise<string> {
  let total = 0;
  const chunks: Buffer[] = [];
  for await (const chunk of req) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk), "utf8");
    total += buffer.length;
    if (total > maxBytes) {
      throw new Error("webauthn route body too large");
    }
    chunks.push(buffer);
  }
  return Buffer.concat(chunks).toString("utf8");
}

function requireRecord(value: unknown, path: string): Readonly<Record<string, unknown>> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${path}: must be an object`);
  }
  return value as Readonly<Record<string, unknown>>;
}

function assertKnownKeys(
  record: Readonly<Record<string, unknown>>,
  path: string,
  allowed: ReadonlySet<string>,
): void {
  for (const key of Object.keys(record)) {
    if (!allowed.has(key)) {
      throw new Error(`${path}.${key}: key is not allowed`);
    }
  }
}

function requiredField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): unknown {
  if (!Object.hasOwn(record, key)) {
    throw new Error(`${path}.${key}: is required`);
  }
  return record[key];
}

function requiredString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string {
  const value = requiredField(record, key, path);
  if (typeof value !== "string") {
    throw new Error(`${path}.${key}: must be a string`);
  }
  return value;
}

function requiredBoolean(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): boolean {
  const value = requiredField(record, key, path);
  if (typeof value !== "boolean") {
    throw new Error(`${path}.${key}: must be a boolean`);
  }
  return value;
}

function optionalAuthenticatorAttachment(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): AuthenticatorAttachment | undefined {
  if (!Object.hasOwn(record, key)) return undefined;
  const value = record[key];
  if (value === "cross-platform" || value === "platform") {
    return value;
  }
  throw new Error(`${path}.${key}: must be a supported authenticator attachment`);
}

function optionalSafeInteger(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): number | undefined {
  if (!Object.hasOwn(record, key)) return undefined;
  const value = record[key];
  if (typeof value !== "number" || !Number.isSafeInteger(value)) {
    throw new Error(`${path}.${key}: must be a safe integer`);
  }
  return value;
}

function optionalBase64UrlString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string | undefined {
  if (!Object.hasOwn(record, key)) return undefined;
  return base64UrlStringFromJson(requiredString(record, key, path), `${path}.${key}`);
}

function optionalTransports(
  record: Readonly<Record<string, unknown>>,
  path: string,
): AuthenticatorTransportFuture[] | undefined {
  if (!Object.hasOwn(record, "transports")) return undefined;
  const value = requiredField(record, "transports", path);
  if (!Array.isArray(value)) {
    throw new Error(`${path}.transports: must be a string array`);
  }
  return value.map((item, index) => {
    if (typeof item !== "string" || !AUTHENTICATOR_TRANSPORT_SET.has(item)) {
      throw new Error(`${path}.transports/${index}: must be a supported authenticator transport`);
    }
    return item as AuthenticatorTransportFuture;
  });
}

function extensionResultsFromJson(
  value: unknown,
  path: string,
): AuthenticationExtensionsClientOutputs {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, EXTENSION_RESULTS_KEYS);
  const result: AuthenticationExtensionsClientOutputs = {};
  if (Object.hasOwn(record, "appid")) {
    result.appid = requiredBoolean(record, "appid", path);
  }
  if (Object.hasOwn(record, "hmacCreateSecret")) {
    result.hmacCreateSecret = requiredBoolean(record, "hmacCreateSecret", path);
  }
  if (Object.hasOwn(record, "credProps")) {
    const credProps = requireRecord(requiredField(record, "credProps", path), `${path}.credProps`);
    assertKnownKeys(credProps, `${path}.credProps`, CRED_PROPS_KEYS);
    const rk = Object.hasOwn(credProps, "rk")
      ? requiredBoolean(credProps, "rk", `${path}.credProps`)
      : undefined;
    result.credProps = rk === undefined ? {} : { rk };
  }
  return result;
}

function publicKeyTypeFromJson(value: string, path: string): "public-key" {
  if (value !== "public-key") {
    throw new Error(`${path}: must be public-key`);
  }
  return value;
}

function base64UrlStringFromJson(value: string, path: string): string {
  if (value.length === 0 || !BASE64URL_RE.test(value)) {
    throw new Error(`${path}: must be a base64url string`);
  }
  return value;
}

function roleFromJson(value: string): ApprovalRole {
  if (APPROVAL_ROLE_SET.has(value)) {
    return value as ApprovalRole;
  }
  throw new Error("registrationChallenge.role: not a supported approval role");
}

function randomBase64Url(byteLength: number): string {
  return randomBytes(byteLength).toString("base64url");
}

function generateApprovalTokenId(): ApprovalTokenId {
  const bytes = randomBytes(26);
  let tokenId = "";
  for (const byte of bytes) {
    tokenId += TOKEN_ID_ALPHABET[byte & 31] ?? "0";
  }
  return asApprovalTokenId(tokenId);
}

function writeJson(res: ServerResponse, status: number, bodyValue: unknown): void {
  const body = JSON.stringify(bodyValue);
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(Buffer.byteLength(body, "utf8")),
  });
  res.end(body);
}

function methodNotAllowed(res: ServerResponse): void {
  const body = JSON.stringify({ error: "method_not_allowed" });
  res.writeHead(405, {
    Allow: ALLOW_WEBAUTHN_ROUTE,
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Length": String(Buffer.byteLength(body, "utf8")),
  });
  res.end(body);
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function isKnownWebAuthnPath(pathname: string): boolean {
  return (
    pathname === "/api/webauthn/registration/challenge" ||
    pathname === "/api/webauthn/registration/verify" ||
    pathname === "/api/webauthn/cosign/challenge" ||
    pathname === "/api/webauthn/cosign/verify"
  );
}
