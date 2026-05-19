import {
  type ApprovalRequest,
  approvalDecisionRequestFromJson,
  approvalDecisionRequestToJsonValue,
  approvalDecisionResponseFromJson,
  approvalDecisionResponseToJsonValue,
  approvalRequestCreateRequestFromJson,
  approvalRequestCreateRequestToJsonValue,
  approvalRequestCreateResponseFromJson,
  approvalRequestCreateResponseToJsonValue,
  approvalRequestFromJsonValue,
  approvalRequestToJson,
  approvalRequestToJsonValue,
  asApprovalRequestId,
  asIdempotencyKey,
  asSignerIdentity,
  asThreadId,
  asThreadSpecRevisionId,
  canonicalJSON,
  lsnFromV1Number,
  routeErrorFromJson,
  routeErrorToJsonValue,
  signedApprovalTokenToJsonValue,
  type Thread,
  threadCreateRequestFromJson,
  threadCreateRequestToJsonValue,
  threadGetResponseFromJson,
  threadGetResponseToJsonValue,
  threadListResponseFromJson,
  threadListResponseToJsonValue,
  threadMutationResponseFromJson,
  threadMutationResponseToJsonValue,
  threadSpecContentHash,
  threadSpecEditRequestFromJson,
  threadSpecEditRequestToJsonValue,
  threadStatusChangeRequestFromJson,
  threadStatusChangeRequestToJsonValue,
  threadToJson,
} from "../../src/index.ts";
import { buildValidReceipt } from "./fixtures.ts";
import { expectCodecRoundTrip, expectEqual, expectThrows, header, nonNull } from "./harness.ts";

export function runApprovalRouteScenarios(): void {
  header(32, "ApprovalRequest artifact folds pending and decided approval state");
  const approvalReceipt = buildValidReceipt();
  const approvalEvidence = nonNull(approvalReceipt.approvals[0], "approvalReceipt.approvals[0]");
  const approvalRequest: ApprovalRequest = {
    id: asApprovalRequestId("01HRQ7KZ7D4E6F8G9H0J1K2M3N"),
    claim: approvalEvidence.signedToken.claim,
    scope: approvalEvidence.signedToken.scope,
    riskClass: "high",
    threadId: asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY"),
    taskId: approvalReceipt.taskId,
    receiptId: approvalReceipt.id,
    requestedBy: asSignerIdentity("fran@example.com"),
    requestedAt: new Date("2026-05-08T18:00:00.000Z"),
    status: "approved",
    decision: {
      decision: "approve",
      decidedBy: asSignerIdentity("approver@example.com"),
      decidedAt: new Date("2026-05-08T18:05:00.000Z"),
      token: approvalEvidence.signedToken,
    },
    schemaVersion: 1,
  };
  const approvalRequestWire = approvalRequestToJsonValue(approvalRequest);
  expectEqual(
    "approval request round-trips through canonical JSON",
    canonicalJSON(approvalRequestToJsonValue(approvalRequestFromJsonValue(approvalRequestWire))),
    canonicalJSON(approvalRequestWire),
  );
  expectEqual(
    "approval request JSON stays canonical",
    approvalRequestToJson(approvalRequest),
    canonicalJSON(approvalRequestWire),
  );
  expectThrows(
    () => approvalRequestFromJsonValue({ ...approvalRequestWire, shadow: true }),
    /shadow.*not allowed/,
  );
  expectThrows(
    () =>
      approvalRequestFromJsonValue({
        ...approvalRequestWire,
        request_id: "A".repeat(27),
      }),
    /ApprovalRequestId bytes/,
  );
  const approvedWithoutDecision = { ...approvalRequestWire };
  Reflect.deleteProperty(approvedWithoutDecision, "decision");
  expectThrows(() => approvalRequestFromJsonValue(approvedWithoutDecision), /decision.*required/);
  expectThrows(
    () => approvalRequestFromJsonValue({ ...approvalRequestWire, status: "pending" }),
    /decision.*absent/,
  );
  expectThrows(
    () =>
      approvalRequestFromJsonValue({
        ...approvalRequestWire,
        decision: {
          decision: "approve",
          decided_by: "approver@example.com",
          decided_at: "2026-05-08T18:05:00.000Z",
        },
      }),
    /token.*required/,
  );
  const tokenBoundToDifferentClaim = JSON.parse(canonicalJSON(approvalRequestWire)) as {
    decision: {
      token: {
        claim: Record<string, unknown>;
        scope: Record<string, unknown>;
      };
    };
  };
  tokenBoundToDifferentClaim.decision.token.claim.claimId = "claim_demo_receipt_cosign_02";
  tokenBoundToDifferentClaim.decision.token.scope.claimId = "claim_demo_receipt_cosign_02";
  expectThrows(
    () => approvalRequestFromJsonValue(tokenBoundToDifferentClaim),
    /token.*claim.*must match request claim/,
  );
  expectThrows(
    () =>
      approvalRequestFromJsonValue({
        ...approvalRequestWire,
        receipt_id: "01ARZ3NDEKTSV4RRFFQ69G5FAZ",
      }),
    /receiptId.*must match claim\.receiptId/,
  );

  header(33, "Route-envelope codecs own thread and approval HTTP bodies");
  const routeThreadId = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
  const routeRevision1 = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA0");
  const routeRevision2 = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA1");
  const routeCreatedAt = new Date("2026-05-08T18:00:00.000Z");
  const routeUpdatedAt = new Date("2026-05-08T18:05:00.000Z");
  const routeSpecContent = {
    body: "Implement route envelope codecs",
    checklist: ["codecs", "vectors", "demo"],
  };
  const routeEditedContent = { body: "Edited", checklist: ["tests", "vectors"] };
  const routeThread: Thread = {
    id: routeThreadId,
    title: "Approval request protocol",
    status: "open",
    spec: {
      revisionId: routeRevision1,
      threadId: routeThreadId,
      content: routeSpecContent,
      contentHash: threadSpecContentHash(routeSpecContent),
      authoredBy: asSignerIdentity("fran@example.com"),
      authoredAt: routeCreatedAt,
    },
    externalRefs: {
      sourceUrls: ["https://example.test/wuphf/914"],
      entityIds: ["issue:914"],
    },
    taskIds: [approvalReceipt.taskId],
    createdBy: asSignerIdentity("fran@example.com"),
    createdAt: routeCreatedAt,
    updatedAt: routeUpdatedAt,
  };
  const routeThreadJson = JSON.parse(threadToJson(routeThread));
  const pendingApprovalRequestWire = { ...approvalRequestWire, status: "pending" };
  Reflect.deleteProperty(pendingApprovalRequestWire, "decision");
  const pendingApprovalRequest = approvalRequestFromJsonValue(pendingApprovalRequestWire);

  expectCodecRoundTrip(
    "thread create request route envelope",
    {
      title: routeThread.title,
      specContent: routeSpecContent,
      externalRefs: { source_urls: routeThread.externalRefs.sourceUrls, entity_ids: [] },
      idempotencyKey: asIdempotencyKey("route-envelope-demo-01"),
    },
    threadCreateRequestFromJson,
    threadCreateRequestToJsonValue,
  );
  expectCodecRoundTrip(
    "thread spec edit request route envelope",
    {
      schemaVersion: 1,
      baseRevisionId: routeRevision1,
      baseContentHash: routeThread.spec.contentHash,
      content: routeEditedContent,
      idempotencyKey: asIdempotencyKey("route-envelope-demo-02"),
    },
    threadSpecEditRequestFromJson,
    threadSpecEditRequestToJsonValue,
  );
  expectCodecRoundTrip(
    "thread status change request route envelope",
    {
      schemaVersion: 1,
      fromStatus: "open",
      toStatus: "in_progress",
      idempotencyKey: asIdempotencyKey("route-envelope-demo-03"),
    },
    threadStatusChangeRequestFromJson,
    threadStatusChangeRequestToJsonValue,
  );
  expectCodecRoundTrip(
    "thread mutation response route envelope",
    {
      schemaVersion: 1,
      threadId: routeThreadId,
      headLsn: lsnFromV1Number(42),
      revisionId: routeRevision2,
      contentHash: threadSpecContentHash(routeEditedContent),
    },
    threadMutationResponseFromJson,
    threadMutationResponseToJsonValue,
  );
  expectCodecRoundTrip(
    "thread list response route envelope",
    {
      schemaVersion: 1,
      threads: [routeThreadJson],
      nextCursor: "bHNuOjQy",
    },
    threadListResponseFromJson,
    threadListResponseToJsonValue,
  );
  expectCodecRoundTrip(
    "thread get response route envelope",
    {
      schemaVersion: 1,
      thread: routeThreadJson,
    },
    threadGetResponseFromJson,
    threadGetResponseToJsonValue,
  );
  expectCodecRoundTrip(
    "approval request create route envelope",
    {
      schemaVersion: 1,
      claim: approvalEvidence.signedToken.claim,
      scope: approvalEvidence.signedToken.scope,
      riskClass: "high",
      threadId: routeThreadId,
      taskId: approvalReceipt.taskId,
      receiptId: approvalReceipt.id,
      idempotencyKey: asIdempotencyKey("route-envelope-demo-04"),
    },
    approvalRequestCreateRequestFromJson,
    approvalRequestCreateRequestToJsonValue,
  );
  expectCodecRoundTrip(
    "approval decision request route envelope",
    {
      schemaVersion: 1,
      decision: "approve",
      token: signedApprovalTokenToJsonValue(approvalEvidence.signedToken),
      idempotencyKey: asIdempotencyKey("route-envelope-demo-05"),
    },
    approvalDecisionRequestFromJson,
    approvalDecisionRequestToJsonValue,
  );
  expectCodecRoundTrip(
    "approval request create response route envelope",
    {
      schemaVersion: 1,
      approvalRequest: approvalRequestToJsonValue(pendingApprovalRequest),
      headLsn: lsnFromV1Number(42),
    },
    approvalRequestCreateResponseFromJson,
    approvalRequestCreateResponseToJsonValue,
  );
  expectCodecRoundTrip(
    "approval decision response route envelope",
    {
      schemaVersion: 1,
      approvalRequest: approvalRequestWire,
      headLsn: lsnFromV1Number(43),
    },
    approvalDecisionResponseFromJson,
    approvalDecisionResponseToJsonValue,
  );
  expectCodecRoundTrip(
    "route error envelope",
    {
      error: "store_busy",
      message: "The projection store is temporarily busy.",
      retryAfterMs: 1000,
    },
    routeErrorFromJson,
    routeErrorToJsonValue,
  );
}
