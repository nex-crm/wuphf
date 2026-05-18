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

import { ApiError, post } from "./client";

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

export function describeWebAuthnBrokerStorageError(
  error: unknown,
): string | null {
  const code = brokerErrorCode(error);
  if (code === "store_busy") {
    const retryAfter = error instanceof ApiError ? error.retryAfter : null;
    return `The broker's WebAuthn storage is busy. Try again ${retryAfterPhrase(retryAfter)}.`;
  }
  if (code === "store_full") {
    return "The broker cannot save WebAuthn state because local storage is full. Free disk space and restart the broker.";
  }
  if (code === "storage_error") {
    return "The broker could not access WebAuthn storage. Restart the broker and try again.";
  }
  return null;
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

function brokerErrorCode(error: unknown): string | null {
  if (error instanceof ApiError) {
    return error.errorCode;
  }
  const message = error instanceof Error ? error.message : String(error);
  try {
    const parsed = JSON.parse(message) as unknown;
    if (
      typeof parsed !== "object" ||
      parsed === null ||
      Array.isArray(parsed)
    ) {
      return null;
    }
    const value = (parsed as Readonly<Record<string, unknown>>).error;
    return typeof value === "string" ? value : null;
  } catch {
    return null;
  }
}

function retryAfterPhrase(retryAfter: string | null): string {
  if (retryAfter === null || retryAfter.trim().length === 0) {
    return "shortly";
  }
  const seconds = Number(retryAfter);
  if (Number.isSafeInteger(seconds) && seconds > 0) {
    return seconds === 1 ? "in 1 second" : `in ${seconds} seconds`;
  }
  return "after the broker's retry window";
}
