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

const REQUEST_ID = asApprovalRequestId("01HRQ7KZ7D4E6F8G9H0J1K2M3N");
const THREAD_ID = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
const TASK_ID = asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW");
const RECEIPT_ID = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
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
    const request = approvalRequestFixture({
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

  it("enforces status and decision coupling in both directions", () => {
    const approved = approvalRequestFixture();
    const approvedJson = mutableJson(approvalRequestToJsonValue(approved));
    Reflect.deleteProperty(approvedJson, "decision");

    const pendingJson = mutableJson(
      approvalRequestToJsonValue(
        approvalRequestFixture({ status: "pending", decision: approved.decision }),
      ),
    );
    pendingJson.status = "pending";

    const mismatchedDecisionJson = mutableJson(approvalRequestToJsonValue(approved));
    nestedRecord(mismatchedDecisionJson, "decision").decision = "reject";

    expect(() => approvalRequestFromJsonValue(approvedJson)).toThrow(/decision.*required/);
    expect(() => approvalRequestFromJsonValue(pendingJson)).toThrow(/decision.*must be absent/);
    expect(validateApprovalRequest({ ...approved, status: "rejected" })).toEqual({
      ok: false,
      errors: [{ path: "/decision/decision", message: "must match status" }],
    });
    expect(() => approvalRequestFromJsonValue(mismatchedDecisionJson)).toThrow(
      /decision.*must match status/,
    );
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

type ApprovalRequestFixtureOverrides = Partial<
  Omit<
    ApprovalRequest,
    "id" | "claim" | "scope" | "riskClass" | "requestedBy" | "requestedAt" | "schemaVersion"
  >
> & {
  readonly claim?: ApprovalClaim;
  readonly scope?: ApprovalScope;
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

function decisionRecordFixture(): ApprovalDecisionRecord {
  return {
    decision: "approve",
    decidedBy: DECIDED_BY,
    decidedAt: DECIDED_AT,
    token: signedApprovalTokenFixture(),
  };
}

function signedApprovalTokenFixture(): SignedApprovalToken {
  const claim = receiptCoSignClaimFixture();
  return {
    schemaVersion: 1,
    tokenId: asApprovalTokenId("01HX6P2D8T4Y7K9M3N5Q1R6S2V"),
    claim,
    scope: receiptCoSignScopeFixture(claim),
    notBefore: asTimestampMs(Date.UTC(2026, 4, 8, 18, 0, 0, 0)),
    expiresAt: asTimestampMs(Date.UTC(2026, 4, 8, 18, 30, 0, 0)),
    issuedTo: asAgentId("agent_alpha"),
    signature: webAuthnAssertionFixture(),
  };
}

function receiptCoSignClaimFixture(): Extract<ApprovalClaim, { readonly kind: "receipt_co_sign" }> {
  return {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim_receipt_cosign_01"),
    kind: "receipt_co_sign",
    receiptId: RECEIPT_ID,
    writeId: asWriteId("write_01"),
    frozenArgsHash: sha256Hex("approval-request-frozen-args"),
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

function optionalField<K extends string, V>(key: K, value: V | undefined): Partial<Record<K, V>> {
  if (value === undefined) return {};
  return { [key]: value } as Partial<Record<K, V>>;
}
