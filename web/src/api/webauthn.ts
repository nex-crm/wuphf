import {
  type AuthenticationResponseJSON,
  type PublicKeyCredentialCreationOptionsJSON,
  type PublicKeyCredentialRequestOptionsJSON,
  type RegistrationResponseJSON,
  startAuthentication,
  startRegistration,
} from "@simplewebauthn/browser";
import type {
  ApprovalClaim,
  ApprovalClaimJsonValue,
  ApprovalRole,
  ApprovalScope,
  ApprovalScopeJsonValue,
  SignedApprovalTokenJsonValue,
} from "@wuphf/protocol";
import {
  approvalClaimFromJson,
  approvalClaimToJsonValue,
  approvalScopeFromJson,
  approvalScopeToJsonValue,
} from "@wuphf/protocol";

import { post } from "./client";

export type WebAuthnCreationOptionsJson =
  PublicKeyCredentialCreationOptionsJSON;
export type WebAuthnRequestOptionsJson = PublicKeyCredentialRequestOptionsJSON;
export type WebAuthnAttestationResponseJson = RegistrationResponseJSON;
export type WebAuthnAssertionResponseJson = AuthenticationResponseJSON;

export interface WebAuthnRegistrationChallengeRequest {
  readonly role: ApprovalRole;
}

export interface WebAuthnRegistrationChallengeResponse {
  readonly challengeId: string;
  readonly creationOptions: WebAuthnCreationOptionsJson;
}

export interface WebAuthnRegistrationVerifyRequest {
  readonly challengeId: string;
  readonly attestationResponse: WebAuthnAttestationResponseJson;
}

export interface WebAuthnRegistrationVerifyResponse {
  readonly credentialId: string;
  readonly role: ApprovalRole;
}

export interface WebAuthnCosignChallengeRequest {
  readonly claim: ApprovalClaimJsonValue;
  readonly scope: ApprovalScopeJsonValue;
}

export interface WebAuthnCosignChallengeInput {
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
}

interface WebAuthnCosignChallengeWireResponse {
  readonly challengeId: string;
  readonly requestOptions: WebAuthnRequestOptionsJson;
  readonly claim: ApprovalClaimJsonValue;
  readonly scope: ApprovalScopeJsonValue;
}

export interface WebAuthnCosignChallengeResponse {
  readonly challengeId: string;
  readonly requestOptions: WebAuthnRequestOptionsJson;
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
}

export interface WebAuthnCosignVerifyRequest {
  readonly challengeId: string;
  readonly assertionResponse: WebAuthnAssertionResponseJson;
}

export interface WebAuthnApprovalPendingResponse {
  readonly status: "approval_pending";
  readonly satisfiedRoles: readonly string[];
  readonly requiredThreshold: number;
}

export type WebAuthnCosignVerifyResponse =
  | SignedApprovalTokenJsonValue
  | WebAuthnApprovalPendingResponse;

export function isWebAuthnApprovalPendingResponse(
  response: unknown,
): response is WebAuthnApprovalPendingResponse {
  if (typeof response !== "object" || response === null) return false;
  const record = response as Record<string, unknown>;
  const { requiredThreshold, satisfiedRoles, status } = record;
  if (!Array.isArray(satisfiedRoles)) return false;
  if (!satisfiedRoles.every((role) => typeof role === "string")) return false;
  return (
    status === "approval_pending" &&
    typeof requiredThreshold === "number" &&
    Number.isFinite(requiredThreshold)
  );
}

export function toWebAuthnCosignChallengeRequest(
  input: WebAuthnCosignChallengeInput,
): WebAuthnCosignChallengeRequest {
  return {
    claim: approvalClaimToJsonValue(input.claim),
    scope: approvalScopeToJsonValue(input.scope),
  };
}

export function requestWebAuthnRegistrationChallenge(
  request: WebAuthnRegistrationChallengeRequest,
): Promise<WebAuthnRegistrationChallengeResponse> {
  return post<WebAuthnRegistrationChallengeResponse>(
    "/webauthn/registration/challenge",
    request,
  );
}

export function verifyWebAuthnRegistration(
  request: WebAuthnRegistrationVerifyRequest,
): Promise<WebAuthnRegistrationVerifyResponse> {
  return post<WebAuthnRegistrationVerifyResponse>(
    "/webauthn/registration/verify",
    request,
  );
}

export async function requestWebAuthnCosignChallenge(
  input: WebAuthnCosignChallengeInput,
): Promise<WebAuthnCosignChallengeResponse> {
  const response = await post<WebAuthnCosignChallengeWireResponse>(
    "/webauthn/cosign/challenge",
    toWebAuthnCosignChallengeRequest(input),
  );
  return {
    challengeId: response.challengeId,
    requestOptions: response.requestOptions,
    claim: approvalClaimFromJson(
      response.claim,
      "webauthnCosignChallenge.claim",
    ),
    scope: approvalScopeFromJson(
      response.scope,
      "webauthnCosignChallenge.scope",
    ),
  };
}

export function verifyWebAuthnCosign(
  request: WebAuthnCosignVerifyRequest,
): Promise<WebAuthnCosignVerifyResponse> {
  return post<WebAuthnCosignVerifyResponse>("/webauthn/cosign/verify", request);
}

export function runWebAuthnRegistrationCeremony(
  creationOptions: WebAuthnCreationOptionsJson,
): Promise<WebAuthnAttestationResponseJson> {
  return startRegistration({ optionsJSON: creationOptions });
}

export function runWebAuthnAuthenticationCeremony(
  requestOptions: WebAuthnRequestOptionsJson,
): Promise<WebAuthnAssertionResponseJson> {
  return startAuthentication({ optionsJSON: requestOptions });
}
