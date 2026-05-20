import type { ApprovalRequest, ApprovalView } from "@wuphf/protocol";

export function approvalViewFromApproval(approval: ApprovalRequest): ApprovalView {
  return {
    id: approval.id,
    claim: approval.claim,
    scope: approval.scope,
    riskClass: approval.riskClass,
    ...(approval.threadId === undefined ? {} : { threadId: approval.threadId }),
    ...(approval.taskId === undefined ? {} : { taskId: approval.taskId }),
    ...(approval.receiptId === undefined ? {} : { receiptId: approval.receiptId }),
    requestedBy: approval.requestedBy,
    requestedAt: approval.requestedAt,
    status: approval.status,
    ...(approval.decision === undefined
      ? {}
      : {
          decisionSummary: {
            decision: approval.decision.decision,
            decidedBy: approval.decision.decidedBy,
            decidedAt: approval.decision.decidedAt,
          },
        }),
    schemaVersion: 1,
  };
}
