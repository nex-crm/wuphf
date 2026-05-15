import { describe, expect, it } from "vitest";
import { canonicalJSON } from "../src/canonical-json.ts";
import {
  APPROVAL_CLAIM_KIND_VALUES,
  APPROVAL_TOKEN_SCHEMA_VERSION,
  type ApprovalClaim,
  type ApprovalScope,
  approvalClaimFromJson,
  approvalClaimToJsonValue,
  approvalScopeFromJson,
  approvalScopeToJsonValue,
  asAgentId,
  asApprovalClaimId,
  asApprovalTokenId,
  asCredentialHandleId,
  asCredentialScope,
  asProviderKind,
  asTimestampMs,
  isApprovalClaimId,
  isApprovalTokenId,
  isReceiptCoSignClaim,
  isReceiptCoSignScope,
  isTimestampMs,
  MAX_APPROVAL_CLAIM_CANONICAL_JSON_BYTES,
  MAX_APPROVAL_CLAIM_ID_BYTES,
  MAX_APPROVAL_COST_CEILING_ID_BYTES,
  MAX_APPROVAL_ENDPOINT_ORIGIN_BYTES,
  MAX_APPROVAL_IDENTIFIER_BYTES,
  MAX_APPROVAL_REASON_BYTES,
  MAX_APPROVAL_SCOPE_CANONICAL_JSON_BYTES,
  MAX_APPROVAL_TOKEN_ID_BYTES,
  MAX_APPROVAL_TOKEN_LIFETIME_MS,
  MAX_WEBAUTHN_ASSERTION_BYTES,
  type SignedApprovalToken,
  signedApprovalTokenFromJson,
  signedApprovalTokenToJsonValue,
  validateApprovalClaimCanonicalJsonBudget,
  validateApprovalClaimIdBudget,
  validateApprovalCostCeilingIdBudget,
  validateApprovalEndpointOriginBudget,
  validateApprovalIdentifierBudget,
  validateApprovalReasonBudget,
  validateApprovalScopeCanonicalJsonBudget,
  validateApprovalTokenIdBudget,
  validateWebAuthnAssertionBudget,
  validateWebAuthnAssertionFieldBudget,
  type WebAuthnAssertion,
  webAuthnAssertionFromJson,
  webAuthnAssertionToJsonValue,
} from "../src/index.ts";
import { asReceiptId, asWriteId } from "../src/receipt.ts";
import { sha256Hex } from "../src/sha256.ts";

describe("SignedApprovalToken codec", () => {
  it("exposes the approval token public surface through src/index.ts", () => {
    const token = signedApprovalTokenFixture();
    const assertionJson = webAuthnAssertionToJsonValue(token.signature);

    expect(APPROVAL_TOKEN_SCHEMA_VERSION).toBe(1);
    expect(APPROVAL_CLAIM_KIND_VALUES).toContain("receipt_co_sign");
    expect(MAX_APPROVAL_TOKEN_ID_BYTES).toBe(26);
    expect(MAX_APPROVAL_CLAIM_ID_BYTES).toBe(128);
    expect(MAX_APPROVAL_IDENTIFIER_BYTES).toBeGreaterThan(0);
    expect(MAX_APPROVAL_COST_CEILING_ID_BYTES).toBe(128);
    expect(MAX_APPROVAL_ENDPOINT_ORIGIN_BYTES).toBeGreaterThan(0);
    expect(MAX_APPROVAL_REASON_BYTES).toBeGreaterThan(0);
    expect(isApprovalTokenId(token.tokenId)).toBe(true);
    expect(isApprovalClaimId(token.claim.claimId)).toBe(true);
    expect(isTimestampMs(token.notBefore)).toBe(true);
    expect(isReceiptCoSignClaim(token.claim)).toBe(true);
    expect(isReceiptCoSignScope(token.scope)).toBe(true);
    expect(validateApprovalTokenIdBudget(token.tokenId)).toEqual({ ok: true });
    expect(validateApprovalClaimIdBudget(token.claim.claimId)).toEqual({ ok: true });
    expect(validateApprovalIdentifierBudget(token.issuedTo, "issuedTo")).toEqual({ ok: true });
    expect(validateApprovalCostCeilingIdBudget("budget-prod-01")).toEqual({ ok: true });
    expect(validateApprovalEndpointOriginBudget("https://api.openai.example")).toEqual({
      ok: true,
    });
    expect(validateApprovalReasonBudget("Need temporary endpoint access.")).toEqual({ ok: true });
    expect(validateWebAuthnAssertionFieldBudget(assertionJson.signature, "signature")).toEqual({
      ok: true,
    });
    expect(validateWebAuthnAssertionBudget(canonicalJSON(assertionJson))).toEqual({ ok: true });
  });

  it("round-trips through canonical JSON deterministically", () => {
    const token = signedApprovalTokenFixture();
    const parsed = signedApprovalTokenFromJson({
      signature: webAuthnAssertionFixture(),
      issuedTo: token.issuedTo,
      expiresAt: token.expiresAt,
      notBefore: token.notBefore,
      scope: token.scope,
      claim: token.claim,
      tokenId: token.tokenId,
      schemaVersion: token.schemaVersion,
    });

    const firstJson = signedApprovalTokenToJsonValue(parsed);
    const reparsed = signedApprovalTokenFromJson(firstJson);
    const secondJson = signedApprovalTokenToJsonValue(reparsed);

    expect(canonicalJSON(firstJson)).toBe(canonicalJSON(secondJson));
    expect(reparsed).toStrictEqual(parsed);
  });

  it.each([
    {
      name: "token",
      mutate: (record: MutableJsonRecord) => {
        record.extra = true;
      },
    },
    {
      name: "claim",
      mutate: (record: MutableJsonRecord) => {
        nestedRecord(record, "claim").extra = true;
      },
    },
    {
      name: "scope",
      mutate: (record: MutableJsonRecord) => {
        nestedRecord(record, "scope").extra = true;
      },
    },
    {
      name: "signature",
      mutate: (record: MutableJsonRecord) => {
        nestedRecord(record, "signature").extra = true;
      },
    },
  ])("rejects unknown $name keys", ({ mutate }) => {
    const record = mutableJson(signedApprovalTokenToJsonValue(signedApprovalTokenFixture()));

    mutate(record);

    expect(() => signedApprovalTokenFromJson(record)).toThrow(/extra.*not allowed/);
  });

  it.each(claimVariantFixtures())("round-trips $name claims", ({ claim, scope }) => {
    const parsedClaim = approvalClaimFromJson(approvalClaimToJsonValue(claim));
    const parsedScope = approvalScopeFromJson(approvalScopeToJsonValue(scope));
    const token = signedApprovalTokenFixture({ claim: parsedClaim, scope: parsedScope });

    const parsed = signedApprovalTokenFromJson(signedApprovalTokenToJsonValue(token));

    expect(parsed.claim.kind).toBe(claim.kind);
    expect(parsed.scope.claimKind).toBe(scope.claimKind);
    expect(canonicalJSON(signedApprovalTokenToJsonValue(parsed))).toBe(
      canonicalJSON(signedApprovalTokenToJsonValue(token)),
    );
  });

  it("bounds public canonical projection budget helpers at the edge", () => {
    expect(
      validateApprovalClaimCanonicalJsonBudget("A".repeat(MAX_APPROVAL_CLAIM_CANONICAL_JSON_BYTES)),
    ).toEqual({
      ok: true,
    });
    expect(
      validateApprovalClaimCanonicalJsonBudget(
        "A".repeat(MAX_APPROVAL_CLAIM_CANONICAL_JSON_BYTES + 1),
      ),
    ).toMatchObject({ ok: false });

    expect(
      validateApprovalScopeCanonicalJsonBudget("A".repeat(MAX_APPROVAL_SCOPE_CANONICAL_JSON_BYTES)),
    ).toEqual({
      ok: true,
    });
    expect(
      validateApprovalScopeCanonicalJsonBudget(
        "A".repeat(MAX_APPROVAL_SCOPE_CANONICAL_JSON_BYTES + 1),
      ),
    ).toMatchObject({ ok: false });
  });

  it("enforces token and assertion byte budgets at the edge", () => {
    const token = mutableJson(signedApprovalTokenToJsonValue(signedApprovalTokenFixture()));
    token.tokenId = "A".repeat(27);
    expect(() => signedApprovalTokenFromJson(token)).toThrow(/ApprovalTokenId bytes.*27 > 26/);

    const baseAssertion = webAuthnAssertionFixture();
    const assertionOverhead = canonicalJSON({ ...baseAssertion, signature: "" }).length;
    const atCapSignature = "A".repeat(MAX_WEBAUTHN_ASSERTION_BYTES - assertionOverhead);
    expect(
      webAuthnAssertionFromJson({
        ...baseAssertion,
        signature: atCapSignature,
      }),
    ).toMatchObject({ signature: atCapSignature });

    expect(() =>
      webAuthnAssertionFromJson({
        ...baseAssertion,
        signature: `${atCapSignature}A`,
      }),
    ).toThrow(/WebAuthnAssertion canonical JSON bytes/);
  });

  it("rejects tokens that expire before or at notBefore", () => {
    const fixture = signedApprovalTokenToJsonValue(signedApprovalTokenFixture());
    const token = mutableJson(fixture);
    token.expiresAt = fixture.notBefore;

    expect(() => signedApprovalTokenFromJson(token)).toThrow(/expiresAt.*strictly greater/);
  });

  it("rejects token lifetimes over the cap", () => {
    const fixture = signedApprovalTokenToJsonValue(signedApprovalTokenFixture());
    const token = mutableJson(fixture);
    token.expiresAt = fixture.notBefore + MAX_APPROVAL_TOKEN_LIFETIME_MS + 1;

    expect(() => signedApprovalTokenFromJson(token)).toThrow(/approval token lifetime ms/);
  });

  it.each([
    {
      name: "non-object assertions",
      value: "not-an-assertion",
      reason: /must be an object/,
    },
    {
      name: "missing credential ids",
      value: omitKey(webAuthnAssertionFixture(), "credentialId"),
      reason: /credentialId.*required/,
    },
    {
      name: "invalid signature alphabet",
      value: { ...webAuthnAssertionFixture(), signature: "not base64!" },
      reason: /signature.*base64url/,
    },
  ])("rejects malformed WebAuthn assertion payloads: $name", ({ value, reason }) => {
    expect(() => webAuthnAssertionFromJson(value)).toThrow(reason);
  });
});

type MutableJsonRecord = Record<string, unknown> & {
  extra?: unknown;
  expiresAt?: unknown;
  tokenId?: unknown;
};

function signedApprovalTokenFixture(
  overrides: {
    readonly claim?: ApprovalClaim;
    readonly scope?: ApprovalScope;
    readonly signature?: WebAuthnAssertion;
  } = {},
): SignedApprovalToken {
  const claim = overrides.claim ?? receiptCoSignClaimFixture();
  const scope = overrides.scope ?? receiptCoSignScopeFixture(receiptCoSignClaim(claim));
  return {
    schemaVersion: 1,
    tokenId: asApprovalTokenId("01HX6P2D8T4Y7K9M3N5Q1R6S2V"),
    claim,
    scope,
    notBefore: asTimestampMs(Date.UTC(2026, 4, 8, 18, 0, 0, 0)),
    expiresAt: asTimestampMs(Date.UTC(2026, 4, 8, 18, 30, 0, 0)),
    issuedTo: asAgentId("agent_alpha"),
    signature: overrides.signature ?? webAuthnAssertionFixture(),
  };
}

function claimVariantFixtures(): readonly {
  readonly name: string;
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
}[] {
  const costClaim: ApprovalClaim = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim_cost_01"),
    kind: "cost_spike_acknowledgement",
    agentId: asAgentId("agent_alpha"),
    costCeilingId: "budget-prod-01",
    thresholdBps: 2500,
    currentMicroUsd: 42_000_000,
    ceilingMicroUsd: 20_000_000,
  };
  const endpointClaim: ApprovalClaim = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim_endpoint_01"),
    kind: "endpoint_allowlist_extension",
    agentId: asAgentId("agent_alpha"),
    providerKind: asProviderKind("openai"),
    endpointOrigin: "https://api.openai.example",
    reason: "Temporary allowlist expansion for signed approval test.",
  };
  const credentialClaim: ApprovalClaim = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim_credential_01"),
    kind: "credential_grant_to_agent",
    granteeAgentId: asAgentId("agent_alpha"),
    credentialHandleId: asCredentialHandleId("cred_ipc0123456789ABCDEFGHIJKLMNOP"),
    credentialScope: asCredentialScope("openai"),
  };
  const receiptClaim = receiptCoSignClaimFixture();

  return [
    {
      name: "cost_spike_acknowledgement",
      claim: costClaim,
      scope: {
        mode: "single_use",
        claimId: costClaim.claimId,
        claimKind: costClaim.kind,
        role: "approver",
        maxUses: 1,
        agentId: costClaim.agentId,
        costCeilingId: costClaim.costCeilingId,
      },
    },
    {
      name: "endpoint_allowlist_extension",
      claim: endpointClaim,
      scope: {
        mode: "single_use",
        claimId: endpointClaim.claimId,
        claimKind: endpointClaim.kind,
        role: "host",
        maxUses: 1,
        agentId: endpointClaim.agentId,
        providerKind: endpointClaim.providerKind,
        endpointOrigin: endpointClaim.endpointOrigin,
      },
    },
    {
      name: "credential_grant_to_agent",
      claim: credentialClaim,
      scope: {
        mode: "single_use",
        claimId: credentialClaim.claimId,
        claimKind: credentialClaim.kind,
        role: "host",
        maxUses: 1,
        granteeAgentId: credentialClaim.granteeAgentId,
        credentialHandleId: credentialClaim.credentialHandleId,
      },
    },
    {
      name: "receipt_co_sign",
      claim: receiptClaim,
      scope: receiptCoSignScopeFixture(receiptClaim),
    },
  ];
}

function receiptCoSignClaimFixture(): Extract<ApprovalClaim, { readonly kind: "receipt_co_sign" }> {
  return {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim_receipt_cosign_01"),
    kind: "receipt_co_sign",
    receiptId: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
    writeId: asWriteId("write_01"),
    frozenArgsHash: sha256Hex("approval-submit-frozen-args"),
    riskClass: "high",
  };
}

function receiptCoSignScopeFixture(
  claim: Pick<
    Extract<ApprovalClaim, { readonly kind: "receipt_co_sign" }>,
    "claimId" | "kind" | "receiptId" | "writeId" | "frozenArgsHash"
  >,
): Extract<ApprovalScope, { readonly claimKind: "receipt_co_sign" }> {
  return {
    mode: "single_use",
    claimId: claim.claimId,
    claimKind: claim.kind,
    role: "approver",
    maxUses: 1,
    receiptId: claim.receiptId,
    ...(claim.writeId === undefined ? {} : { writeId: claim.writeId }),
    frozenArgsHash: claim.frozenArgsHash,
  };
}

function webAuthnAssertionFixture(): WebAuthnAssertion {
  return {
    credentialId: "Y3JlZGVudGlhbC0wMQ",
    authenticatorData: "YXV0aGVudGljYXRvci1kYXRh",
    clientDataJson: "Y2xpZW50LWRhdGEtanNvbg",
    signature: "c2lnbmF0dXJl",
    userHandle: "dXNlci0wMQ",
  };
}

function mutableJson(value: unknown): MutableJsonRecord {
  return JSON.parse(JSON.stringify(value)) as MutableJsonRecord;
}

function nestedRecord(record: MutableJsonRecord, key: string): MutableJsonRecord {
  const value = record[key];
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`expected nested record at ${key}`);
  }
  return value as MutableJsonRecord;
}

function omitKey(record: unknown, key: string): MutableJsonRecord {
  const copy = mutableJson(record);
  Reflect.deleteProperty(copy, key);
  return copy;
}

function receiptCoSignClaim(
  claim: ApprovalClaim,
): Extract<ApprovalClaim, { readonly kind: "receipt_co_sign" }> {
  if (claim.kind !== "receipt_co_sign") {
    throw new Error("test fixture requires a receipt co-sign claim");
  }
  return claim;
}
