import { describe, expect, it } from "vitest";
import {
  type ApprovalClaim,
  type ApprovalScope,
  type ApprovalView,
  asAgentId,
  asApprovalClaimId,
  asApprovalRequestId,
  asCredentialHandleId,
  asCredentialScope,
  asProviderKind,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  asWriteId,
  sha256Hex,
  validateApprovalView,
} from "../src/index.ts";

const REQUEST_ID = asApprovalRequestId("01HRQ7KZ7D4E6F8G9H0J1K2M3N");
const THREAD_ID = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
const TASK_ID = asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW");
const RECEIPT_ID = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
const OTHER_RECEIPT_ID = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAZ");
const REQUESTED_BY = asSignerIdentity("fran@example.com");
const DECIDED_BY = asSignerIdentity("approver@example.com");
const REQUESTED_AT = new Date("2026-05-08T18:00:00.000Z");
const DECIDED_AT = new Date("2026-05-08T18:05:00.000Z");

describe("approval view validator", () => {
  it("returns structured errors for malformed scalar fields", () => {
    expect(validateApprovalView("not-an-object")).toEqual({
      ok: false,
      errors: [{ path: "", message: "must be an object" }],
    });
    expect(validateApprovalView({ ...approvalViewFixture(), id: "not-a-ulid" })).toEqual({
      ok: false,
      errors: [{ path: "/id", message: "must be an uppercase ULID ApprovalRequestId" }],
    });
    expect(
      validateApprovalView({ ...approvalViewFixture(), requestedAt: new Date("invalid") }),
    ).toEqual({
      ok: false,
      errors: [{ path: "/requestedAt", message: "must be a valid Date" }],
    });
    expect(validateApprovalView({ ...approvalViewFixture(), schemaVersion: 2 })).toEqual({
      ok: false,
      errors: [{ path: "/schemaVersion", message: "must be 1" }],
    });
    const { claim: costClaim, scope: costScope } = costApprovalPair();
    expect(
      validateApprovalView({
        ...approvalViewFixture({ claim: costClaim, scope: costScope }),
        riskClass: "severe",
      }),
    ).toEqual({
      ok: false,
      errors: [{ path: "/riskClass", message: "must be a valid risk class" }],
    });
    expect(validateApprovalView({ ...approvalViewFixture(), status: "done" })).toEqual({
      ok: false,
      errors: [{ path: "/status", message: "must be a valid approval request status" }],
    });
    expect(validateApprovalView({ ...approvalViewFixture(), requestedBy: "" })).toEqual({
      ok: false,
      errors: [{ path: "/requestedBy", message: "must be a bounded non-empty SignerIdentity" }],
    });
  });

  it("validates decision summary object shape and status coupling", () => {
    const decisionSummary = decisionSummaryFixture();

    expect(validateApprovalView({ ...approvalViewFixture(), decisionSummary: "approved" })).toEqual(
      {
        ok: false,
        errors: [{ path: "/decisionSummary", message: "must be an object" }],
      },
    );
    expect(
      validateApprovalView({
        ...approvalViewFixture(),
        decisionSummary: { ...decisionSummary, decision: "maybe" },
      }),
    ).toEqual({
      ok: false,
      errors: [{ path: "/decisionSummary/decision", message: "must be a valid approval decision" }],
    });
    expect(
      validateApprovalView({
        ...approvalViewFixture(),
        decisionSummary: { ...decisionSummary, decidedBy: "" },
      }),
    ).toEqual({
      ok: false,
      errors: [
        {
          path: "/decisionSummary/decidedBy",
          message: "must be a bounded non-empty SignerIdentity",
        },
      ],
    });
    expect(
      validateApprovalView({
        ...approvalViewFixture(),
        decisionSummary: { ...decisionSummary, decidedAt: new Date("invalid") },
      }),
    ).toEqual({
      ok: false,
      errors: [{ path: "/decisionSummary/decidedAt", message: "must be a valid Date" }],
    });
    expect(
      validateApprovalView({
        ...approvalViewFixture(),
        status: "rejected",
        decisionSummary: { ...decisionSummary, decision: "reject" },
      }),
    ).toEqual({ ok: true });
    expect(
      validateApprovalView({
        ...approvalViewFixture(),
        status: "abstained",
        decisionSummary: { ...decisionSummary, decision: "abstain" },
      }),
    ).toEqual({ ok: true });
  });

  it("rejects required and optional accessors without invoking getters", () => {
    let idGetterCalled = false;
    const requiredAccessor = { ...approvalViewFixture() } as Record<string, unknown>;
    Object.defineProperty(requiredAccessor, "id", {
      enumerable: true,
      get() {
        idGetterCalled = true;
        return REQUEST_ID;
      },
    });
    expect(validateApprovalView(requiredAccessor)).toEqual({
      ok: false,
      errors: [{ path: "/id", message: "must be a data property" }],
    });
    expect(idGetterCalled).toBe(false);

    let threadGetterCalled = false;
    const optionalAccessor = { ...approvalViewFixture() } as Record<string, unknown>;
    Object.defineProperty(optionalAccessor, "threadId", {
      enumerable: true,
      get() {
        threadGetterCalled = true;
        return THREAD_ID;
      },
    });
    expect(validateApprovalView(optionalAccessor)).toEqual({
      ok: false,
      errors: [{ path: "/threadId", message: "must be a data property" }],
    });
    expect(threadGetterCalled).toBe(false);
    expect(validateApprovalView({ ...approvalViewFixture(), threadId: undefined })).toEqual({
      ok: true,
    });
  });

  it("rejects missing and undefined required fields", () => {
    const missingRequired = { ...approvalViewFixture() } as Record<string, unknown>;
    Reflect.deleteProperty(missingRequired, "requestedBy");
    expect(validateApprovalView(missingRequired)).toEqual({
      ok: false,
      errors: [{ path: "/requestedBy", message: "is required" }],
    });
    expect(validateApprovalView({ ...approvalViewFixture(), requestedBy: undefined })).toEqual({
      ok: false,
      errors: [{ path: "/requestedBy", message: "is required" }],
    });
  });

  it("rejects unknown keys, malformed nested approvals, and invalid optional ids", () => {
    expect(validateApprovalView({ ...approvalViewFixture(), shadow: true })).toEqual({
      ok: false,
      errors: [{ path: "/shadow", message: "is not allowed" }],
    });
    expect(validateApprovalView({ ...approvalViewFixture(), claim: {} })).toEqual({
      ok: false,
      errors: [{ path: "/claim", message: "/claim/kind: is required" }],
    });
    expect(validateApprovalView({ ...approvalViewFixture(), scope: {} })).toEqual({
      ok: false,
      errors: [{ path: "/scope", message: "/scope/claimKind: is required" }],
    });
    expect(validateApprovalView({ ...approvalViewFixture(), threadId: "bad" })).toEqual({
      ok: false,
      errors: [{ path: "/threadId", message: "must be an uppercase ULID ThreadId" }],
    });
    expect(validateApprovalView({ ...approvalViewFixture(), taskId: "bad" })).toEqual({
      ok: false,
      errors: [{ path: "/taskId", message: "must be an uppercase ULID TaskId" }],
    });
    expect(validateApprovalView({ ...approvalViewFixture(), receiptId: "bad" })).toEqual({
      ok: false,
      errors: [
        { path: "/receiptId", message: "must be an uppercase ULID ReceiptId" },
        { path: "/receiptId", message: "must match claim.receiptId" },
      ],
    });
    expect(
      validateApprovalView({
        ...approvalViewFixture(),
        decisionSummary: { ...decisionSummaryFixture(), token: true },
      }),
    ).toEqual({
      ok: false,
      errors: [{ path: "/decisionSummary/token", message: "is not allowed" }],
    });
  });

  it("rejects receipt approval view binding drift", () => {
    const claim = receiptCoSignClaimFixture();
    expect(
      validateApprovalView(
        approvalViewFixture({
          claim,
          scope: { ...receiptCoSignScopeFixture(claim), writeId: asWriteId("write_02") },
        }),
      ),
    ).toEqual({
      ok: false,
      errors: [{ path: "/scope/writeId", message: "must match claim.writeId" }],
    });
    expect(validateApprovalView({ ...approvalViewFixture(), receiptId: OTHER_RECEIPT_ID })).toEqual(
      {
        ok: false,
        errors: [{ path: "/receiptId", message: "must match claim.receiptId" }],
      },
    );
    expect(validateApprovalView({ ...approvalViewFixture(), riskClass: "medium" })).toEqual({
      ok: false,
      errors: [{ path: "/riskClass", message: "must match claim.riskClass" }],
    });
  });

  it("rejects non-receipt approval view binding drift", () => {
    const { claim: costClaim, scope: costScope } = costApprovalPair();
    expect(
      validateApprovalView(
        approvalViewFixture({
          claim: costClaim,
          scope: { ...costScope, claimId: asApprovalClaimId("claim_cost_02") },
        }),
      ),
    ).toEqual({
      ok: false,
      errors: [{ path: "/scope/claimId", message: "must match claim.claimId" }],
    });
    expect(
      validateApprovalView(
        approvalViewFixture({
          claim: costClaim,
          scope: receiptCoSignScopeFixture(receiptCoSignClaimFixture()),
        }),
      ),
    ).toEqual(
      expect.objectContaining({
        ok: false,
        errors: expect.arrayContaining([
          { path: "/scope/claimKind", message: "must match claim.kind" },
        ]),
      }),
    );

    expect(
      validateApprovalView(
        approvalViewFixture({
          claim: costClaim,
          scope: { ...costScope, agentId: asAgentId("agent_gamma") },
        }),
      ),
    ).toEqual({
      ok: false,
      errors: [{ path: "/scope/agentId", message: "must match claim.agentId" }],
    });

    const { claim: endpointClaim, scope: endpointScope } = endpointApprovalPair();
    expect(
      validateApprovalView(
        approvalViewFixture({
          claim: endpointClaim,
          scope: { ...endpointScope, endpointOrigin: "https://proxy.openai.example" },
        }),
      ),
    ).toEqual({
      ok: false,
      errors: [{ path: "/scope/endpointOrigin", message: "must match claim.endpointOrigin" }],
    });

    const { claim: credentialClaim, scope: credentialScope } = credentialApprovalPair();
    expect(
      validateApprovalView(
        approvalViewFixture({
          claim: credentialClaim,
          scope: {
            ...credentialScope,
            credentialHandleId: asCredentialHandleId("cred_ZYXWVUTSRQPONMLKJIHGFEDCBA98"),
          },
        }),
      ),
    ).toEqual({
      ok: false,
      errors: [
        { path: "/scope/credentialHandleId", message: "must match claim.credentialHandleId" },
      ],
    });
  });
});

function approvalViewFixture(overrides: Partial<ApprovalView> = {}): ApprovalView {
  const claim = overrides.claim ?? receiptCoSignClaimFixture();
  const scope = overrides.scope ?? receiptCoSignScopeFor(claim);
  const decisionSummary = Object.hasOwn(overrides, "decisionSummary")
    ? overrides.decisionSummary
    : decisionSummaryFixture();
  return {
    id: REQUEST_ID,
    claim,
    scope,
    riskClass: "high",
    threadId: THREAD_ID,
    taskId: TASK_ID,
    receiptId: RECEIPT_ID,
    requestedBy: REQUESTED_BY,
    requestedAt: REQUESTED_AT,
    status: "approved",
    ...(decisionSummary === undefined ? {} : { decisionSummary }),
    schemaVersion: 1,
    ...overrides,
  };
}

function decisionSummaryFixture() {
  return {
    decision: "approve" as const,
    decidedBy: DECIDED_BY,
    decidedAt: DECIDED_AT,
  };
}

function receiptCoSignScopeFor(claim: ApprovalClaim): ApprovalScope {
  if (claim.kind !== "receipt_co_sign") {
    throw new Error("approvalViewFixture requires scope when overriding non-receipt claims");
  }
  return receiptCoSignScopeFixture(claim);
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
    writeId: claim.writeId,
    frozenArgsHash: claim.frozenArgsHash,
  };
}

function costApprovalPair(): {
  readonly claim: Extract<ApprovalClaim, { readonly kind: "cost_spike_acknowledgement" }>;
  readonly scope: Extract<ApprovalScope, { readonly claimKind: "cost_spike_acknowledgement" }>;
} {
  const claim: Extract<ApprovalClaim, { readonly kind: "cost_spike_acknowledgement" }> = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim_cost_01"),
    kind: "cost_spike_acknowledgement",
    agentId: asAgentId("agent_alpha"),
    costCeilingId: "budget-prod-01",
    thresholdBps: 10_000,
    currentMicroUsd: 250_000,
    ceilingMicroUsd: 500_000,
  };
  return {
    claim,
    scope: {
      mode: "single_use",
      claimId: claim.claimId,
      claimKind: claim.kind,
      role: "approver",
      maxUses: 1,
      agentId: claim.agentId,
      costCeilingId: claim.costCeilingId,
    },
  };
}

function endpointApprovalPair(): {
  readonly claim: Extract<ApprovalClaim, { readonly kind: "endpoint_allowlist_extension" }>;
  readonly scope: Extract<ApprovalScope, { readonly claimKind: "endpoint_allowlist_extension" }>;
} {
  const claim: Extract<ApprovalClaim, { readonly kind: "endpoint_allowlist_extension" }> = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim_endpoint_01"),
    kind: "endpoint_allowlist_extension",
    agentId: asAgentId("agent_alpha"),
    providerKind: asProviderKind("openai"),
    endpointOrigin: "https://api.openai.example",
    reason: "Temporary allowlist expansion.",
  };
  return {
    claim,
    scope: {
      mode: "single_use",
      claimId: claim.claimId,
      claimKind: claim.kind,
      role: "host",
      maxUses: 1,
      agentId: claim.agentId,
      providerKind: claim.providerKind,
      endpointOrigin: claim.endpointOrigin,
    },
  };
}

function credentialApprovalPair(): {
  readonly claim: Extract<ApprovalClaim, { readonly kind: "credential_grant_to_agent" }>;
  readonly scope: Extract<ApprovalScope, { readonly claimKind: "credential_grant_to_agent" }>;
} {
  const claim: Extract<ApprovalClaim, { readonly kind: "credential_grant_to_agent" }> = {
    schemaVersion: 1,
    claimId: asApprovalClaimId("claim_credential_01"),
    kind: "credential_grant_to_agent",
    granteeAgentId: asAgentId("agent_beta"),
    credentialHandleId: asCredentialHandleId("cred_0123456789ABCDEFGHIJKLMNOPQRSTUV"),
    credentialScope: asCredentialScope("openai"),
  };
  return {
    claim,
    scope: {
      mode: "single_use",
      claimId: claim.claimId,
      claimKind: claim.kind,
      role: "approver",
      maxUses: 1,
      granteeAgentId: claim.granteeAgentId,
      credentialHandleId: claim.credentialHandleId,
    },
  };
}
