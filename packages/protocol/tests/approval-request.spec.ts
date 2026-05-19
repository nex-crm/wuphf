import { describe, expect, it } from "vitest";
import {
  APPROVAL_REQUEST_SCHEMA_VERSION,
  APPROVAL_REQUEST_STATUS_VALUES,
  type ApprovalClaim,
  type ApprovalDecisionRecord,
  type ApprovalRequest,
  type ApprovalRequestStatus,
  type ApprovalScope,
  approvalAuditPayloadFromJsonValue,
  approvalAuditPayloadToBytes,
  approvalAuditPayloadToJsonValue,
  approvalDecisionRecordToJsonValue,
  approvalRequestFromJson,
  approvalRequestFromJsonValue,
  approvalRequestToJson,
  approvalRequestToJsonValue,
  asAgentId,
  asApprovalClaimId,
  asApprovalRequestId,
  asApprovalTokenId,
  asCredentialHandleId,
  asCredentialScope,
  asProviderKind,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  asTimestampMs,
  asWriteId,
  canonicalJSON,
  isApprovalAuditEventKind,
  isApprovalRequestId,
  MAX_APPROVAL_REQUEST_ID_BYTES,
  type SignedApprovalToken,
  sha256Hex,
  validateApprovalAuditPayloadForKind,
  validateApprovalDecidedAuditPayload,
  validateApprovalDecisionRecord,
  validateApprovalRequest,
  validateApprovalRequestedAuditPayload,
  validateApprovalRequestIdBudget,
  type WebAuthnAssertion,
} from "../src/index.ts";
import approvalRequestVectorsJson from "../testdata/approval-request-vectors.json";

const approvalRequestVectors = approvalRequestVectorsJson as ApprovalRequestVectorsFixture;

const REQUEST_ID = asApprovalRequestId("01HRQ7KZ7D4E6F8G9H0J1K2M3N");
const THREAD_ID = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
const TASK_ID = asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW");
const RECEIPT_ID = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
const OTHER_RECEIPT_ID = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAZ");
const REQUESTED_BY = asSignerIdentity("fran@example.com");
const DECIDED_BY = asSignerIdentity("approver@example.com");
const REQUESTED_AT = new Date("2026-05-08T18:00:00.000Z");
const DECIDED_AT = new Date("2026-05-08T18:05:00.000Z");

describe("ApprovalRequest protocol artifact", () => {
  it("brands ApprovalRequestId and exposes the closed status tuple", () => {
    const coverage: Record<ApprovalRequestStatus, true> = {
      pending: true,
      approved: true,
      rejected: true,
      abstained: true,
    };

    expect(APPROVAL_REQUEST_SCHEMA_VERSION).toBe(1);
    expect(asApprovalRequestId(REQUEST_ID) as string).toBe(REQUEST_ID);
    expect(isApprovalRequestId(REQUEST_ID)).toBe(true);
    expect(isApprovalRequestId("not-a-ulid")).toBe(false);
    expect(MAX_APPROVAL_REQUEST_ID_BYTES).toBe(26);
    expect(validateApprovalRequestIdBudget("A".repeat(MAX_APPROVAL_REQUEST_ID_BYTES))).toEqual({
      ok: true,
    });
    expect([...APPROVAL_REQUEST_STATUS_VALUES].sort()).toStrictEqual(
      (Object.keys(coverage) as ApprovalRequestStatus[]).sort(),
    );
  });

  it("round-trips every field through snake_case canonical JSON", () => {
    const request = approvalRequestFixture();
    const jsonValue = approvalRequestToJsonValue(request);
    const canonical = canonicalJSON(jsonValue);
    const parsed = approvalRequestFromJsonValue(jsonValue);
    const reparsedJsonValue = approvalRequestToJsonValue(parsed);

    expect(jsonValue).toMatchObject({
      request_id: REQUEST_ID,
      risk_class: "high",
      thread_id: THREAD_ID,
      task_id: TASK_ID,
      receipt_id: RECEIPT_ID,
      requested_by: REQUESTED_BY,
      requested_at: REQUESTED_AT.toISOString(),
      schema_version: 1,
    });
    expect(parsed).toStrictEqual(request);
    expect(canonicalJSON(reparsedJsonValue)).toBe(canonical);
    expect(approvalRequestFromJson(approvalRequestToJson(request))).toStrictEqual(request);
  });

  it("round-trips absent optionals without materializing undefined fields", () => {
    const nonReceiptCase = nonReceiptClaimScopeCases()[0];
    if (nonReceiptCase === undefined) throw new Error("missing non-receipt approval case");
    const { claim, scope } = nonReceiptCase;
    const request = approvalRequestFixture({
      claim,
      scope,
      threadId: undefined,
      taskId: undefined,
      receiptId: undefined,
      decision: undefined,
      status: "pending",
    });
    const jsonValue = approvalRequestToJsonValue(request);

    expect(jsonValue).not.toHaveProperty("thread_id");
    expect(jsonValue).not.toHaveProperty("task_id");
    expect(jsonValue).not.toHaveProperty("receipt_id");
    expect(jsonValue).not.toHaveProperty("decision");
    expect(approvalRequestFromJsonValue(jsonValue)).toStrictEqual(request);
  });

  it("rejects unknown keys at the artifact boundary", () => {
    const jsonValue = mutableJson(approvalRequestToJsonValue(approvalRequestFixture()));
    jsonValue.shadow = true;

    expect(() => approvalRequestFromJsonValue(jsonValue)).toThrow(/approvalRequest\/shadow/);
  });

  it("rejects over-budget ApprovalRequestId strings", () => {
    const jsonValue = mutableJson(approvalRequestToJsonValue(approvalRequestFixture()));
    Reflect.set(jsonValue, "request_id", "A".repeat(MAX_APPROVAL_REQUEST_ID_BYTES + 1));

    expect(() => approvalRequestFromJsonValue(jsonValue)).toThrow(/ApprovalRequestId bytes/);
  });

  it("enforces status and decision coupling for every decision outcome", () => {
    const cases = [
      ["approve", "approved"],
      ["reject", "rejected"],
      ["abstain", "abstained"],
    ] as const;

    for (const [decision, status] of cases) {
      const token = decision === "approve" ? signedApprovalTokenFixture() : undefined;
      const request = approvalRequestFixture({
        status,
        decision: decisionRecordFixture({ decision, token }),
      });
      expect(validateApprovalRequest(request)).toEqual({ ok: true });
      expect(approvalRequestFromJson(approvalRequestToJson(request))).toStrictEqual(request);

      const mismatchedStatus = status === "approved" ? "rejected" : "approved";
      expect(validateApprovalRequest({ ...request, status: mismatchedStatus })).toEqual({
        ok: false,
        errors: [{ path: "/decision/decision", message: "must match status" }],
      });
    }

    const approved = approvalRequestFixture();
    const approvedJson = mutableJson(approvalRequestToJsonValue(approved));
    Reflect.deleteProperty(approvedJson, "decision");

    const pendingJson = mutableJson(
      approvalRequestToJsonValue(
        approvalRequestFixture({ status: "pending", decision: approved.decision }),
      ),
    );

    expect(() => approvalRequestFromJsonValue(approvedJson)).toThrow(/decision.*required/);
    expect(() => approvalRequestFromJsonValue(pendingJson)).toThrow(/decision.*must be absent/);
  });

  it("requires approval evidence only for approve decisions", () => {
    const missingToken = approvalRequestFixture({
      decision: decisionRecordFixture({ token: undefined }),
    });
    expect(validateApprovalRequest(missingToken)).toEqual({
      ok: false,
      errors: [{ path: "/decision/token", message: "is required when decision is approve" }],
    });
    expect(() => approvalRequestFromJsonValue(approvalRequestToJsonValue(missingToken))).toThrow(
      /token.*required/,
    );

    for (const [decision, status] of [
      ["reject", "rejected"],
      ["abstain", "abstained"],
    ] as const) {
      expect(
        validateApprovalRequest(
          approvalRequestFixture({
            status,
            decision: decisionRecordFixture({ decision, token: undefined }),
          }),
        ),
      ).toEqual({ ok: true });
    }
  });

  it("binds decision tokens to the request claim, scope, and validity window", () => {
    const request = approvalRequestFixture();
    expect(validateApprovalRequest(request)).toEqual({ ok: true });

    const wrongClaim = receiptCoSignClaimFixture({ receiptId: OTHER_RECEIPT_ID });
    const wrongClaimRequest = approvalRequestFixture({
      decision: decisionRecordFixture({
        token: signedApprovalTokenFixture({
          claim: wrongClaim,
          scope: receiptCoSignScopeFixture(wrongClaim),
        }),
      }),
    });
    expect(validateApprovalRequest(wrongClaimRequest)).toEqual(
      expect.objectContaining({
        ok: false,
        errors: expect.arrayContaining([
          { path: "/decision/token/claim", message: "must match request claim" },
        ]),
      }),
    );

    const claim = receiptCoSignClaimFixture();
    const wrongScopeRequest = approvalRequestFixture({
      decision: decisionRecordFixture({
        token: signedApprovalTokenFixture({
          claim,
          scope: { ...receiptCoSignScopeFixture(claim), role: "host" },
        }),
      }),
    });
    expect(validateApprovalRequest(wrongScopeRequest)).toEqual(
      expect.objectContaining({
        ok: false,
        errors: expect.arrayContaining([
          { path: "/decision/token/scope", message: "must match request scope" },
        ]),
      }),
    );

    for (const decidedAt of [
      new Date("2026-05-08T17:59:59.999Z"),
      new Date("2026-05-08T18:30:00.000Z"),
    ]) {
      expect(
        validateApprovalRequest(
          approvalRequestFixture({
            decision: decisionRecordFixture({ decidedAt }),
          }),
        ),
      ).toEqual({
        ok: false,
        errors: [{ path: "/decision/decidedAt", message: "must be within token validity window" }],
      });
    }
  });

  it("requires receipt_co_sign top-level fields to match the signed claim", () => {
    const good = approvalRequestFixture();
    expect(validateApprovalRequest(good)).toEqual({ ok: true });

    expect(validateApprovalRequest({ ...good, receiptId: OTHER_RECEIPT_ID })).toEqual({
      ok: false,
      errors: [{ path: "/receiptId", message: "must match claim.receiptId" }],
    });
    expect(validateApprovalRequest({ ...good, riskClass: "medium" })).toEqual({
      ok: false,
      errors: [{ path: "/riskClass", message: "must match claim.riskClass" }],
    });
    expect(() =>
      approvalRequestFromJsonValue({
        ...approvalRequestToJsonValue(good),
        receipt_id: OTHER_RECEIPT_ID,
      }),
    ).toThrow(/receiptId.*must match claim\.receiptId/);
    expect(() =>
      approvalRequestFromJsonValue({
        ...approvalRequestToJsonValue(good),
        risk_class: "medium",
      }),
    ).toThrow(/riskClass.*must match claim\.riskClass/);
  });

  it("binds claim and scope for every non-receipt approval claim kind", () => {
    for (const { claim, scope, tamperedScope, path, message } of nonReceiptClaimScopeCases()) {
      const request = approvalRequestFixture({
        claim,
        scope,
        status: "pending",
        decision: undefined,
        receiptId: undefined,
      });
      expect(validateApprovalRequest(request)).toEqual({ ok: true });
      expect(validateApprovalRequest({ ...request, scope: tamperedScope })).toEqual({
        ok: false,
        errors: [{ path, message }],
      });
    }
  });

  it("round-trips approval_requested and approval_decided audit payloads", () => {
    const requestedPayload = approvalRequestedPayloadFixture();
    const decidedPayload = approvalDecidedPayloadFixture();

    expect(isApprovalAuditEventKind("approval_requested")).toBe(true);
    expect(isApprovalAuditEventKind("approval_decided")).toBe(true);
    expect(isApprovalAuditEventKind("approval_decision")).toBe(false);
    expect(validateApprovalRequestedAuditPayload(requestedPayload)).toEqual({ ok: true });
    expect(validateApprovalDecidedAuditPayload(decidedPayload)).toEqual({ ok: true });

    for (const [kind, payload] of [
      ["approval_requested", requestedPayload],
      ["approval_decided", decidedPayload],
    ] as const) {
      const jsonValue = approvalAuditPayloadToJsonValue(kind, payload);
      const bytes = approvalAuditPayloadToBytes(kind, payload);
      const parsedBytes = JSON.parse(new TextDecoder().decode(bytes)) as unknown;
      const decoded = approvalAuditPayloadFromJsonValue(kind, parsedBytes);

      expect(validateApprovalAuditPayloadForKind(kind, payload)).toEqual({ ok: true });
      expect(decoded).toStrictEqual(payload);
      expect(canonicalJSON(parsedBytes)).toBe(canonicalJSON(jsonValue));
    }
  });

  it("exposes the approval decision record helper surface", () => {
    const decision = decisionRecordFixture();

    expect(validateApprovalDecisionRecord(decision)).toEqual({ ok: true });
    expect(approvalDecisionRecordToJsonValue(decision)).toMatchObject({
      decision: "approve",
      decided_by: DECIDED_BY,
      decided_at: DECIDED_AT.toISOString(),
    });
  });

  it("rejects approval audit payload claim/scope mismatches", () => {
    const payload = approvalRequestedPayloadFixture();

    expect(
      validateApprovalAuditPayloadForKind("approval_requested", {
        ...payload,
        scope: { ...payload.scope, frozenArgsHash: sha256Hex("different-hash") },
      }),
    ).toEqual({
      ok: false,
      errors: [{ path: "/scope/frozenArgsHash", message: "must match claim.frozenArgsHash" }],
    });
  });
});

describe("ApprovalRequest conformance vectors", () => {
  it("uses fixture schemaVersion 1 and covers required boundaries", () => {
    const acceptedNames = new Set(approvalRequestVectors.accepted.map((vector) => vector.name));
    const rejectedNames = new Set(approvalRequestVectors.rejected.map((vector) => vector.name));

    expect(approvalRequestVectors.schemaVersion).toBe(1);
    expect(acceptedNames).toContain("approved receipt co-sign with bound token and optionals");
    expect(acceptedNames).toContain("pending cost acknowledgement with absent optionals");
    expect(rejectedNames).toContain("rejects unsupported schemaVersion");
    expect(rejectedNames).toContain("rejects status decision mismatch");
    expect(rejectedNames).toContain("rejects unknown top-level key");
    expect(rejectedNames).toContain("rejects approved request without token");
    expect(rejectedNames).toContain("rejects decision token claim mismatch");
    expect(rejectedNames).toContain("rejects decision token scope mismatch");
    expect(rejectedNames).toContain("rejects decision outside token validity");
    expect(rejectedNames).toContain("rejects receipt id mismatch for receipt co-sign");
    expect(rejectedNames).toContain("rejects risk class mismatch for receipt co-sign");
  });

  for (const vector of approvalRequestVectors.accepted) {
    it(`accepts ${vector.name}`, () => {
      const parsed = approvalRequestFromJsonValue(vector.input);
      expect(canonicalJSON(approvalRequestToJsonValue(parsed))).toBe(
        vector.expected.canonicalSerialization,
      );
    });
  }

  for (const vector of approvalRequestVectors.rejected) {
    it(`rejects ${vector.name}`, () => {
      const message = captureErrorMessage(() => approvalRequestFromJsonValue(vector.input));
      expect(message).toContain(vector.expectedError);
    });
  }
});

interface ApprovalRequestAcceptedVector {
  readonly name: string;
  readonly input: unknown;
  readonly expected: {
    readonly canonicalSerialization: string;
  };
}

interface ApprovalRequestRejectedVector {
  readonly name: string;
  readonly input: unknown;
  readonly expectedError: string;
}

interface ApprovalRequestVectorsFixture {
  readonly schemaVersion: 1;
  readonly comment: string;
  readonly accepted: readonly ApprovalRequestAcceptedVector[];
  readonly rejected: readonly ApprovalRequestRejectedVector[];
}

type ApprovalRequestFixtureOverrides = Partial<
  Omit<
    ApprovalRequest,
    "id" | "claim" | "scope" | "riskClass" | "requestedBy" | "requestedAt" | "schemaVersion"
  >
> & {
  readonly claim?: ApprovalClaim;
  readonly scope?: ApprovalScope;
};

type DecisionRecordFixtureOverrides = Partial<ApprovalDecisionRecord>;

type SignedApprovalTokenFixtureOverrides = Partial<
  Omit<SignedApprovalToken, "schemaVersion" | "tokenId" | "claim" | "scope" | "signature">
> & {
  readonly tokenId?: SignedApprovalToken["tokenId"] | undefined;
  readonly claim?: ApprovalClaim | undefined;
  readonly scope?: ApprovalScope | undefined;
  readonly signature?: WebAuthnAssertion | undefined;
};

type MutableJsonRecord = Record<string, unknown> & {
  decision?: unknown;
  shadow?: unknown;
  status?: unknown;
};

function approvalRequestFixture(overrides: ApprovalRequestFixtureOverrides = {}): ApprovalRequest {
  const claim = overrides.claim ?? receiptCoSignClaimFixture();
  const scope = overrides.scope ?? receiptCoSignScopeFor(claim);
  const hasDecisionOverride = Object.hasOwn(overrides, "decision");
  const decision = hasDecisionOverride ? overrides.decision : decisionRecordFixture();
  return {
    id: REQUEST_ID,
    claim,
    scope,
    riskClass: "high",
    ...(Object.hasOwn(overrides, "threadId")
      ? optionalField("threadId", overrides.threadId)
      : { threadId: THREAD_ID }),
    ...(Object.hasOwn(overrides, "taskId")
      ? optionalField("taskId", overrides.taskId)
      : { taskId: TASK_ID }),
    ...(Object.hasOwn(overrides, "receiptId")
      ? optionalField("receiptId", overrides.receiptId)
      : { receiptId: RECEIPT_ID }),
    requestedBy: REQUESTED_BY,
    requestedAt: REQUESTED_AT,
    status: overrides.status ?? "approved",
    ...(decision === undefined ? {} : { decision }),
    schemaVersion: 1,
  };
}

function receiptCoSignScopeFor(claim: ApprovalClaim): ApprovalScope {
  if (claim.kind !== "receipt_co_sign") {
    throw new Error("approvalRequestFixture requires scope when overriding non-receipt claims");
  }
  return receiptCoSignScopeFixture(claim);
}

function approvalRequestedPayloadFixture() {
  const request = approvalRequestFixture({ status: "pending", decision: undefined });
  return {
    requestId: request.id,
    claim: request.claim,
    scope: request.scope,
    riskClass: request.riskClass,
    threadId: request.threadId,
    taskId: request.taskId,
    receiptId: request.receiptId,
    requestedBy: request.requestedBy,
    requestedAt: request.requestedAt,
  };
}

function approvalDecidedPayloadFixture() {
  const decision = decisionRecordFixture();
  return {
    requestId: REQUEST_ID,
    decision: decision.decision,
    decidedBy: decision.decidedBy,
    decidedAt: decision.decidedAt,
    token: decision.token,
  };
}

function decisionRecordFixture(
  overrides: DecisionRecordFixtureOverrides = {},
): ApprovalDecisionRecord {
  const token = Object.hasOwn(overrides, "token") ? overrides.token : signedApprovalTokenFixture();
  return {
    decision: overrides.decision ?? "approve",
    decidedBy: overrides.decidedBy ?? DECIDED_BY,
    decidedAt: overrides.decidedAt ?? DECIDED_AT,
    ...(token === undefined ? {} : { token }),
  };
}

function signedApprovalTokenFixture(
  overrides: SignedApprovalTokenFixtureOverrides = {},
): SignedApprovalToken {
  const claim = overrides.claim ?? receiptCoSignClaimFixture();
  const scope = overrides.scope ?? receiptCoSignScopeFor(claim);
  return {
    schemaVersion: 1,
    tokenId: overrides.tokenId ?? asApprovalTokenId("01HX6P2D8T4Y7K9M3N5Q1R6S2V"),
    claim,
    scope,
    notBefore: overrides.notBefore ?? asTimestampMs(Date.UTC(2026, 4, 8, 18, 0, 0, 0)),
    expiresAt: overrides.expiresAt ?? asTimestampMs(Date.UTC(2026, 4, 8, 18, 30, 0, 0)),
    issuedTo: overrides.issuedTo ?? asAgentId("agent_alpha"),
    signature: overrides.signature ?? webAuthnAssertionFixture(),
  };
}

function receiptCoSignClaimFixture(
  overrides: Partial<Extract<ApprovalClaim, { readonly kind: "receipt_co_sign" }>> = {},
): Extract<ApprovalClaim, { readonly kind: "receipt_co_sign" }> {
  return {
    schemaVersion: 1,
    claimId: overrides.claimId ?? asApprovalClaimId("claim_receipt_cosign_01"),
    kind: "receipt_co_sign",
    receiptId: overrides.receiptId ?? RECEIPT_ID,
    writeId: overrides.writeId ?? asWriteId("write_01"),
    frozenArgsHash: overrides.frozenArgsHash ?? sha256Hex("approval-request-frozen-args"),
    riskClass: overrides.riskClass ?? "high",
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

function nonReceiptClaimScopeCases(): readonly {
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
  readonly tamperedScope: ApprovalScope;
  readonly path: string;
  readonly message: string;
}[] {
  const costClaim: Extract<ApprovalClaim, { readonly kind: "cost_spike_acknowledgement" }> = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim_cost_01"),
    kind: "cost_spike_acknowledgement",
    agentId: asAgentId("agent_alpha"),
    costCeilingId: "budget-prod-01",
    thresholdBps: 10_000,
    currentMicroUsd: 250_000,
    ceilingMicroUsd: 500_000,
  };
  const costScope: Extract<ApprovalScope, { readonly claimKind: "cost_spike_acknowledgement" }> = {
    mode: "single_use",
    claimId: costClaim.claimId,
    claimKind: costClaim.kind,
    role: "approver",
    maxUses: 1,
    agentId: costClaim.agentId,
    costCeilingId: costClaim.costCeilingId,
  };

  const endpointClaim: Extract<ApprovalClaim, { readonly kind: "endpoint_allowlist_extension" }> = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim_endpoint_01"),
    kind: "endpoint_allowlist_extension",
    agentId: asAgentId("agent_alpha"),
    providerKind: asProviderKind("openai"),
    endpointOrigin: "https://api.openai.example",
    reason: "Temporary allowlist expansion.",
  };
  const endpointScope: Extract<
    ApprovalScope,
    { readonly claimKind: "endpoint_allowlist_extension" }
  > = {
    mode: "single_use",
    claimId: endpointClaim.claimId,
    claimKind: endpointClaim.kind,
    role: "host",
    maxUses: 1,
    agentId: endpointClaim.agentId,
    providerKind: endpointClaim.providerKind,
    endpointOrigin: endpointClaim.endpointOrigin,
  };

  const credentialClaim: Extract<ApprovalClaim, { readonly kind: "credential_grant_to_agent" }> = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim_credential_01"),
    kind: "credential_grant_to_agent",
    granteeAgentId: asAgentId("agent_beta"),
    credentialHandleId: asCredentialHandleId("cred_0123456789ABCDEFGHIJKLMNOPQRSTUV"),
    credentialScope: asCredentialScope("openai"),
  };
  const credentialScope: Extract<
    ApprovalScope,
    { readonly claimKind: "credential_grant_to_agent" }
  > = {
    mode: "single_use",
    claimId: credentialClaim.claimId,
    claimKind: credentialClaim.kind,
    role: "approver",
    maxUses: 1,
    granteeAgentId: credentialClaim.granteeAgentId,
    credentialHandleId: credentialClaim.credentialHandleId,
  };

  return [
    {
      claim: costClaim,
      scope: costScope,
      tamperedScope: { ...costScope, costCeilingId: "budget-prod-02" },
      path: "/scope/costCeilingId",
      message: "must match claim.costCeilingId",
    },
    {
      claim: endpointClaim,
      scope: endpointScope,
      tamperedScope: { ...endpointScope, endpointOrigin: "https://proxy.openai.example" },
      path: "/scope/endpointOrigin",
      message: "must match claim.endpointOrigin",
    },
    {
      claim: credentialClaim,
      scope: credentialScope,
      tamperedScope: {
        ...credentialScope,
        credentialHandleId: asCredentialHandleId("cred_ZYXWVUTSRQPONMLKJIHGFEDCBA98"),
      },
      path: "/scope/credentialHandleId",
      message: "must match claim.credentialHandleId",
    },
  ];
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

function captureErrorMessage(fn: () => unknown): string {
  try {
    fn();
  } catch (err) {
    return err instanceof Error ? err.message : String(err);
  }
  throw new Error("expected function to throw");
}

function optionalField<K extends string, V>(key: K, value: V | undefined): Partial<Record<K, V>> {
  if (value === undefined) return {};
  return { [key]: value } as Partial<Record<K, V>>;
}
