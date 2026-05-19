import { describe, expect, it } from "vitest";
import {
  type ApprovalClaim,
  type ApprovalDecisionRequest,
  type ApprovalDecisionResponse,
  type ApprovalGetResponse,
  type ApprovalListResponse,
  type ApprovalRequest,
  type ApprovalRequestCreateRequest,
  type ApprovalRequestCreateResponse,
  type ApprovalScope,
  type ApprovalView,
  approvalDecisionRequestFromJson,
  approvalDecisionRequestToJsonValue,
  approvalDecisionResponseFromJson,
  approvalDecisionResponseToJsonValue,
  approvalGetResponseFromJson,
  approvalGetResponseToJsonValue,
  approvalListResponseFromJson,
  approvalListResponseToJsonValue,
  approvalRequestCreateRequestFromJson,
  approvalRequestCreateRequestToJsonValue,
  approvalRequestCreateResponseFromJson,
  approvalRequestCreateResponseToJsonValue,
  approvalRequestToJsonValue,
  approvalViewFromJson,
  approvalViewToJsonValue,
  asAgentId,
  asApprovalClaimId,
  asApprovalRequestId,
  asApprovalTokenId,
  asIdempotencyKey,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  asThreadSpecRevisionId,
  asTimestampMs,
  asWriteId,
  canonicalJSON,
  lsnFromV1Number,
  MAX_ROUTE_APPROVAL_LIST_ITEMS,
  MAX_ROUTE_CURSOR_BYTES,
  MAX_ROUTE_ERROR_CODE_BYTES,
  MAX_ROUTE_ERROR_MESSAGE_BYTES,
  MAX_ROUTE_THREAD_LIST_ITEMS,
  MAX_THREAD_SPEC_CONTENT_BYTES,
  MAX_THREAD_TITLE_BYTES,
  ROUTE_ENVELOPE_SCHEMA_VERSION,
  type RouteError,
  routeErrorFromJson,
  routeErrorToJsonValue,
  type SignedApprovalToken,
  sha256Hex,
  THREAD_ATTENTION_REASON_VALUES,
  THREAD_BOARD_COLUMN_VALUES,
  THREAD_CURRENT_SEAT_VALUES,
  THREAD_EFFECTIVE_STATUS_VALUES,
  type Thread,
  type ThreadCreateRequest,
  type ThreadGetResponse,
  type ThreadListResponse,
  type ThreadMutationResponse,
  type ThreadPinnedApprovalsResponse,
  type ThreadSpecEditRequest,
  type ThreadStatusChangeRequest,
  type ThreadView,
  threadCreateRequestFromJson,
  threadCreateRequestToJsonValue,
  threadGetResponseFromJson,
  threadGetResponseToJsonValue,
  threadListResponseFromJson,
  threadListResponseToJsonValue,
  threadMutationResponseFromJson,
  threadMutationResponseToJsonValue,
  threadPinnedApprovalsResponseFromJson,
  threadPinnedApprovalsResponseToJsonValue,
  threadSpecContentHash,
  threadSpecEditRequestFromJson,
  threadSpecEditRequestToJsonValue,
  threadStatusChangeRequestFromJson,
  threadStatusChangeRequestToJsonValue,
  threadViewFromJson,
  threadViewToJsonValue,
  validateApprovalView,
  validateRouteCursorBudget,
  validateRouteErrorCodeBudget,
  validateRouteErrorMessageBudget,
  type WebAuthnAssertion,
} from "../src/index.ts";
import routeEnvelopeVectorsJson from "../testdata/route-envelope-vectors.json";

const routeEnvelopeVectors = routeEnvelopeVectorsJson as RouteEnvelopeVectorsFixture;

const THREAD_ID = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
const TASK_ID = asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW");
const RECEIPT_ID = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
const OTHER_RECEIPT_ID = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAZ");
const REVISION_1 = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA0");
const REVISION_2 = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA1");
const REQUEST_ID = asApprovalRequestId("01HRQ7KZ7D4E6F8G9H0J1K2M3N");
const SIGNER = asSignerIdentity("fran@example.com");
const DECIDER = asSignerIdentity("approver@example.com");
const CREATED_AT = new Date("2026-05-08T18:00:00.000Z");
const UPDATED_AT = new Date("2026-05-08T18:05:00.000Z");
const DECIDED_AT = new Date("2026-05-08T18:05:00.000Z");
const IDEMPOTENCY_KEY = asIdempotencyKey("route-envelope-01");
const HEAD_LSN = lsnFromV1Number(42);

describe("route-envelope codecs", () => {
  it("round-trips thread route requests and mutation responses", () => {
    const createWithRefs: ThreadCreateRequest = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      title: "Approval request protocol",
      specContent: specContentFixture(),
      externalRefs: externalRefsFixture(),
      idempotencyKey: IDEMPOTENCY_KEY,
    };
    const createWithoutRefs = threadCreateRequestFromJson({
      title: "Approval request protocol",
      specContent: specContentFixture(),
      idempotencyKey: IDEMPOTENCY_KEY,
    });
    const specEdit: ThreadSpecEditRequest = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      baseRevisionId: REVISION_1,
      baseContentHash: threadSpecContentHash(specContentFixture()),
      content: { body: "Edited", checklist: ["tests", "vectors"] },
      idempotencyKey: IDEMPOTENCY_KEY,
    };
    const statusChange: ThreadStatusChangeRequest = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      fromStatus: "open",
      toStatus: "in_progress",
      idempotencyKey: IDEMPOTENCY_KEY,
    };
    const mutation: ThreadMutationResponse = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      threadId: THREAD_ID,
      headLsn: HEAD_LSN,
      revisionId: REVISION_2,
      contentHash: threadSpecContentHash({ body: "Edited", checklist: ["tests", "vectors"] }),
    };

    expect(
      roundTrip(createWithRefs, threadCreateRequestToJsonValue, threadCreateRequestFromJson),
    ).toStrictEqual(createWithRefs);
    expect(threadCreateRequestToJsonValue(createWithoutRefs)).not.toHaveProperty("externalRefs");
    expect(
      roundTrip(createWithoutRefs, threadCreateRequestToJsonValue, threadCreateRequestFromJson),
    ).toStrictEqual({
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      title: createWithoutRefs.title,
      specContent: createWithoutRefs.specContent,
      idempotencyKey: createWithoutRefs.idempotencyKey,
    });
    expect(
      roundTrip(specEdit, threadSpecEditRequestToJsonValue, threadSpecEditRequestFromJson),
    ).toStrictEqual(specEdit);
    expect(
      roundTrip(
        statusChange,
        threadStatusChangeRequestToJsonValue,
        threadStatusChangeRequestFromJson,
      ),
    ).toStrictEqual(statusChange);
    expect(
      roundTrip(mutation, threadMutationResponseToJsonValue, threadMutationResponseFromJson),
    ).toStrictEqual(mutation);
  });

  it("round-trips thread list and get responses with nested Thread codec output", () => {
    const thread = threadViewFixture();
    const listWithCursor: ThreadListResponse = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      threads: [thread],
      nextCursor: "bHNuOjQy",
    };
    const listWithoutCursor: ThreadListResponse = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      threads: [thread],
    };
    const getResponse: ThreadGetResponse = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      thread,
    };

    expect(
      roundTrip(listWithCursor, threadListResponseToJsonValue, threadListResponseFromJson),
    ).toStrictEqual(listWithCursor);
    expect(threadListResponseToJsonValue(listWithoutCursor)).not.toHaveProperty("nextCursor");
    expect(
      roundTrip(listWithoutCursor, threadListResponseToJsonValue, threadListResponseFromJson),
    ).toStrictEqual(listWithoutCursor);
    expect(
      roundTrip(getResponse, threadGetResponseToJsonValue, threadGetResponseFromJson),
    ).toStrictEqual(getResponse);
    const listJson = threadListResponseToJsonValue(listWithCursor) as Readonly<{
      threads: unknown;
    }>;
    expect(listJson.threads).toEqual([threadViewToJsonValue(thread)]);
  });

  it("round-trips approval route requests and responses", () => {
    const claim = receiptCoSignClaimFixture();
    const receiptCreate: ApprovalRequestCreateRequest = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      claim,
      scope: receiptCoSignScopeFixture(claim),
      riskClass: "high",
      threadId: THREAD_ID,
      taskId: TASK_ID,
      receiptId: RECEIPT_ID,
      idempotencyKey: IDEMPOTENCY_KEY,
    };
    const { claim: costClaim, scope: costScope } = costApprovalPair();
    const costCreate = approvalRequestCreateRequestFromJson({
      claim: costClaim,
      scope: costScope,
      riskClass: "medium",
      idempotencyKey: IDEMPOTENCY_KEY,
    });
    const approveDecision: ApprovalDecisionRequest = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      decision: "approve",
      token: signedApprovalTokenFixture(),
      idempotencyKey: IDEMPOTENCY_KEY,
    };
    const rejectDecision = approvalDecisionRequestFromJson({
      decision: "reject",
      idempotencyKey: IDEMPOTENCY_KEY,
    });
    const createResponse: ApprovalRequestCreateResponse = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      approvalRequest: approvalRequestFixture({ status: "pending", decision: undefined }),
      headLsn: HEAD_LSN,
    };
    const decisionResponse: ApprovalDecisionResponse = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      approvalRequest: approvalRequestFixture(),
      headLsn: lsnFromV1Number(43),
    };
    const approvedView = approvalViewFixture();
    const pendingView = approvalViewFixture({ status: "pending", decisionSummary: undefined });
    const listResponse: ApprovalListResponse = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      approvals: [approvedView, pendingView],
      nextCursor: "bHNuOjQz",
    };
    const getResponse: ApprovalGetResponse = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      approval: approvedView,
    };
    const pinnedApprovals: ThreadPinnedApprovalsResponse = {
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      threadId: THREAD_ID,
      headLsn: lsnFromV1Number(44),
      approvals: [pendingView],
    };

    expect(
      roundTrip(
        receiptCreate,
        approvalRequestCreateRequestToJsonValue,
        approvalRequestCreateRequestFromJson,
      ),
    ).toStrictEqual(receiptCreate);
    expect(approvalRequestCreateRequestToJsonValue(costCreate)).not.toHaveProperty("threadId");
    expect(
      roundTrip(
        costCreate,
        approvalRequestCreateRequestToJsonValue,
        approvalRequestCreateRequestFromJson,
      ),
    ).toStrictEqual({
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      claim: costClaim,
      scope: costScope,
      riskClass: "medium",
      idempotencyKey: IDEMPOTENCY_KEY,
    });
    expect(
      roundTrip(
        approveDecision,
        approvalDecisionRequestToJsonValue,
        approvalDecisionRequestFromJson,
      ),
    ).toStrictEqual(approveDecision);
    expect(approvalDecisionRequestToJsonValue(rejectDecision)).not.toHaveProperty("token");
    expect(
      roundTrip(
        rejectDecision,
        approvalDecisionRequestToJsonValue,
        approvalDecisionRequestFromJson,
      ),
    ).toStrictEqual({
      schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
      decision: "reject",
      idempotencyKey: IDEMPOTENCY_KEY,
    });
    expect(
      roundTrip(
        createResponse,
        approvalRequestCreateResponseToJsonValue,
        approvalRequestCreateResponseFromJson,
      ),
    ).toStrictEqual(createResponse);
    expect(
      roundTrip(
        decisionResponse,
        approvalDecisionResponseToJsonValue,
        approvalDecisionResponseFromJson,
      ),
    ).toStrictEqual(decisionResponse);
    const decisionResponseJson = approvalDecisionResponseToJsonValue(decisionResponse) as Readonly<{
      approvalRequest: unknown;
    }>;
    expect(decisionResponseJson.approvalRequest).toEqual(
      approvalRequestToJsonValue(decisionResponse.approvalRequest),
    );
    expect(roundTrip(approvedView, approvalViewToJsonValue, approvalViewFromJson)).toStrictEqual(
      approvedView,
    );
    expect(
      roundTrip(listResponse, approvalListResponseToJsonValue, approvalListResponseFromJson),
    ).toStrictEqual(listResponse);
    expect(
      roundTrip(getResponse, approvalGetResponseToJsonValue, approvalGetResponseFromJson),
    ).toStrictEqual(getResponse);
    expect(
      roundTrip(
        pinnedApprovals,
        threadPinnedApprovalsResponseToJsonValue,
        threadPinnedApprovalsResponseFromJson,
      ),
    ).toStrictEqual(pinnedApprovals);
    expect(validateApprovalView(approvedView).ok).toBe(true);

    const viewJson = approvalViewToJsonValue(approvedView) as JsonObject & {
      readonly decisionSummary?: unknown;
    };
    expect(viewJson).toHaveProperty("decisionSummary");
    expect(viewJson).not.toHaveProperty("decision");
    expect(viewJson.decisionSummary).not.toHaveProperty("token");
    expect(JSON.stringify(viewJson)).not.toContain("tokenId");
  });

  it("round-trips route errors with optional diagnostics present and absent", () => {
    const full: RouteError = {
      error: "store_busy",
      message: "The projection store is temporarily busy.",
      retryAfterMs: 1000,
    };
    const minimal: RouteError = { error: "invalid_payload" };

    expect(roundTrip(full, routeErrorToJsonValue, routeErrorFromJson)).toStrictEqual(full);
    expect(routeErrorToJsonValue(minimal)).not.toHaveProperty("message");
    expect(routeErrorToJsonValue(minimal)).not.toHaveProperty("retryAfterMs");
    expect(roundTrip(minimal, routeErrorToJsonValue, routeErrorFromJson)).toStrictEqual(minimal);
  });

  it("rejects unknown keys at every route-envelope boundary", () => {
    for (const item of strictKnownKeyCases()) {
      expect(
        captureErrorMessage(() => item.parse({ ...item.input, shadow: true })),
        item.name,
      ).toContain("shadow");
    }
  });

  it("rejects byte-budget and bounded-list violations", () => {
    expect(() =>
      threadCreateRequestFromJson({
        schemaVersion: 1,
        title: "x".repeat(MAX_THREAD_TITLE_BYTES + 1),
        specContent: specContentFixture(),
        idempotencyKey: IDEMPOTENCY_KEY,
      }),
    ).toThrow(/Thread\.title bytes/);

    expect(() =>
      threadCreateRequestFromJson({
        schemaVersion: 1,
        title: "x",
        specContent: "x".repeat(MAX_THREAD_SPEC_CONTENT_BYTES + 1),
        idempotencyKey: IDEMPOTENCY_KEY,
      }),
    ).toThrow(/ThreadSpecRevision\.content bytes/);

    expect(() =>
      threadListResponseFromJson({
        schemaVersion: 1,
        threads: Array.from({ length: MAX_ROUTE_THREAD_LIST_ITEMS + 1 }, () =>
          threadViewToJsonValue(threadViewFixture()),
        ),
      }),
    ).toThrow(/MAX_ROUTE_THREAD_LIST_ITEMS/);

    expect(() =>
      threadListResponseFromJson({
        schemaVersion: 1,
        threads: [threadViewToJsonValue(threadViewFixture())],
        nextCursor: "x".repeat(MAX_ROUTE_CURSOR_BYTES + 1),
      }),
    ).toThrow(/RouteListResponse\.nextCursor bytes/);

    expect(() =>
      approvalListResponseFromJson({
        schemaVersion: 1,
        approvals: Array.from({ length: MAX_ROUTE_APPROVAL_LIST_ITEMS + 1 }, () =>
          approvalViewToJsonValue(approvalViewFixture()),
        ),
      }),
    ).toThrow(/MAX_ROUTE_APPROVAL_LIST_ITEMS/);

    expect(() =>
      threadViewFromJson({
        ...threadViewToJsonValue(threadViewFixture()),
        pendingApprovalCount: -1,
      }),
    ).toThrow(/pendingApprovalCount/);

    expect(() =>
      routeErrorFromJson({
        error: "store_busy",
        message: "x".repeat(MAX_ROUTE_ERROR_MESSAGE_BYTES + 1),
      }),
    ).toThrow(/RouteError\.message bytes/);
  });

  it("keeps exported route budget helpers covered", () => {
    expect(validateRouteCursorBudget("x".repeat(MAX_ROUTE_CURSOR_BYTES)).ok).toBe(true);
    expect(validateRouteCursorBudget("x".repeat(MAX_ROUTE_CURSOR_BYTES + 1)).ok).toBe(false);
    expect(validateRouteErrorCodeBudget("x".repeat(MAX_ROUTE_ERROR_CODE_BYTES)).ok).toBe(true);
    expect(validateRouteErrorCodeBudget("x".repeat(MAX_ROUTE_ERROR_CODE_BYTES + 1)).ok).toBe(false);
    expect(validateRouteErrorMessageBudget("x".repeat(MAX_ROUTE_ERROR_MESSAGE_BYTES)).ok).toBe(
      true,
    );
    expect(validateRouteErrorMessageBudget("x".repeat(MAX_ROUTE_ERROR_MESSAGE_BYTES + 1)).ok).toBe(
      false,
    );
  });

  it("keeps thread view enum value arrays closed and exported", () => {
    expect(THREAD_EFFECTIVE_STATUS_VALUES).toEqual([
      "open",
      "in_progress",
      "needs_review",
      "needs_attention",
      "merged",
      "closed",
    ]);
    expect(THREAD_ATTENTION_REASON_VALUES).toEqual(["pending_approval", "failed", "stalled"]);
    expect(THREAD_BOARD_COLUMN_VALUES).toEqual(["running", "review", "needs_me", "done"]);
    expect(THREAD_CURRENT_SEAT_VALUES).toEqual(["agent", "human"]);
  });

  it("rejects unsupported schema versions and approval invariant drift", () => {
    expect(() =>
      threadSpecEditRequestFromJson({
        schemaVersion: 2,
        baseRevisionId: REVISION_1,
        baseContentHash: threadSpecContentHash(specContentFixture()),
        content: specContentFixture(),
        idempotencyKey: IDEMPOTENCY_KEY,
      }),
    ).toThrow(/unsupported schemaVersion/);

    expect(() =>
      approvalDecisionRequestFromJson({
        schemaVersion: 1,
        decision: "approve",
        idempotencyKey: IDEMPOTENCY_KEY,
      }),
    ).toThrow(/token.*required/);

    const claim = receiptCoSignClaimFixture();
    expect(() =>
      approvalRequestCreateRequestFromJson({
        schemaVersion: 1,
        claim,
        scope: receiptCoSignScopeFixture(claim),
        riskClass: "high",
        receiptId: OTHER_RECEIPT_ID,
        idempotencyKey: IDEMPOTENCY_KEY,
      }),
    ).toThrow(/receiptId.*must match claim\.receiptId/);

    const approvedViewJson = approvalViewToJsonValue(approvalViewFixture()) as JsonObject & {
      readonly decisionSummary?: unknown;
    };
    const withoutDecisionSummary = { ...approvedViewJson };
    Reflect.deleteProperty(withoutDecisionSummary, "decisionSummary");
    expect(() => approvalViewFromJson(withoutDecisionSummary)).toThrow(/decisionSummary.*required/);
    expect(() => approvalViewFromJson({ ...approvedViewJson, status: "pending" })).toThrow(
      /decisionSummary.*absent/,
    );
    expect(() => approvalViewFromJson({ ...approvedViewJson, status: "rejected" })).toThrow(
      /decisionSummary\/decision.*must match status/,
    );
    expect(() =>
      approvalViewFromJson({
        ...approvedViewJson,
        decisionSummary: {
          ...(approvedViewJson.decisionSummary as JsonObject),
          token: signedApprovalTokenFixture(),
        },
      }),
    ).toThrow(/decisionSummary\/token.*not allowed/);

    expect(() =>
      threadViewFromJson({
        ...threadViewToJsonValue(
          threadViewFixture({
            effectiveStatus: "open",
            attentionReason: undefined,
            boardColumn: "running",
            currentSeat: "agent",
            pendingApprovalCount: 0,
          }),
        ),
        attentionReason: "pending_approval",
      }),
    ).toThrow(/attentionReason.*absent/);
  });
});

describe("route-envelope conformance vectors", () => {
  it("covers every public route-envelope codec family", () => {
    const acceptedCodecs = new Set(routeEnvelopeVectors.accepted.map((vector) => vector.codec));
    const rejectedNames = new Set(routeEnvelopeVectors.rejected.map((vector) => vector.name));

    expect(routeEnvelopeVectors.schemaVersion).toBe(1);
    expect(acceptedCodecs).toEqual(
      new Set([
        "threadCreateRequest",
        "threadSpecEditRequest",
        "threadStatusChangeRequest",
        "threadMutationResponse",
        "threadListResponse",
        "threadGetResponse",
        "approvalRequestCreateRequest",
        "approvalDecisionRequest",
        "approvalRequestCreateResponse",
        "approvalDecisionResponse",
        "approvalView",
        "approvalListResponse",
        "approvalGetResponse",
        "threadPinnedApprovalsResponse",
        "routeError",
      ]),
    );
    expect(rejectedNames).toContain("thread create request unknown key");
    expect(rejectedNames).toContain("route error message exceeds budget");
    expect(rejectedNames).toContain("approval decision approve missing token");
  });

  for (const vector of routeEnvelopeVectors.accepted) {
    it(`accepts ${vector.name}`, () => {
      const serialized = routeEnvelopeCanonicalSerialization(vector.codec, vector.input);
      expect(serialized).toBe(vector.expected.canonicalSerialization);
    });
  }

  for (const vector of routeEnvelopeVectors.rejected) {
    it(`rejects ${vector.name}`, () => {
      const message = captureErrorMessage(() =>
        routeEnvelopeCanonicalSerialization(vector.codec, vector.input),
      );
      expect(message).toContain(vector.expectedError);
    });
  }
});

interface RouteEnvelopeAcceptedVector {
  readonly name: string;
  readonly codec: RouteEnvelopeCodec;
  readonly input: unknown;
  readonly expected: {
    readonly canonicalSerialization: string;
  };
}

interface RouteEnvelopeRejectedVector {
  readonly name: string;
  readonly codec: RouteEnvelopeCodec;
  readonly input: unknown;
  readonly expectedError: string;
}

interface RouteEnvelopeVectorsFixture {
  readonly schemaVersion: 1;
  readonly comment: string;
  readonly accepted: readonly RouteEnvelopeAcceptedVector[];
  readonly rejected: readonly RouteEnvelopeRejectedVector[];
}

type RouteEnvelopeCodec =
  | "threadCreateRequest"
  | "threadSpecEditRequest"
  | "threadStatusChangeRequest"
  | "threadMutationResponse"
  | "threadListResponse"
  | "threadGetResponse"
  | "approvalRequestCreateRequest"
  | "approvalDecisionRequest"
  | "approvalRequestCreateResponse"
  | "approvalDecisionResponse"
  | "approvalView"
  | "approvalListResponse"
  | "approvalGetResponse"
  | "threadPinnedApprovalsResponse"
  | "routeError";

type JsonObject = Record<string, unknown>;
type FixtureOverrides = Partial<
  Omit<
    ApprovalRequest,
    "id" | "claim" | "scope" | "riskClass" | "requestedBy" | "requestedAt" | "schemaVersion"
  >
> & {
  readonly claim?: ApprovalClaim | undefined;
  readonly scope?: ApprovalScope | undefined;
};
type ApprovalViewFixtureOverrides = Partial<
  Omit<
    ApprovalView,
    "id" | "claim" | "scope" | "riskClass" | "requestedBy" | "requestedAt" | "schemaVersion"
  >
> & {
  readonly claim?: ApprovalClaim | undefined;
  readonly scope?: ApprovalScope | undefined;
};

function roundTrip<T>(
  value: T,
  toJsonValue: (value: T) => Readonly<Record<string, unknown>>,
  fromJson: (value: unknown) => T,
): T {
  return fromJson(JSON.parse(canonicalJSON(toJsonValue(value))) as unknown);
}

function routeEnvelopeCanonicalSerialization(codec: RouteEnvelopeCodec, input: unknown): string {
  switch (codec) {
    case "threadCreateRequest":
      return canonicalJSON(threadCreateRequestToJsonValue(threadCreateRequestFromJson(input)));
    case "threadSpecEditRequest":
      return canonicalJSON(threadSpecEditRequestToJsonValue(threadSpecEditRequestFromJson(input)));
    case "threadStatusChangeRequest":
      return canonicalJSON(
        threadStatusChangeRequestToJsonValue(threadStatusChangeRequestFromJson(input)),
      );
    case "threadMutationResponse":
      return canonicalJSON(
        threadMutationResponseToJsonValue(threadMutationResponseFromJson(input)),
      );
    case "threadListResponse":
      return canonicalJSON(threadListResponseToJsonValue(threadListResponseFromJson(input)));
    case "threadGetResponse":
      return canonicalJSON(threadGetResponseToJsonValue(threadGetResponseFromJson(input)));
    case "approvalRequestCreateRequest":
      return canonicalJSON(
        approvalRequestCreateRequestToJsonValue(approvalRequestCreateRequestFromJson(input)),
      );
    case "approvalDecisionRequest":
      return canonicalJSON(
        approvalDecisionRequestToJsonValue(approvalDecisionRequestFromJson(input)),
      );
    case "approvalRequestCreateResponse":
      return canonicalJSON(
        approvalRequestCreateResponseToJsonValue(approvalRequestCreateResponseFromJson(input)),
      );
    case "approvalDecisionResponse":
      return canonicalJSON(
        approvalDecisionResponseToJsonValue(approvalDecisionResponseFromJson(input)),
      );
    case "approvalView":
      return canonicalJSON(approvalViewToJsonValue(approvalViewFromJson(input)));
    case "approvalListResponse":
      return canonicalJSON(approvalListResponseToJsonValue(approvalListResponseFromJson(input)));
    case "approvalGetResponse":
      return canonicalJSON(approvalGetResponseToJsonValue(approvalGetResponseFromJson(input)));
    case "threadPinnedApprovalsResponse":
      return canonicalJSON(
        threadPinnedApprovalsResponseToJsonValue(threadPinnedApprovalsResponseFromJson(input)),
      );
    case "routeError":
      return canonicalJSON(routeErrorToJsonValue(routeErrorFromJson(input)));
  }
}

function strictKnownKeyCases(): readonly {
  readonly name: string;
  readonly input: JsonObject;
  readonly parse: (value: unknown) => unknown;
}[] {
  const thread = threadViewFixture();
  const claim = receiptCoSignClaimFixture();
  const approval = approvalRequestFixture();
  const view = approvalViewFixture();
  return [
    {
      name: "threadCreateRequest",
      input: threadCreateRequestToJsonValue({
        schemaVersion: 1,
        title: "Approval request protocol",
        specContent: specContentFixture(),
        idempotencyKey: IDEMPOTENCY_KEY,
      }) as JsonObject,
      parse: threadCreateRequestFromJson,
    },
    {
      name: "threadSpecEditRequest",
      input: threadSpecEditRequestToJsonValue({
        schemaVersion: 1,
        baseRevisionId: REVISION_1,
        baseContentHash: threadSpecContentHash(specContentFixture()),
        content: specContentFixture(),
        idempotencyKey: IDEMPOTENCY_KEY,
      }) as JsonObject,
      parse: threadSpecEditRequestFromJson,
    },
    {
      name: "threadStatusChangeRequest",
      input: threadStatusChangeRequestToJsonValue({
        schemaVersion: 1,
        fromStatus: "open",
        toStatus: "in_progress",
        idempotencyKey: IDEMPOTENCY_KEY,
      }) as JsonObject,
      parse: threadStatusChangeRequestFromJson,
    },
    {
      name: "threadMutationResponse",
      input: threadMutationResponseToJsonValue({
        schemaVersion: 1,
        threadId: THREAD_ID,
        headLsn: HEAD_LSN,
        revisionId: REVISION_1,
        contentHash: thread.spec.contentHash,
      }) as JsonObject,
      parse: threadMutationResponseFromJson,
    },
    {
      name: "threadView",
      input: threadViewToJsonValue(thread) as JsonObject,
      parse: threadViewFromJson,
    },
    {
      name: "threadListResponse",
      input: threadListResponseToJsonValue({ schemaVersion: 1, threads: [thread] }) as JsonObject,
      parse: threadListResponseFromJson,
    },
    {
      name: "threadGetResponse",
      input: threadGetResponseToJsonValue({ schemaVersion: 1, thread }) as JsonObject,
      parse: threadGetResponseFromJson,
    },
    {
      name: "approvalRequestCreateRequest",
      input: approvalRequestCreateRequestToJsonValue({
        schemaVersion: 1,
        claim,
        scope: receiptCoSignScopeFixture(claim),
        riskClass: "high",
        receiptId: RECEIPT_ID,
        idempotencyKey: IDEMPOTENCY_KEY,
      }) as JsonObject,
      parse: approvalRequestCreateRequestFromJson,
    },
    {
      name: "approvalDecisionRequest",
      input: approvalDecisionRequestToJsonValue({
        schemaVersion: 1,
        decision: "approve",
        token: signedApprovalTokenFixture(),
        idempotencyKey: IDEMPOTENCY_KEY,
      }) as JsonObject,
      parse: approvalDecisionRequestFromJson,
    },
    {
      name: "approvalRequestCreateResponse",
      input: approvalRequestCreateResponseToJsonValue({
        schemaVersion: 1,
        approvalRequest: approval,
        headLsn: HEAD_LSN,
      }) as JsonObject,
      parse: approvalRequestCreateResponseFromJson,
    },
    {
      name: "approvalDecisionResponse",
      input: approvalDecisionResponseToJsonValue({
        schemaVersion: 1,
        approvalRequest: approval,
        headLsn: HEAD_LSN,
      }) as JsonObject,
      parse: approvalDecisionResponseFromJson,
    },
    {
      name: "approvalView",
      input: approvalViewToJsonValue(view) as JsonObject,
      parse: approvalViewFromJson,
    },
    {
      name: "approvalListResponse",
      input: approvalListResponseToJsonValue({
        schemaVersion: 1,
        approvals: [view],
      }) as JsonObject,
      parse: approvalListResponseFromJson,
    },
    {
      name: "approvalGetResponse",
      input: approvalGetResponseToJsonValue({
        schemaVersion: 1,
        approval: view,
      }) as JsonObject,
      parse: approvalGetResponseFromJson,
    },
    {
      name: "threadPinnedApprovalsResponse",
      input: threadPinnedApprovalsResponseToJsonValue({
        schemaVersion: 1,
        threadId: THREAD_ID,
        headLsn: HEAD_LSN,
        approvals: [view],
      }) as JsonObject,
      parse: threadPinnedApprovalsResponseFromJson,
    },
    {
      name: "routeError",
      input: routeErrorToJsonValue({ error: "store_busy" }) as JsonObject,
      parse: routeErrorFromJson,
    },
  ];
}

function specContentFixture() {
  return {
    body: "Implement route envelope codecs",
    checklist: ["codecs", "vectors", "docs"],
  };
}

function externalRefsFixture() {
  return {
    sourceUrls: ["https://example.test/wuphf/914"],
    entityIds: ["issue:914"],
  };
}

function threadFixture(): Thread {
  const content = specContentFixture();
  return {
    id: THREAD_ID,
    title: "Approval request protocol",
    status: "open",
    spec: {
      revisionId: REVISION_1,
      threadId: THREAD_ID,
      content,
      contentHash: threadSpecContentHash(content),
      authoredBy: SIGNER,
      authoredAt: CREATED_AT,
    },
    externalRefs: externalRefsFixture(),
    taskIds: [TASK_ID],
    createdBy: SIGNER,
    createdAt: CREATED_AT,
    updatedAt: UPDATED_AT,
  };
}

function threadViewFixture(overrides: Partial<ThreadView> = {}): ThreadView {
  return {
    ...threadFixture(),
    effectiveStatus: "needs_attention",
    attentionReason: "pending_approval",
    boardColumn: "needs_me",
    currentSeat: "human",
    pendingApprovalCount: 1,
    ...overrides,
  };
}

function approvalRequestFixture(overrides: FixtureOverrides = {}): ApprovalRequest {
  const claim = overrides.claim ?? receiptCoSignClaimFixture();
  const scope = overrides.scope ?? receiptCoSignScopeFor(claim);
  const decision = Object.hasOwn(overrides, "decision")
    ? overrides.decision
    : decisionRecordFixture();
  return {
    id: REQUEST_ID,
    claim,
    scope,
    riskClass: "high",
    threadId: THREAD_ID,
    taskId: TASK_ID,
    receiptId: RECEIPT_ID,
    requestedBy: SIGNER,
    requestedAt: CREATED_AT,
    status: overrides.status ?? "approved",
    ...(decision === undefined ? {} : { decision }),
    schemaVersion: 1,
  };
}

function approvalViewFixture(overrides: ApprovalViewFixtureOverrides = {}): ApprovalView {
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
    requestedBy: SIGNER,
    requestedAt: CREATED_AT,
    status: overrides.status ?? "approved",
    ...(decisionSummary === undefined ? {} : { decisionSummary }),
    schemaVersion: 1,
  };
}

function receiptCoSignScopeFor(claim: ApprovalClaim): ApprovalScope {
  if (claim.kind !== "receipt_co_sign") {
    throw new Error("approvalRequestFixture requires scope when overriding non-receipt claims");
  }
  return receiptCoSignScopeFixture(claim);
}

function decisionRecordFixture() {
  return {
    decision: "approve" as const,
    decidedBy: DECIDER,
    decidedAt: DECIDED_AT,
    token: signedApprovalTokenFixture(),
  };
}

function decisionSummaryFixture() {
  return {
    decision: "approve" as const,
    decidedBy: DECIDER,
    decidedAt: DECIDED_AT,
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

function webAuthnAssertionFixture(): WebAuthnAssertion {
  return {
    credentialId: "Y3JlZGVudGlhbC0wMQ",
    authenticatorData: "YXV0aGVudGljYXRvci1kYXRh",
    clientDataJson: "Y2xpZW50LWRhdGEtanNvbg",
    signature: "c2lnbmF0dXJl",
    userHandle: "dXNlci0wMQ",
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
