import type {
  AuthenticationResponseJSON,
  PublicKeyCredentialCreationOptionsJSON,
  PublicKeyCredentialRequestOptionsJSON,
  RegistrationResponseJSON,
  WebAuthnCredential,
} from "@simplewebauthn/server";
import type {
  AgentId,
  ApprovalClaim,
  ApprovalClaimKind,
  ApprovalRole,
  ApprovalScope,
  ApprovalTokenId,
  JsonValue,
  Sha256Hex,
  TimestampMs,
} from "@wuphf/protocol";

export const WEBAUTHN_CHALLENGE_TTL_MS = 5 * 60 * 1000;
export const WEBAUTHN_RP_NAME = "WUPHF";
export const WEBAUTHN_RP_ID = "localhost";
export const WEBAUTHN_ALLOWED_ORIGINS = ["http://localhost:5173", "http://127.0.0.1:5173"] as const;
export const WEBAUTHN_TRUSTED_APPROVAL_ROLES = ["approver", "host"] as const;

export type WebAuthnChallengeType = "registration" | "cosign";
export type WebAuthnTokenOutcome = "approval_pending" | "approved";

export interface Clock {
  now(): number;
}

export const SYSTEM_CLOCK: Clock = Object.freeze({
  now: () => Date.now(),
});

export interface RegisteredWebAuthnCredential {
  readonly credentialId: string;
  readonly publicKey: ReturnType<Uint8Array["slice"]>;
  readonly signCount: number;
  readonly role: ApprovalRole;
  readonly agentId: AgentId;
  readonly createdAtMs: number;
}

export interface RegistrationChallengeRecord {
  readonly challengeId: string;
  readonly type: "registration";
  readonly challenge: string;
  readonly role: ApprovalRole;
  readonly issuedToAgentId: AgentId;
  readonly expiresAtMs: number;
  readonly consumedAtMs: number | null;
}

export interface CosignChallengeRecord {
  readonly challengeId: string;
  readonly type: "cosign";
  readonly challenge: string;
  readonly tokenId: ApprovalTokenId;
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
  readonly claimScopeHash: Sha256Hex;
  readonly approvalGroupHash: Sha256Hex;
  readonly issuedToAgentId: AgentId;
  readonly notBeforeMs: TimestampMs;
  readonly expiresAtMs: TimestampMs;
  readonly consumedAtMs: number | null;
}

export type WebAuthnChallengeRecord = RegistrationChallengeRecord | CosignChallengeRecord;

export interface ConsumedWebAuthnTokenRecord {
  readonly tokenId: ApprovalTokenId;
  readonly challengeId: string;
  readonly outcome: WebAuthnTokenOutcome;
  readonly responseJson: JsonValue;
  readonly role: ApprovalRole;
  readonly approvalGroupHash: Sha256Hex;
  readonly issuedToAgentId: AgentId;
  readonly expiresAtMs: number;
  readonly consumedAtMs: number;
}

export interface SaveRegistrationChallengeArgs {
  readonly challengeId: string;
  readonly challenge: string;
  readonly role: ApprovalRole;
  readonly issuedToAgentId: AgentId;
  readonly createdAtMs: number;
  readonly expiresAtMs: number;
}

export interface SaveCosignChallengeArgs {
  readonly challengeId: string;
  readonly challenge: string;
  readonly tokenId: ApprovalTokenId;
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
  readonly claimScopeHash: Sha256Hex;
  readonly approvalGroupHash: Sha256Hex;
  readonly issuedToAgentId: AgentId;
  readonly notBeforeMs: TimestampMs;
  readonly expiresAtMs: TimestampMs;
  readonly createdAtMs: number;
}

export interface SaveCredentialArgs {
  readonly challengeId: string;
  readonly credential: RegisteredWebAuthnCredential;
  readonly consumedAtMs: number;
}

export interface ConsumeCosignChallengeArgs {
  readonly challengeId: string;
  readonly tokenId: ApprovalTokenId;
  readonly credentialId: string;
  readonly newSignCount: number;
  readonly outcome: WebAuthnTokenOutcome;
  readonly responseJson: JsonValue;
  readonly role: ApprovalRole;
  readonly approvalGroupHash: Sha256Hex;
  readonly issuedToAgentId: AgentId;
  readonly expiresAtMs: number;
  readonly consumedAtMs: number;
}

export interface WebAuthnStore {
  saveRegistrationChallenge(args: SaveRegistrationChallengeArgs): Promise<void>;
  saveCosignChallenge(args: SaveCosignChallengeArgs): Promise<void>;
  getChallenge(challengeId: string): Promise<WebAuthnChallengeRecord | null>;
  listCredentialsForAgent(agentId: AgentId): Promise<readonly RegisteredWebAuthnCredential[]>;
  listCredentialsForAgentRole(args: {
    readonly agentId: AgentId;
    readonly role: ApprovalRole;
  }): Promise<readonly RegisteredWebAuthnCredential[]>;
  getCredential(credentialId: string): Promise<RegisteredWebAuthnCredential | null>;
  saveCredential(args: SaveCredentialArgs): Promise<void>;
  getConsumedToken(tokenId: ApprovalTokenId): Promise<ConsumedWebAuthnTokenRecord | null>;
  listSatisfiedRoles(args: {
    readonly approvalGroupHash: Sha256Hex;
    readonly issuedToAgentId: AgentId;
    readonly nowMs: number;
  }): Promise<readonly ApprovalRole[]>;
  consumeCosignChallenge(
    args: ConsumeCosignChallengeArgs,
  ): Promise<ConsumedWebAuthnTokenRecord | null>;
}

export interface WebAuthnCeremony {
  generateRegistrationOptions(args: {
    readonly rpName: string;
    readonly rpId: string;
    readonly agentId: AgentId;
    readonly role: ApprovalRole;
    readonly challenge: string;
    readonly excludeCredentialIds: readonly string[];
  }): Promise<PublicKeyCredentialCreationOptionsJSON>;
  verifyRegistration(args: {
    readonly response: RegistrationResponseJSON;
    readonly expectedChallenge: string;
    readonly expectedOrigins: readonly string[];
    readonly expectedRpId: string;
  }): Promise<RegisteredWebAuthnCredentialVerification | null>;
  generateAuthenticationOptions(args: {
    readonly rpId: string;
    readonly challenge: string;
    readonly allowCredentialIds: readonly string[];
  }): Promise<PublicKeyCredentialRequestOptionsJSON>;
  verifyAuthentication(args: {
    readonly response: AuthenticationResponseJSON;
    readonly expectedChallenge: string;
    readonly expectedOrigins: readonly string[];
    readonly expectedRpId: string;
    readonly credential: WebAuthnCredential;
  }): Promise<WebAuthnAuthenticationVerification | null>;
}

export interface RegisteredWebAuthnCredentialVerification {
  readonly credentialId: string;
  readonly publicKey: ReturnType<Uint8Array["slice"]>;
  readonly signCount: number;
}

export interface WebAuthnAuthenticationVerification {
  readonly credentialId: string;
  readonly newSignCount: number;
  readonly userVerified: boolean;
}

export interface WebAuthnRouteDeps {
  readonly store: WebAuthnStore;
  readonly tokenAgentIds: ReadonlyMap<import("@wuphf/protocol").ApiToken, AgentId>;
  readonly ceremony: WebAuthnCeremony;
  readonly clock: Clock;
  readonly rpName: string;
  readonly rpId: string;
  readonly allowedOrigins: readonly string[];
  readonly challengeTtlMs: number;
  readonly trustedRoles: readonly ApprovalRole[];
  readonly defaultThreshold: number;
  readonly receiptCoSignThreshold: number;
  readonly logger: import("../types.ts").BrokerLogger;
}

export interface WebAuthnPolicyConfig {
  readonly rpName?: string;
  readonly rpId?: string;
  readonly allowedOrigins?: readonly string[];
  readonly challengeTtlMs?: number;
  readonly trustedRoles?: readonly ApprovalRole[];
  readonly defaultThreshold?: number;
  readonly receiptCoSignThreshold?: number;
  readonly ceremony?: WebAuthnCeremony;
}

export function thresholdForClaimKind(
  kind: ApprovalClaimKind,
  deps: Pick<WebAuthnRouteDeps, "defaultThreshold" | "receiptCoSignThreshold">,
): number {
  return kind === "receipt_co_sign" ? deps.receiptCoSignThreshold : deps.defaultThreshold;
}
