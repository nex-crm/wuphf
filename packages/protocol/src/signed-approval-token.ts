import type { Brand } from "./brand.ts";
import {
  MAX_BUDGET_LIMIT_MICRO_USD,
  MAX_BUDGET_THRESHOLD_BPS,
  validateApprovalClaimCanonicalJsonBudget,
  validateApprovalClaimIdBudget,
  validateApprovalCostCeilingIdBudget,
  validateApprovalEndpointOriginBudget,
  validateApprovalIdentifierBudget,
  validateApprovalReasonBudget,
  validateApprovalScopeCanonicalJsonBudget,
  validateApprovalTokenIdBudget,
  validateApprovalTokenLifetimeValues,
  validateWebAuthnAssertionBudget,
  validateWebAuthnAssertionFieldBudget,
} from "./budgets.ts";
import { canonicalJSON } from "./canonical-json.ts";
import {
  type AgentId,
  asAgentId,
  asCredentialHandleId,
  asCredentialScope,
  type CredentialHandleId,
  type CredentialScope,
} from "./credential-handle.ts";
import { APPROVAL_ROLE_VALUES, RISK_CLASS_VALUES } from "./receipt-literals.ts";
import {
  type ApprovalRole,
  asProviderKind,
  asReceiptId,
  asWriteId,
  type ProviderKind,
  type ReceiptId,
  type RiskClass,
  type WriteId,
} from "./receipt-types.ts";
import { assertKnownKeys, hasOwn, omitUndefined, pointer, requireRecord } from "./receipt-utils.ts";
import { SanitizedString } from "./sanitized-string.ts";
import { asSha256Hex, type Sha256Hex } from "./sha256.ts";

export const APPROVAL_TOKEN_SCHEMA_VERSION = 1;

export const APPROVAL_CLAIM_KIND_VALUES = [
  "cost_spike_acknowledgement",
  "endpoint_allowlist_extension",
  "credential_grant_to_agent",
  "receipt_co_sign",
] as const;

export type ApprovalClaimKind = (typeof APPROVAL_CLAIM_KIND_VALUES)[number];
export type ApprovalClaimId = Brand<string, "ApprovalClaimId">;
export type ApprovalTokenId = Brand<string, "ApprovalTokenId">;
export type TimestampMs = Brand<number, "TimestampMs">;

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
  readonly role: ApprovalRole;
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

export type SignedApprovalTokenJsonValue = ReturnType<typeof signedApprovalTokenToJsonValue>;
export type ApprovalClaimJsonValue = ReturnType<typeof approvalClaimToJsonValue>;
export type ApprovalScopeJsonValue = ReturnType<typeof approvalScopeToJsonValue>;
export type WebAuthnAssertionJsonValue = ReturnType<typeof webAuthnAssertionToJsonValue>;

const ULID_RE = /^[0-9A-HJKMNP-TV-Z]{26}$/;
const CLAIM_ID_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const COST_CEILING_ID_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const BASE64URL_RE = /^[A-Za-z0-9_-]+$/;

const APPROVAL_CLAIM_KIND_SET: ReadonlySet<string> = new Set(APPROVAL_CLAIM_KIND_VALUES);
const APPROVAL_ROLE_SET: ReadonlySet<string> = new Set(APPROVAL_ROLE_VALUES);
const RISK_CLASS_SET: ReadonlySet<string> = new Set(RISK_CLASS_VALUES);

const SIGNED_APPROVAL_TOKEN_KEYS_TUPLE = [
  "schemaVersion",
  "tokenId",
  "claim",
  "scope",
  "notBefore",
  "expiresAt",
  "issuedTo",
  "signature",
] as const satisfies readonly (keyof SignedApprovalToken)[];
export const SIGNED_APPROVAL_TOKEN_KEYS: ReadonlySet<string> = new Set(
  SIGNED_APPROVAL_TOKEN_KEYS_TUPLE,
);

const COST_SPIKE_ACKNOWLEDGEMENT_CLAIM_KEYS_TUPLE = [
  "schemaVersion",
  "claimId",
  "kind",
  "agentId",
  "costCeilingId",
  "thresholdBps",
  "currentMicroUsd",
  "ceilingMicroUsd",
] as const satisfies readonly (keyof CostSpikeAcknowledgementClaim)[];
export const COST_SPIKE_ACKNOWLEDGEMENT_CLAIM_KEYS: ReadonlySet<string> = new Set(
  COST_SPIKE_ACKNOWLEDGEMENT_CLAIM_KEYS_TUPLE,
);

const ENDPOINT_ALLOWLIST_EXTENSION_CLAIM_KEYS_TUPLE = [
  "schemaVersion",
  "claimId",
  "kind",
  "agentId",
  "providerKind",
  "endpointOrigin",
  "reason",
] as const satisfies readonly (keyof EndpointAllowlistExtensionClaim)[];
export const ENDPOINT_ALLOWLIST_EXTENSION_CLAIM_KEYS: ReadonlySet<string> = new Set(
  ENDPOINT_ALLOWLIST_EXTENSION_CLAIM_KEYS_TUPLE,
);

const CREDENTIAL_GRANT_TO_AGENT_CLAIM_KEYS_TUPLE = [
  "schemaVersion",
  "claimId",
  "kind",
  "granteeAgentId",
  "credentialHandleId",
  "credentialScope",
] as const satisfies readonly (keyof CredentialGrantToAgentClaim)[];
export const CREDENTIAL_GRANT_TO_AGENT_CLAIM_KEYS: ReadonlySet<string> = new Set(
  CREDENTIAL_GRANT_TO_AGENT_CLAIM_KEYS_TUPLE,
);

const RECEIPT_CO_SIGN_CLAIM_KEYS_TUPLE = [
  "schemaVersion",
  "claimId",
  "kind",
  "receiptId",
  "writeId",
  "frozenArgsHash",
  "riskClass",
] as const satisfies readonly (keyof ReceiptCoSignClaim)[];
export const RECEIPT_CO_SIGN_CLAIM_KEYS: ReadonlySet<string> = new Set(
  RECEIPT_CO_SIGN_CLAIM_KEYS_TUPLE,
);

const COST_SPIKE_ACKNOWLEDGEMENT_SCOPE_KEYS_TUPLE = [
  "mode",
  "claimId",
  "claimKind",
  "role",
  "maxUses",
  "agentId",
  "costCeilingId",
] as const satisfies readonly (keyof CostSpikeAcknowledgementScope)[];
export const COST_SPIKE_ACKNOWLEDGEMENT_SCOPE_KEYS: ReadonlySet<string> = new Set(
  COST_SPIKE_ACKNOWLEDGEMENT_SCOPE_KEYS_TUPLE,
);

const ENDPOINT_ALLOWLIST_EXTENSION_SCOPE_KEYS_TUPLE = [
  "mode",
  "claimId",
  "claimKind",
  "role",
  "maxUses",
  "agentId",
  "providerKind",
  "endpointOrigin",
] as const satisfies readonly (keyof EndpointAllowlistExtensionScope)[];
export const ENDPOINT_ALLOWLIST_EXTENSION_SCOPE_KEYS: ReadonlySet<string> = new Set(
  ENDPOINT_ALLOWLIST_EXTENSION_SCOPE_KEYS_TUPLE,
);

const CREDENTIAL_GRANT_TO_AGENT_SCOPE_KEYS_TUPLE = [
  "mode",
  "claimId",
  "claimKind",
  "role",
  "maxUses",
  "granteeAgentId",
  "credentialHandleId",
] as const satisfies readonly (keyof CredentialGrantToAgentScope)[];
export const CREDENTIAL_GRANT_TO_AGENT_SCOPE_KEYS: ReadonlySet<string> = new Set(
  CREDENTIAL_GRANT_TO_AGENT_SCOPE_KEYS_TUPLE,
);

const RECEIPT_CO_SIGN_SCOPE_KEYS_TUPLE = [
  "mode",
  "claimId",
  "claimKind",
  "role",
  "maxUses",
  "receiptId",
  "writeId",
  "frozenArgsHash",
] as const satisfies readonly (keyof ReceiptCoSignScope)[];
export const RECEIPT_CO_SIGN_SCOPE_KEYS: ReadonlySet<string> = new Set(
  RECEIPT_CO_SIGN_SCOPE_KEYS_TUPLE,
);

const WEBAUTHN_ASSERTION_KEYS_TUPLE = [
  "credentialId",
  "authenticatorData",
  "clientDataJson",
  "signature",
  "userHandle",
] as const satisfies readonly (keyof WebAuthnAssertion)[];
export const WEBAUTHN_ASSERTION_KEYS: ReadonlySet<string> = new Set(WEBAUTHN_ASSERTION_KEYS_TUPLE);

export function asApprovalTokenId(value: string): ApprovalTokenId {
  assertBudget(validateApprovalTokenIdBudget(value), "ApprovalTokenId");
  if (!ULID_RE.test(value)) {
    throw new Error("not an ApprovalTokenId");
  }
  return value as ApprovalTokenId;
}

export function isApprovalTokenId(value: unknown): value is ApprovalTokenId {
  return typeof value === "string" && ULID_RE.test(value);
}

export function asApprovalClaimId(value: string): ApprovalClaimId {
  assertBudget(validateApprovalClaimIdBudget(value), "ApprovalClaimId");
  if (!CLAIM_ID_RE.test(value)) {
    throw new Error("not an ApprovalClaimId");
  }
  return value as ApprovalClaimId;
}

export function isApprovalClaimId(value: unknown): value is ApprovalClaimId {
  return typeof value === "string" && CLAIM_ID_RE.test(value);
}

export function asTimestampMs(value: number): TimestampMs {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new Error("not a TimestampMs");
  }
  return value as TimestampMs;
}

export function isTimestampMs(value: unknown): value is TimestampMs {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

export function signedApprovalTokenFromJson(
  value: unknown,
  path = "signedApprovalToken",
): SignedApprovalToken {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, SIGNED_APPROVAL_TOKEN_KEYS);
  const schemaVersion = requiredSchemaVersion(record, "schemaVersion", path);
  const tokenId = approvalTokenIdFromJson(requiredString(record, "tokenId", path), path);
  const claim = approvalClaimFromJson(requiredField(record, "claim", path), pointer(path, "claim"));
  const scope = approvalScopeFromJson(requiredField(record, "scope", path), pointer(path, "scope"));
  assertClaimScopeBinding(claim, scope, path);
  const notBefore = timestampMsFromJson(
    requiredNumber(record, "notBefore", path),
    pointer(path, "notBefore"),
  );
  const expiresAt = timestampMsFromJson(
    requiredNumber(record, "expiresAt", path),
    pointer(path, "expiresAt"),
  );
  if (expiresAt <= notBefore) {
    throw new Error(`${pointer(path, "expiresAt")}: must be strictly greater than notBefore`);
  }
  assertBudget(
    validateApprovalTokenLifetimeValues(notBefore, expiresAt),
    pointer(path, "expiresAt"),
  );
  const issuedTo = agentIdFromJson(requiredString(record, "issuedTo", path), path, "issuedTo");
  const signature = webAuthnAssertionFromJson(
    requiredField(record, "signature", path),
    pointer(path, "signature"),
  );

  return {
    schemaVersion,
    tokenId,
    claim,
    scope,
    notBefore,
    expiresAt,
    issuedTo,
    signature,
  };
}

export function signedApprovalTokenToJsonValue(token: SignedApprovalToken): {
  readonly schemaVersion: 1;
  readonly tokenId: string;
  readonly claim: ApprovalClaimJsonValue;
  readonly scope: ApprovalScopeJsonValue;
  readonly notBefore: number;
  readonly expiresAt: number;
  readonly issuedTo: string;
  readonly signature: WebAuthnAssertionJsonValue;
} {
  return {
    schemaVersion: token.schemaVersion,
    tokenId: token.tokenId,
    claim: approvalClaimToJsonValue(token.claim),
    scope: approvalScopeToJsonValue(token.scope),
    notBefore: token.notBefore,
    expiresAt: token.expiresAt,
    issuedTo: token.issuedTo,
    signature: webAuthnAssertionToJsonValue(token.signature),
  };
}

export function approvalClaimFromJson(value: unknown, path = "approvalClaim"): ApprovalClaim {
  const record = requireRecord(value, path);
  const kind = requiredApprovalClaimKind(record, "kind", path);
  const claim =
    kind === "cost_spike_acknowledgement"
      ? costSpikeAcknowledgementClaimFromRecord(record, path)
      : kind === "endpoint_allowlist_extension"
        ? endpointAllowlistExtensionClaimFromRecord(record, path)
        : kind === "credential_grant_to_agent"
          ? credentialGrantToAgentClaimFromRecord(record, path)
          : receiptCoSignClaimFromRecord(record, path);
  assertBudget(
    validateApprovalClaimCanonicalJsonBudget(canonicalJSON(approvalClaimToJsonValue(claim))),
    path,
  );
  return claim;
}

export function approvalClaimToJsonValue(claim: ApprovalClaim):
  | {
      readonly schemaVersion: 1;
      readonly claimId: string;
      readonly kind: "cost_spike_acknowledgement";
      readonly agentId: string;
      readonly costCeilingId: string;
      readonly thresholdBps: number;
      readonly currentMicroUsd: number;
      readonly ceilingMicroUsd: number;
    }
  | {
      readonly schemaVersion: 1;
      readonly claimId: string;
      readonly kind: "endpoint_allowlist_extension";
      readonly agentId: string;
      readonly providerKind: string;
      readonly endpointOrigin: string;
      readonly reason: string;
    }
  | {
      readonly schemaVersion: 1;
      readonly claimId: string;
      readonly kind: "credential_grant_to_agent";
      readonly granteeAgentId: string;
      readonly credentialHandleId: string;
      readonly credentialScope: string;
    }
  | {
      readonly schemaVersion: 1;
      readonly claimId: string;
      readonly kind: "receipt_co_sign";
      readonly receiptId: string;
      readonly writeId?: string | undefined;
      readonly frozenArgsHash: string;
      readonly riskClass: RiskClass;
    } {
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

export function approvalScopeFromJson(value: unknown, path = "approvalScope"): ApprovalScope {
  const record = requireRecord(value, path);
  const claimKind = requiredApprovalClaimKind(record, "claimKind", path);
  const scope =
    claimKind === "cost_spike_acknowledgement"
      ? costSpikeAcknowledgementScopeFromRecord(record, path)
      : claimKind === "endpoint_allowlist_extension"
        ? endpointAllowlistExtensionScopeFromRecord(record, path)
        : claimKind === "credential_grant_to_agent"
          ? credentialGrantToAgentScopeFromRecord(record, path)
          : receiptCoSignScopeFromRecord(record, path);
  assertBudget(
    validateApprovalScopeCanonicalJsonBudget(canonicalJSON(approvalScopeToJsonValue(scope))),
    path,
  );
  return scope;
}

export function approvalScopeToJsonValue(scope: ApprovalScope):
  | {
      readonly mode: "single_use";
      readonly claimId: string;
      readonly claimKind: "cost_spike_acknowledgement";
      readonly role: ApprovalRole;
      readonly maxUses: 1;
      readonly agentId: string;
      readonly costCeilingId: string;
    }
  | {
      readonly mode: "single_use";
      readonly claimId: string;
      readonly claimKind: "endpoint_allowlist_extension";
      readonly role: ApprovalRole;
      readonly maxUses: 1;
      readonly agentId: string;
      readonly providerKind: string;
      readonly endpointOrigin: string;
    }
  | {
      readonly mode: "single_use";
      readonly claimId: string;
      readonly claimKind: "credential_grant_to_agent";
      readonly role: ApprovalRole;
      readonly maxUses: 1;
      readonly granteeAgentId: string;
      readonly credentialHandleId: string;
    }
  | {
      readonly mode: "single_use";
      readonly claimId: string;
      readonly claimKind: "receipt_co_sign";
      readonly role: ApprovalRole;
      readonly maxUses: 1;
      readonly receiptId: string;
      readonly writeId?: string | undefined;
      readonly frozenArgsHash: string;
    } {
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

export function webAuthnAssertionFromJson(
  value: unknown,
  path = "webAuthnAssertion",
): WebAuthnAssertion {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, WEBAUTHN_ASSERTION_KEYS);
  const userHandle = optionalBase64UrlString(record, "userHandle", path);
  const assertion = {
    credentialId: requiredBase64UrlString(record, "credentialId", path),
    authenticatorData: requiredBase64UrlString(record, "authenticatorData", path),
    clientDataJson: requiredBase64UrlString(record, "clientDataJson", path),
    signature: requiredBase64UrlString(record, "signature", path),
    ...(userHandle === undefined ? {} : { userHandle }),
  };
  assertBudget(
    validateWebAuthnAssertionBudget(canonicalJSON(webAuthnAssertionToJsonValue(assertion))),
    path,
  );
  return assertion;
}

export function webAuthnAssertionToJsonValue(assertion: WebAuthnAssertion): {
  readonly credentialId: string;
  readonly authenticatorData: string;
  readonly clientDataJson: string;
  readonly signature: string;
  readonly userHandle?: string | undefined;
} {
  return omitUndefined({
    credentialId: assertion.credentialId,
    authenticatorData: assertion.authenticatorData,
    clientDataJson: assertion.clientDataJson,
    signature: assertion.signature,
    userHandle: assertion.userHandle,
  });
}

export function isReceiptCoSignClaim(claim: ApprovalClaim): claim is ReceiptCoSignClaim {
  return claim.kind === "receipt_co_sign";
}

export function isReceiptCoSignScope(scope: ApprovalScope): scope is ReceiptCoSignScope {
  return scope.claimKind === "receipt_co_sign";
}

function costSpikeAcknowledgementClaimFromRecord(
  record: Readonly<Record<string, unknown>>,
  path: string,
): CostSpikeAcknowledgementClaim {
  assertKnownKeys(record, path, COST_SPIKE_ACKNOWLEDGEMENT_CLAIM_KEYS);
  return {
    schemaVersion: requiredSchemaVersion(record, "schemaVersion", path),
    claimId: approvalClaimIdFromJson(requiredString(record, "claimId", path), path),
    kind: "cost_spike_acknowledgement",
    agentId: agentIdFromJson(requiredString(record, "agentId", path), path, "agentId"),
    costCeilingId: costCeilingIdFromJson(requiredString(record, "costCeilingId", path), path),
    thresholdBps: thresholdBpsFromJson(requiredNumber(record, "thresholdBps", path), path),
    currentMicroUsd: microUsdFromJson(
      requiredNumber(record, "currentMicroUsd", path),
      pointer(path, "currentMicroUsd"),
    ),
    ceilingMicroUsd: microUsdFromJson(
      requiredNumber(record, "ceilingMicroUsd", path),
      pointer(path, "ceilingMicroUsd"),
    ),
  };
}

function endpointAllowlistExtensionClaimFromRecord(
  record: Readonly<Record<string, unknown>>,
  path: string,
): EndpointAllowlistExtensionClaim {
  assertKnownKeys(record, path, ENDPOINT_ALLOWLIST_EXTENSION_CLAIM_KEYS);
  return {
    schemaVersion: requiredSchemaVersion(record, "schemaVersion", path),
    claimId: approvalClaimIdFromJson(requiredString(record, "claimId", path), path),
    kind: "endpoint_allowlist_extension",
    agentId: agentIdFromJson(requiredString(record, "agentId", path), path, "agentId"),
    providerKind: providerKindFromJson(requiredString(record, "providerKind", path), path),
    endpointOrigin: endpointOriginFromJson(requiredString(record, "endpointOrigin", path), path),
    reason: reasonFromJson(requiredString(record, "reason", path), path),
  };
}

function credentialGrantToAgentClaimFromRecord(
  record: Readonly<Record<string, unknown>>,
  path: string,
): CredentialGrantToAgentClaim {
  assertKnownKeys(record, path, CREDENTIAL_GRANT_TO_AGENT_CLAIM_KEYS);
  return {
    schemaVersion: requiredSchemaVersion(record, "schemaVersion", path),
    claimId: approvalClaimIdFromJson(requiredString(record, "claimId", path), path),
    kind: "credential_grant_to_agent",
    granteeAgentId: agentIdFromJson(
      requiredString(record, "granteeAgentId", path),
      path,
      "granteeAgentId",
    ),
    credentialHandleId: credentialHandleIdFromJson(
      requiredString(record, "credentialHandleId", path),
      path,
    ),
    credentialScope: credentialScopeFromJson(requiredString(record, "credentialScope", path), path),
  };
}

function receiptCoSignClaimFromRecord(
  record: Readonly<Record<string, unknown>>,
  path: string,
): ReceiptCoSignClaim {
  assertKnownKeys(record, path, RECEIPT_CO_SIGN_CLAIM_KEYS);
  const writeId = optionalString(record, "writeId", path);
  return {
    schemaVersion: requiredSchemaVersion(record, "schemaVersion", path),
    claimId: approvalClaimIdFromJson(requiredString(record, "claimId", path), path),
    kind: "receipt_co_sign",
    receiptId: receiptIdFromJson(requiredString(record, "receiptId", path), path),
    ...(writeId === undefined ? {} : { writeId: writeIdFromJson(writeId, path) }),
    frozenArgsHash: sha256HexFromJson(requiredString(record, "frozenArgsHash", path), path),
    riskClass: riskClassFromJson(requiredString(record, "riskClass", path), path),
  };
}

function costSpikeAcknowledgementScopeFromRecord(
  record: Readonly<Record<string, unknown>>,
  path: string,
): CostSpikeAcknowledgementScope {
  assertKnownKeys(record, path, COST_SPIKE_ACKNOWLEDGEMENT_SCOPE_KEYS);
  return {
    ...approvalScopeBaseFromRecord(record, path),
    claimKind: "cost_spike_acknowledgement",
    agentId: agentIdFromJson(requiredString(record, "agentId", path), path, "agentId"),
    costCeilingId: costCeilingIdFromJson(requiredString(record, "costCeilingId", path), path),
  };
}

function endpointAllowlistExtensionScopeFromRecord(
  record: Readonly<Record<string, unknown>>,
  path: string,
): EndpointAllowlistExtensionScope {
  assertKnownKeys(record, path, ENDPOINT_ALLOWLIST_EXTENSION_SCOPE_KEYS);
  return {
    ...approvalScopeBaseFromRecord(record, path),
    claimKind: "endpoint_allowlist_extension",
    agentId: agentIdFromJson(requiredString(record, "agentId", path), path, "agentId"),
    providerKind: providerKindFromJson(requiredString(record, "providerKind", path), path),
    endpointOrigin: endpointOriginFromJson(requiredString(record, "endpointOrigin", path), path),
  };
}

function credentialGrantToAgentScopeFromRecord(
  record: Readonly<Record<string, unknown>>,
  path: string,
): CredentialGrantToAgentScope {
  assertKnownKeys(record, path, CREDENTIAL_GRANT_TO_AGENT_SCOPE_KEYS);
  return {
    ...approvalScopeBaseFromRecord(record, path),
    claimKind: "credential_grant_to_agent",
    granteeAgentId: agentIdFromJson(
      requiredString(record, "granteeAgentId", path),
      path,
      "granteeAgentId",
    ),
    credentialHandleId: credentialHandleIdFromJson(
      requiredString(record, "credentialHandleId", path),
      path,
    ),
  };
}

function receiptCoSignScopeFromRecord(
  record: Readonly<Record<string, unknown>>,
  path: string,
): ReceiptCoSignScope {
  assertKnownKeys(record, path, RECEIPT_CO_SIGN_SCOPE_KEYS);
  const writeId = optionalString(record, "writeId", path);
  return {
    ...approvalScopeBaseFromRecord(record, path),
    claimKind: "receipt_co_sign",
    receiptId: receiptIdFromJson(requiredString(record, "receiptId", path), path),
    ...(writeId === undefined ? {} : { writeId: writeIdFromJson(writeId, path) }),
    frozenArgsHash: sha256HexFromJson(requiredString(record, "frozenArgsHash", path), path),
  };
}

function approvalScopeBaseFromRecord(
  record: Readonly<Record<string, unknown>>,
  path: string,
): ApprovalScopeBase {
  const mode = requiredString(record, "mode", path);
  if (mode !== "single_use") {
    throw new Error(`${pointer(path, "mode")}: must be single_use`);
  }
  const maxUses = requiredNumber(record, "maxUses", path);
  if (maxUses !== 1) {
    throw new Error(`${pointer(path, "maxUses")}: must be 1`);
  }
  return {
    mode,
    claimId: approvalClaimIdFromJson(requiredString(record, "claimId", path), path),
    claimKind: requiredApprovalClaimKind(record, "claimKind", path),
    role: approvalRoleFromJson(requiredString(record, "role", path), path),
    maxUses,
  };
}

function assertClaimScopeBinding(claim: ApprovalClaim, scope: ApprovalScope, path: string): void {
  const scopePath = pointer(path, "scope");
  if (claim.claimId !== scope.claimId) {
    throw new Error(`${pointer(scopePath, "claimId")}: must match claim.claimId`);
  }
  if (claim.kind !== scope.claimKind) {
    throw new Error(`${pointer(scopePath, "claimKind")}: must match claim.kind`);
  }
  switch (claim.kind) {
    case "cost_spike_acknowledgement":
      if (scope.claimKind !== claim.kind) return;
      assertSame(scope.agentId, claim.agentId, scopePath, "agentId");
      assertSame(scope.costCeilingId, claim.costCeilingId, scopePath, "costCeilingId");
      return;
    case "endpoint_allowlist_extension":
      if (scope.claimKind !== claim.kind) return;
      assertSame(scope.agentId, claim.agentId, scopePath, "agentId");
      assertSame(scope.providerKind, claim.providerKind, scopePath, "providerKind");
      assertSame(scope.endpointOrigin, claim.endpointOrigin, scopePath, "endpointOrigin");
      return;
    case "credential_grant_to_agent":
      if (scope.claimKind !== claim.kind) return;
      assertSame(scope.granteeAgentId, claim.granteeAgentId, scopePath, "granteeAgentId");
      assertSame(
        scope.credentialHandleId,
        claim.credentialHandleId,
        scopePath,
        "credentialHandleId",
      );
      return;
    case "receipt_co_sign":
      if (scope.claimKind !== claim.kind) return;
      assertSame(scope.receiptId, claim.receiptId, scopePath, "receiptId");
      assertSame(scope.writeId, claim.writeId, scopePath, "writeId");
      assertSame(scope.frozenArgsHash, claim.frozenArgsHash, scopePath, "frozenArgsHash");
      return;
  }
}

function assertSame(left: unknown, right: unknown, path: string, field: string): void {
  if (left !== right) {
    throw new Error(`${pointer(path, field)}: must match claim.${field}`);
  }
}

function requiredSchemaVersion(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): 1 {
  const value = requiredNumber(record, key, basePath);
  if (value !== APPROVAL_TOKEN_SCHEMA_VERSION) {
    throw new Error(`${pointer(basePath, key)}: must be 1`);
  }
  return APPROVAL_TOKEN_SCHEMA_VERSION;
}

function requiredApprovalClaimKind(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): ApprovalClaimKind {
  const value = requiredString(record, key, basePath);
  assertBudget(
    validateApprovalIdentifierBudget(value, `${pointer(basePath, key)} bytes`),
    basePath,
  );
  if (!APPROVAL_CLAIM_KIND_SET.has(value)) {
    throw new Error(`${pointer(basePath, key)}: must be a valid approval claim kind`);
  }
  return value as ApprovalClaimKind;
}

function approvalRoleFromJson(value: string, path: string): ApprovalRole {
  assertBudget(validateApprovalIdentifierBudget(value, `${pointer(path, "role")} bytes`), path);
  if (!APPROVAL_ROLE_SET.has(value)) {
    throw new Error(`${pointer(path, "role")}: must be a valid approval role`);
  }
  return value as ApprovalRole;
}

function riskClassFromJson(value: string, path: string): RiskClass {
  assertBudget(
    validateApprovalIdentifierBudget(value, `${pointer(path, "riskClass")} bytes`),
    path,
  );
  if (!RISK_CLASS_SET.has(value)) {
    throw new Error(`${pointer(path, "riskClass")}: must be a valid risk class`);
  }
  return value as RiskClass;
}

function approvalTokenIdFromJson(value: string, path: string): ApprovalTokenId {
  try {
    return asApprovalTokenId(value);
  } catch (err) {
    throw new Error(`${pointer(path, "tokenId")}: ${messageOf(err)}`);
  }
}

function approvalClaimIdFromJson(value: string, path: string): ApprovalClaimId {
  try {
    return asApprovalClaimId(value);
  } catch (err) {
    throw new Error(`${pointer(path, "claimId")}: ${messageOf(err)}`);
  }
}

function timestampMsFromJson(value: number, path: string): TimestampMs {
  try {
    return asTimestampMs(value);
  } catch (err) {
    throw new Error(`${path}: ${messageOf(err)}`);
  }
}

function agentIdFromJson(value: string, path: string, field: string): AgentId {
  assertBudget(validateApprovalIdentifierBudget(value, `${pointer(path, field)} bytes`), path);
  try {
    return asAgentId(value);
  } catch (err) {
    throw new Error(`${pointer(path, field)}: ${messageOf(err)}`);
  }
}

function credentialHandleIdFromJson(value: string, path: string): CredentialHandleId {
  assertBudget(
    validateApprovalIdentifierBudget(value, `${pointer(path, "credentialHandleId")} bytes`),
    path,
  );
  try {
    return asCredentialHandleId(value);
  } catch (err) {
    throw new Error(`${pointer(path, "credentialHandleId")}: ${messageOf(err)}`);
  }
}

function credentialScopeFromJson(value: string, path: string): CredentialScope {
  assertBudget(
    validateApprovalIdentifierBudget(value, `${pointer(path, "credentialScope")} bytes`),
    path,
  );
  try {
    return asCredentialScope(value);
  } catch (err) {
    throw new Error(`${pointer(path, "credentialScope")}: ${messageOf(err)}`);
  }
}

function providerKindFromJson(value: string, path: string): ProviderKind {
  assertBudget(
    validateApprovalIdentifierBudget(value, `${pointer(path, "providerKind")} bytes`),
    path,
  );
  try {
    return asProviderKind(value);
  } catch (err) {
    throw new Error(`${pointer(path, "providerKind")}: ${messageOf(err)}`);
  }
}

function receiptIdFromJson(value: string, path: string): ReceiptId {
  assertBudget(
    validateApprovalIdentifierBudget(value, `${pointer(path, "receiptId")} bytes`),
    path,
  );
  try {
    return asReceiptId(value);
  } catch (err) {
    throw new Error(`${pointer(path, "receiptId")}: ${messageOf(err)}`);
  }
}

function writeIdFromJson(value: string, path: string): WriteId {
  assertBudget(validateApprovalIdentifierBudget(value, `${pointer(path, "writeId")} bytes`), path);
  try {
    return asWriteId(value);
  } catch (err) {
    throw new Error(`${pointer(path, "writeId")}: ${messageOf(err)}`);
  }
}

function sha256HexFromJson(value: string, path: string): Sha256Hex {
  assertBudget(
    validateApprovalIdentifierBudget(value, `${pointer(path, "frozenArgsHash")} bytes`),
    path,
  );
  try {
    return asSha256Hex(value);
  } catch (err) {
    throw new Error(`${pointer(path, "frozenArgsHash")}: ${messageOf(err)}`);
  }
}

function costCeilingIdFromJson(value: string, path: string): string {
  assertBudget(validateApprovalCostCeilingIdBudget(value), pointer(path, "costCeilingId"));
  const sanitized = sanitizeAllowlistText(value);
  assertBudget(validateApprovalCostCeilingIdBudget(sanitized), pointer(path, "costCeilingId"));
  if (!COST_CEILING_ID_RE.test(sanitized)) {
    throw new Error(`${pointer(path, "costCeilingId")}: must be a valid cost ceiling id`);
  }
  return sanitized;
}

function endpointOriginFromJson(value: string, path: string): string {
  assertBudget(validateApprovalEndpointOriginBudget(value), pointer(path, "endpointOrigin"));
  const sanitized = sanitizeAllowlistText(value);
  assertBudget(validateApprovalEndpointOriginBudget(sanitized), pointer(path, "endpointOrigin"));
  let origin: string;
  try {
    const parsed = new URL(sanitized);
    origin = parsed.origin;
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      throw new Error("unsupported protocol");
    }
  } catch {
    throw new Error(`${pointer(path, "endpointOrigin")}: must be an http(s) URL origin`);
  }
  if (origin !== sanitized) {
    throw new Error(`${pointer(path, "endpointOrigin")}: must be a canonical URL origin`);
  }
  return origin;
}

function reasonFromJson(value: string, path: string): string {
  assertBudget(validateApprovalReasonBudget(value), pointer(path, "reason"));
  const sanitized = sanitizeAllowlistText(value);
  assertBudget(validateApprovalReasonBudget(sanitized), pointer(path, "reason"));
  return sanitized;
}

function thresholdBpsFromJson(value: number, path: string): number {
  if (!Number.isSafeInteger(value) || value < 1 || value > MAX_BUDGET_THRESHOLD_BPS) {
    throw new Error(
      `${pointer(path, "thresholdBps")}: must be an integer in 1..${MAX_BUDGET_THRESHOLD_BPS}`,
    );
  }
  return value;
}

function microUsdFromJson(value: number, path: string): number {
  if (!Number.isSafeInteger(value) || value < 0 || value > MAX_BUDGET_LIMIT_MICRO_USD) {
    throw new Error(
      `${path}: must be a non-negative integer micro-USD amount <= ${MAX_BUDGET_LIMIT_MICRO_USD}`,
    );
  }
  return value;
}

function requiredBase64UrlString(
  record: Readonly<Record<string, unknown>>,
  key: keyof WebAuthnAssertion,
  basePath: string,
): string {
  const value = requiredString(record, key, basePath);
  return base64UrlStringFromJson(value, pointer(basePath, key));
}

function optionalBase64UrlString(
  record: Readonly<Record<string, unknown>>,
  key: keyof WebAuthnAssertion,
  basePath: string,
): string | undefined {
  const value = optionalString(record, key, basePath);
  return value === undefined ? undefined : base64UrlStringFromJson(value, pointer(basePath, key));
}

function base64UrlStringFromJson(value: string, path: string): string {
  assertBudget(validateWebAuthnAssertionFieldBudget(value, `${path} bytes`), path);
  if (!BASE64URL_RE.test(value)) {
    throw new Error(`${path}: must be a non-empty base64url string`);
  }
  return value;
}

function sanitizeAllowlistText(value: string): string {
  return SanitizedString.fromUnknown(value, { policy: "allowlist" }).value;
}

function requiredField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): unknown {
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !hasOwn(record, key)) {
    throw new Error(`${pointer(basePath, key)}: is required`);
  }
  if (!("value" in descriptor)) {
    throw new Error(`${pointer(basePath, key)}: must be a data property`);
  }
  if (descriptor.value === undefined) {
    throw new Error(`${pointer(basePath, key)}: is required`);
  }
  return descriptor.value;
}

function requiredString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): string {
  const value = requiredField(record, key, basePath);
  if (typeof value !== "string") {
    throw new Error(`${pointer(basePath, key)}: must be a string`);
  }
  return value;
}

function optionalString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): string | undefined {
  if (!hasOwn(record, key)) return undefined;
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined) return undefined;
  if (!("value" in descriptor)) {
    throw new Error(`${pointer(basePath, key)}: must be a data property`);
  }
  const value = descriptor.value;
  if (value === undefined) return undefined;
  if (typeof value !== "string") {
    throw new Error(`${pointer(basePath, key)}: must be a string`);
  }
  return value;
}

function requiredNumber(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): number {
  const value = requiredField(record, key, basePath);
  if (typeof value !== "number") {
    throw new Error(`${pointer(basePath, key)}: must be a number`);
  }
  return value;
}

function assertBudget(result: { ok: true } | { ok: false; reason: string }, path: string): void {
  if (!result.ok) {
    throw new Error(`${path}: ${result.reason}`);
  }
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
