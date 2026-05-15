import type { Brand } from "./brand.ts";
import type { AgentId, CredentialHandleId, CredentialScope } from "./credential-handle.ts";
import type { ProviderKind, ReceiptId, RiskClass, WriteId } from "./receipt-types.ts";
import type { Sha256Hex } from "./sha256.ts";

// TODO(branch-12): export when codecs land.

/**
 * Branch-12 scoping stub for the WebAuthn-backed approval token.
 * See ../../../docs/design/branch-12-webauthn-cosign.md.
 */

export type ApprovalClaimId = Brand<string, "ApprovalClaimId">;
export type ApprovalTokenId = Brand<string, "ApprovalTokenId">;
export type TimestampMs = Brand<number, "TimestampMs">;

export type ApprovalClaimKind =
  | "cost_spike_acknowledgement"
  | "endpoint_allowlist_extension"
  | "credential_grant_to_agent"
  | "receipt_co_sign";

export interface SignedApprovalToken {
  readonly schemaVersion: 1;
  readonly tokenId: ApprovalTokenId;
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
  readonly notBefore: TimestampMs;
  readonly expiresAt: TimestampMs;
  readonly issuedTo: AgentId;
  readonly signature: WebAuthnAssertion;
}

export type ApprovalClaim =
  | CostSpikeAcknowledgementClaim
  | EndpointAllowlistExtensionClaim
  | CredentialGrantToAgentClaim
  | ReceiptCoSignClaim;

interface ApprovalClaimBase {
  readonly schemaVersion: 1;
  readonly claimId: ApprovalClaimId;
  readonly kind: ApprovalClaimKind;
}

export interface CostSpikeAcknowledgementClaim extends ApprovalClaimBase {
  readonly kind: "cost_spike_acknowledgement";
  readonly agentId: AgentId;
  readonly costCeilingId: string;
  readonly thresholdBps: number;
  readonly currentMicroUsd: number;
  readonly ceilingMicroUsd: number;
}

export interface EndpointAllowlistExtensionClaim extends ApprovalClaimBase {
  readonly kind: "endpoint_allowlist_extension";
  readonly agentId: AgentId;
  readonly providerKind: ProviderKind;
  readonly endpointOrigin: string;
  readonly reason: string;
}

export interface CredentialGrantToAgentClaim extends ApprovalClaimBase {
  readonly kind: "credential_grant_to_agent";
  readonly granteeAgentId: AgentId;
  readonly credentialHandleId: CredentialHandleId;
  readonly credentialScope: CredentialScope;
}

export interface ReceiptCoSignClaim extends ApprovalClaimBase {
  readonly kind: "receipt_co_sign";
  readonly receiptId: ReceiptId;
  readonly writeId?: WriteId | undefined;
  readonly frozenArgsHash: Sha256Hex;
  readonly riskClass: RiskClass;
}

export type ApprovalScope =
  | CostSpikeAcknowledgementScope
  | EndpointAllowlistExtensionScope
  | CredentialGrantToAgentScope
  | ReceiptCoSignScope;

interface ApprovalScopeBase {
  readonly mode: "single_use";
  readonly claimId: ApprovalClaimId;
  readonly claimKind: ApprovalClaimKind;
  readonly maxUses: 1;
}

export interface CostSpikeAcknowledgementScope extends ApprovalScopeBase {
  readonly claimKind: "cost_spike_acknowledgement";
  readonly agentId: AgentId;
  readonly costCeilingId: string;
}

export interface EndpointAllowlistExtensionScope extends ApprovalScopeBase {
  readonly claimKind: "endpoint_allowlist_extension";
  readonly agentId: AgentId;
  readonly providerKind: ProviderKind;
  readonly endpointOrigin: string;
}

export interface CredentialGrantToAgentScope extends ApprovalScopeBase {
  readonly claimKind: "credential_grant_to_agent";
  readonly granteeAgentId: AgentId;
  readonly credentialHandleId: CredentialHandleId;
}

export interface ReceiptCoSignScope extends ApprovalScopeBase {
  readonly claimKind: "receipt_co_sign";
  readonly receiptId: ReceiptId;
  readonly writeId?: WriteId | undefined;
  readonly frozenArgsHash: Sha256Hex;
}

export interface WebAuthnAssertion {
  readonly credentialId: string;
  readonly authenticatorData: string;
  readonly clientDataJson: string;
  readonly signature: string;
  readonly userHandle?: string | undefined;
}

export function signedApprovalTokenFromJson(_value: unknown): SignedApprovalToken {
  throw new Error("TODO(branch-12): implement SignedApprovalToken strict-known-keys codec");
}

export function signedApprovalTokenToJsonValue(
  _token: SignedApprovalToken,
): Record<string, unknown> {
  throw new Error("TODO(branch-12): implement SignedApprovalToken JSON serializer");
}
