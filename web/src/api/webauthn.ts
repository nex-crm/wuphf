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
  ApprovalScope,
  ApprovalScopeJsonValue,
  SignedApprovalTokenJsonValue,
} from "@wuphf/protocol";

import { post } from "./client";

export type WebAuthnCreationOptionsJson =
  PublicKeyCredentialCreationOptionsJSON;
export type WebAuthnRequestOptionsJson = PublicKeyCredentialRequestOptionsJSON;
export type WebAuthnAttestationResponseJson = RegistrationResponseJSON;
export type WebAuthnAssertionResponseJson = AuthenticationResponseJSON;

export interface WebAuthnRegistrationChallengeRequest {
  readonly role: string;
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
  readonly role: string;
}

export interface WebAuthnCosignChallengeRequest {
  readonly claim: ApprovalClaimJsonValue;
  readonly scope: ApprovalScopeJsonValue;
}

export interface WebAuthnCosignChallengeInput {
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
}

export interface WebAuthnCosignChallengeResponse {
  readonly challengeId: string;
  readonly requestOptions: WebAuthnRequestOptionsJson;
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
  response: WebAuthnCosignVerifyResponse,
): response is WebAuthnApprovalPendingResponse {
  return (
    "status" in response &&
    response.status === "approval_pending" &&
    Array.isArray(response.satisfiedRoles)
  );
}

export function toWebAuthnCosignChallengeRequest(
  input: WebAuthnCosignChallengeInput,
): WebAuthnCosignChallengeRequest {
  return {
    claim: approvalClaimJsonFromClaim(input.claim),
    scope: approvalScopeJsonFromScope(input.scope),
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

export function requestWebAuthnCosignChallenge(
  input: WebAuthnCosignChallengeInput,
): Promise<WebAuthnCosignChallengeResponse> {
  return post<WebAuthnCosignChallengeResponse>(
    "/webauthn/cosign/challenge",
    toWebAuthnCosignChallengeRequest(input),
  );
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

function approvalClaimJsonFromClaim(
  claim: ApprovalClaim,
): ApprovalClaimJsonValue {
  switch (claim.kind) {
    case "cost_spike_acknowledgement":
      return {
        schemaVersion: claim.schemaVersion,
        claimId: claim.claimId,
        kind: claim.kind,
        agentId: claim.agentId,
        costCeilingId: claim.costCeilingId,
        thresholdBps: claim.thresholdBps,
        currentMicroUsd: claim.currentMicroUsd,
        ceilingMicroUsd: claim.ceilingMicroUsd,
      };
    case "endpoint_allowlist_extension":
      return {
        schemaVersion: claim.schemaVersion,
        claimId: claim.claimId,
        kind: claim.kind,
        agentId: claim.agentId,
        providerKind: claim.providerKind,
        endpointOrigin: claim.endpointOrigin,
        reason: claim.reason,
      };
    case "credential_grant_to_agent":
      return {
        schemaVersion: claim.schemaVersion,
        claimId: claim.claimId,
        kind: claim.kind,
        granteeAgentId: claim.granteeAgentId,
        credentialHandleId: claim.credentialHandleId,
        credentialScope: claim.credentialScope,
      };
    case "receipt_co_sign":
      return omitUndefined({
        schemaVersion: claim.schemaVersion,
        claimId: claim.claimId,
        kind: claim.kind,
        receiptId: claim.receiptId,
        writeId: claim.writeId,
        frozenArgsHash: claim.frozenArgsHash,
        riskClass: claim.riskClass,
      });
  }
}

function approvalScopeJsonFromScope(
  scope: ApprovalScope,
): ApprovalScopeJsonValue {
  switch (scope.claimKind) {
    case "cost_spike_acknowledgement":
      return {
        mode: scope.mode,
        claimId: scope.claimId,
        claimKind: scope.claimKind,
        role: scope.role,
        maxUses: scope.maxUses,
        agentId: scope.agentId,
        costCeilingId: scope.costCeilingId,
      };
    case "endpoint_allowlist_extension":
      return {
        mode: scope.mode,
        claimId: scope.claimId,
        claimKind: scope.claimKind,
        role: scope.role,
        maxUses: scope.maxUses,
        agentId: scope.agentId,
        providerKind: scope.providerKind,
        endpointOrigin: scope.endpointOrigin,
      };
    case "credential_grant_to_agent":
      return {
        mode: scope.mode,
        claimId: scope.claimId,
        claimKind: scope.claimKind,
        role: scope.role,
        maxUses: scope.maxUses,
        granteeAgentId: scope.granteeAgentId,
        credentialHandleId: scope.credentialHandleId,
      };
    case "receipt_co_sign":
      return omitUndefined({
        mode: scope.mode,
        claimId: scope.claimId,
        claimKind: scope.claimKind,
        role: scope.role,
        maxUses: scope.maxUses,
        receiptId: scope.receiptId,
        writeId: scope.writeId,
        frozenArgsHash: scope.frozenArgsHash,
      });
  }
}

function omitUndefined<T extends Record<string, unknown>>(value: T): T {
  const entries = Object.entries(value).filter(
    (entry): entry is [string, Exclude<unknown, undefined>] =>
      entry[1] !== undefined,
  );
  return Object.fromEntries(entries) as T;
}
