import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { canonicalJSON } from "../src/canonical-json.ts";
import {
  APPROVAL_CLAIM_KIND_VALUES,
  APPROVAL_TOKEN_SCHEMA_VERSION,
  type ApprovalClaim,
  type ApprovalClaimKind,
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

interface SignedApprovalTokenAcceptedVector {
  readonly name: string;
  readonly input: unknown;
  readonly expected: {
    readonly canonicalSerialization: string;
  };
}

interface SignedApprovalTokenRejectedVector {
  readonly name: string;
  readonly input: unknown;
  readonly expectedError: string;
}

interface SignedApprovalTokenVectorsFixture {
  readonly schemaVersion: 1;
  readonly comment: string;
  readonly accepted: readonly SignedApprovalTokenAcceptedVector[];
  readonly rejected: readonly SignedApprovalTokenRejectedVector[];
}

const signedApprovalTokenVectors = loadSignedApprovalTokenVectors();

const REQUIRED_CLAIM_FIELD_BY_KIND = {
  cost_spike_acknowledgement: "currentMicroUsd",
  endpoint_allowlist_extension: "reason",
  credential_grant_to_agent: "credentialScope",
  receipt_co_sign: "frozenArgsHash",
} as const satisfies Record<ApprovalClaimKind, string>;

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

  it.each(nonReceiptClaimVariantFixtures())("accepts $name claim tokens directly", ({
    claim,
    scope,
  }) => {
    const token = signedApprovalTokenFixture({ claim, scope });

    const parsed = signedApprovalTokenFromJson(signedApprovalTokenToJsonValue(token));

    expect(parsed.claim).toStrictEqual(claim);
    expect(parsed.scope).toStrictEqual(scope);
  });

  it.each(
    nonReceiptClaimVariantFixtures(),
  )("rejects $name claim tokens missing their kind-specific field", ({ claim, scope }) => {
    const token = mutableJson(
      signedApprovalTokenToJsonValue(signedApprovalTokenFixture({ claim, scope })),
    );
    const missingField = REQUIRED_CLAIM_FIELD_BY_KIND[claim.kind];
    Reflect.deleteProperty(nestedRecord(token, "claim"), missingField);

    expect(() => signedApprovalTokenFromJson(token)).toThrow(
      new RegExp(`${missingField}.*required`),
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
    const atCapSignature = canonicalBase64UrlStringAtMost(
      MAX_WEBAUTHN_ASSERTION_BYTES - assertionOverhead,
    );
    expect(
      webAuthnAssertionFromJson({
        ...baseAssertion,
        signature: atCapSignature,
      }),
    ).toMatchObject({ signature: atCapSignature });

    expect(() =>
      webAuthnAssertionFromJson({
        ...baseAssertion,
        signature: canonicalBase64UrlStringAbove(MAX_WEBAUTHN_ASSERTION_BYTES - assertionOverhead),
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

  it("rejects malformed scalar fields before building a token", () => {
    expect(() => asApprovalClaimId("")).toThrow(/ApprovalClaimId/);
    expect(() => asTimestampMs(-1)).toThrow(/TimestampMs/);

    expectTokenMutationToThrow((token) => {
      token.notBefore = -1;
    }, /notBefore.*TimestampMs/);
    expectTokenMutationToThrow((token) => {
      nestedRecord(token, "claim").schemaVersion = 2;
    }, /schemaVersion.*must be 1/);
    expectTokenMutationToThrow((token) => {
      nestedRecord(token, "claim").claimId = "";
    }, /claimId.*ApprovalClaimId/);
    expectTokenMutationToThrow((token) => {
      nestedRecord(token, "claim").kind = "operator_override";
    }, /kind.*valid approval claim kind/);
    expectTokenMutationToThrow((token) => {
      nestedRecord(token, "scope").mode = "multi_use";
    }, /mode.*single_use/);
    expectTokenMutationToThrow((token) => {
      nestedRecord(token, "scope").maxUses = 2;
    }, /maxUses.*must be 1/);
    expectTokenMutationToThrow((token) => {
      nestedRecord(token, "scope").role = "owner";
    }, /role.*valid approval role/);
    expectTokenMutationToThrow((token) => {
      nestedRecord(token, "claim").writeId = 42;
    }, /writeId.*must be a string/);
  });

  it("rejects malformed claim-kind specific fields", () => {
    const endpoint = nonReceiptClaimVariantFixtures().find(
      (fixture) => fixture.name === "endpoint_allowlist_extension",
    );
    const credential = nonReceiptClaimVariantFixtures().find(
      (fixture) => fixture.name === "credential_grant_to_agent",
    );
    if (endpoint === undefined || credential === undefined) {
      throw new Error("expected endpoint and credential claim fixtures");
    }

    expectTokenMutationToThrow((token) => {
      token.claim = approvalClaimToJsonValue(endpoint.claim);
      token.scope = approvalScopeToJsonValue(endpoint.scope);
      nestedRecord(token, "claim").providerKind = "mistral";
    }, /providerKind.*ProviderKind/);
    expectTokenMutationToThrow((token) => {
      token.claim = approvalClaimToJsonValue(endpoint.claim);
      token.scope = approvalScopeToJsonValue(endpoint.scope);
      nestedRecord(token, "claim").endpointOrigin = "ftp://api.example";
    }, /endpointOrigin.*http\(s\) URL origin/);
    expectTokenMutationToThrow((token) => {
      token.claim = approvalClaimToJsonValue(credential.claim);
      token.scope = approvalScopeToJsonValue(credential.scope);
      nestedRecord(token, "claim").credentialHandleId = "not a handle";
    }, /credentialHandleId.*CredentialHandleId/);
    expectTokenMutationToThrow((token) => {
      token.claim = approvalClaimToJsonValue(credential.claim);
      token.scope = approvalScopeToJsonValue(credential.scope);
      nestedRecord(token, "claim").credentialScope = "unknown-scope";
    }, /credentialScope.*CredentialScope/);
  });

  it("rejects accessor-backed required fields without invoking them", () => {
    const token = mutableJson(signedApprovalTokenToJsonValue(signedApprovalTokenFixture()));
    let invoked = false;
    Object.defineProperty(token, "tokenId", {
      enumerable: true,
      get() {
        invoked = true;
        return "01HX6P2D8T4Y7K9M3N5Q1R6S2V";
      },
    });

    expect(() => signedApprovalTokenFromJson(token)).toThrow(/tokenId.*data property/);
    expect(invoked).toBe(false);
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
    {
      name: "invalid signature length",
      value: { ...webAuthnAssertionFixture(), signature: "A" },
      reason: /signature.*canonical.*base64url/,
    },
    {
      name: "non-zero trailing pad bits",
      value: { ...webAuthnAssertionFixture(), signature: "AB" },
      reason: /signature.*canonical.*base64url/,
    },
  ])("rejects malformed WebAuthn assertion payloads: $name", ({ value, reason }) => {
    expect(() => webAuthnAssertionFromJson(value)).toThrow(reason);
  });
});

describe("SignedApprovalToken conformance vectors", () => {
  it("uses fixture schemaVersion 1", () => {
    expect(signedApprovalTokenVectors.schemaVersion).toBe(1);
  });

  it("covers all claim kinds and required rejection boundaries", () => {
    const acceptedVectorNames = new Set(
      signedApprovalTokenVectors.accepted.map((vector) => vector.name),
    );
    const rejectedVectorNames = new Set(
      signedApprovalTokenVectors.rejected.map((vector) => vector.name),
    );

    expect(
      new Set(signedApprovalTokenVectors.accepted.map((vector) => claimKindOf(vector.input))),
    ).toEqual(new Set(APPROVAL_CLAIM_KIND_VALUES));
    expect(acceptedVectorNames).toContain(
      "endpoint allowlist extension sanitizes reason and keeps html-significant chars",
    );
    expect(acceptedVectorNames).toContain("cost spike token sanitizes cost ceiling id");
    expect(rejectedVectorNames).toContain(
      "endpoint_allowlist_extension rejects default port origin",
    );
    expect(rejectedVectorNames).toContain(
      "endpoint_allowlist_extension rejects uppercase host origin",
    );

    for (const kind of APPROVAL_CLAIM_KIND_VALUES) {
      const vectorsForKind = signedApprovalTokenVectors.rejected.filter(
        (vector) => claimKindOf(vector.input) === kind,
      );
      expect(
        vectorsForKind.some((vector) =>
          Object.hasOwn(fixtureRecord(vector.input, "input"), "extra"),
        ),
      ).toBe(true);
      expect(
        vectorsForKind.some((vector) => Object.hasOwn(claimRecordOf(vector.input), "extra")),
      ).toBe(true);
      expect(
        vectorsForKind.some((vector) => Object.hasOwn(scopeRecordOf(vector.input), "extra")),
      ).toBe(true);
      expect(
        vectorsForKind.some((vector) => Object.hasOwn(signatureRecordOf(vector.input), "extra")),
      ).toBe(true);
      expect(vectorsForKind.some((vector) => scopeClaimKindOf(vector.input) !== kind)).toBe(true);
      expect(
        vectorsForKind.some(
          (vector) =>
            !Object.hasOwn(claimRecordOf(vector.input), REQUIRED_CLAIM_FIELD_BY_KIND[kind]),
        ),
      ).toBe(true);
    }
  });

  for (const vector of signedApprovalTokenVectors.accepted) {
    it(`accepts ${vector.name}`, () => {
      const parsed = signedApprovalTokenFromJson(vector.input);
      expect(canonicalJSON(signedApprovalTokenToJsonValue(parsed))).toBe(
        vector.expected.canonicalSerialization,
      );
    });
  }

  for (const vector of signedApprovalTokenVectors.rejected) {
    it(`rejects ${vector.name}`, () => {
      const message = captureErrorMessage(() => signedApprovalTokenFromJson(vector.input));
      expect(message).toContain(vector.expectedError);
    });
  }
});

type MutableJsonRecord = Record<string, unknown> & {
  claim?: unknown;
  claimId?: unknown;
  credentialHandleId?: unknown;
  credentialScope?: unknown;
  endpointOrigin?: unknown;
  extra?: unknown;
  expiresAt?: unknown;
  kind?: unknown;
  maxUses?: unknown;
  mode?: unknown;
  notBefore?: unknown;
  providerKind?: unknown;
  role?: unknown;
  schemaVersion?: unknown;
  scope?: unknown;
  tokenId?: unknown;
  writeId?: unknown;
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

function nonReceiptClaimVariantFixtures(): readonly {
  readonly name: string;
  readonly claim: Exclude<ApprovalClaim, { readonly kind: "receipt_co_sign" }>;
  readonly scope: Exclude<ApprovalScope, { readonly claimKind: "receipt_co_sign" }>;
}[] {
  return claimVariantFixtures().filter(
    (
      fixture,
    ): fixture is {
      readonly name: string;
      readonly claim: Exclude<ApprovalClaim, { readonly kind: "receipt_co_sign" }>;
      readonly scope: Exclude<ApprovalScope, { readonly claimKind: "receipt_co_sign" }>;
    } => fixture.claim.kind !== "receipt_co_sign" && fixture.scope.claimKind !== "receipt_co_sign",
  );
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

function expectTokenMutationToThrow(
  mutate: (token: MutableJsonRecord) => void,
  reason: RegExp,
): void {
  const token = mutableJson(signedApprovalTokenToJsonValue(signedApprovalTokenFixture()));
  mutate(token);
  expect(() => signedApprovalTokenFromJson(token)).toThrow(reason);
}

function receiptCoSignClaim(
  claim: ApprovalClaim,
): Extract<ApprovalClaim, { readonly kind: "receipt_co_sign" }> {
  if (claim.kind !== "receipt_co_sign") {
    throw new Error("test fixture requires a receipt co-sign claim");
  }
  return claim;
}

function loadSignedApprovalTokenVectors(): SignedApprovalTokenVectorsFixture {
  const parsed: unknown = JSON.parse(
    readFileSync(
      new URL("../testdata/signed-approval-token-vectors.json", import.meta.url),
      "utf8",
    ),
  );
  const record = fixtureRecord(parsed, "fixture");
  assertKnownFixtureKeys(record, "fixture", ["schemaVersion", "comment", "accepted", "rejected"]);
  return {
    schemaVersion: fixtureSchemaVersion(record, "schemaVersion", "fixture"),
    comment: fixtureString(record, "comment", "fixture"),
    accepted: fixtureArray(record, "accepted", "fixture").map((vector, index) =>
      parseAcceptedVector(vector, `fixture.accepted.${index}`),
    ),
    rejected: fixtureArray(record, "rejected", "fixture").map((vector, index) =>
      parseRejectedVector(vector, `fixture.rejected.${index}`),
    ),
  };
}

function parseAcceptedVector(value: unknown, path: string): SignedApprovalTokenAcceptedVector {
  const record = fixtureRecord(value, path);
  assertKnownFixtureKeys(record, path, ["name", "input", "expected"]);
  const expected = fixtureRecord(fixtureField(record, "expected", path), `${path}.expected`);
  assertKnownFixtureKeys(expected, `${path}.expected`, ["canonicalSerialization"]);
  return {
    name: fixtureString(record, "name", path),
    input: fixtureField(record, "input", path),
    expected: {
      canonicalSerialization: fixtureString(expected, "canonicalSerialization", `${path}.expected`),
    },
  };
}

function parseRejectedVector(value: unknown, path: string): SignedApprovalTokenRejectedVector {
  const record = fixtureRecord(value, path);
  assertKnownFixtureKeys(record, path, ["name", "input", "expectedError"]);
  return {
    name: fixtureString(record, "name", path),
    input: fixtureField(record, "input", path),
    expectedError: fixtureString(record, "expectedError", path),
  };
}

function captureErrorMessage(fn: () => unknown): string {
  try {
    fn();
  } catch (err) {
    return err instanceof Error ? err.message : String(err);
  }
  throw new Error("expected function to throw");
}

function canonicalBase64UrlStringAtMost(maxLength: number): string {
  let length = maxLength;
  while (length % 4 === 1) {
    length -= 1;
  }
  return "A".repeat(length);
}

function canonicalBase64UrlStringAbove(minLength: number): string {
  let length = minLength + 1;
  while (length % 4 === 1) {
    length += 1;
  }
  return "A".repeat(length);
}

function claimKindOf(input: unknown): string {
  return fixtureString(claimRecordOf(input), "kind", "input.claim");
}

function scopeClaimKindOf(input: unknown): string {
  return fixtureString(scopeRecordOf(input), "claimKind", "input.scope");
}

function claimRecordOf(input: unknown): Readonly<Record<string, unknown>> {
  return fixtureRecord(
    fixtureField(fixtureRecord(input, "input"), "claim", "input"),
    "input.claim",
  );
}

function scopeRecordOf(input: unknown): Readonly<Record<string, unknown>> {
  return fixtureRecord(
    fixtureField(fixtureRecord(input, "input"), "scope", "input"),
    "input.scope",
  );
}

function signatureRecordOf(input: unknown): Readonly<Record<string, unknown>> {
  return fixtureRecord(
    fixtureField(fixtureRecord(input, "input"), "signature", "input"),
    "input.signature",
  );
}

function fixtureRecord(value: unknown, path: string): Readonly<Record<string, unknown>> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${path}: must be an object`);
  }
  return value as Readonly<Record<string, unknown>>;
}

function fixtureField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): unknown {
  if (!Object.hasOwn(record, key) || record[key] === undefined) {
    throw new Error(`${path}.${key}: is required`);
  }
  return record[key];
}

function fixtureString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string {
  const value = fixtureField(record, key, path);
  if (typeof value !== "string") {
    throw new Error(`${path}.${key}: must be a string`);
  }
  return value;
}

function fixtureArray(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): readonly unknown[] {
  const value = fixtureField(record, key, path);
  if (!Array.isArray(value)) {
    throw new Error(`${path}.${key}: must be an array`);
  }
  return value;
}

function fixtureSchemaVersion(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): 1 {
  const value = fixtureField(record, key, path);
  if (value !== 1) {
    throw new Error(`${path}.${key}: must be 1`);
  }
  return 1;
}

function assertKnownFixtureKeys(
  record: Readonly<Record<string, unknown>>,
  path: string,
  allowed: readonly string[],
): void {
  for (const key of Object.keys(record)) {
    if (!allowed.includes(key)) {
      throw new Error(`${path}/${key}: is not allowed`);
    }
  }
}
