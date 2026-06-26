// Approval audit trail API helpers. The Inbox right-pane renders an
// "answered → executed → outcome" trail underneath each answered approval
// request, sourcing rows from these endpoints.
//
// The broker writes one entry per terminal disposition (executed_ok,
// executed_failed, rejected, timed_out, cancelled). The FE queries by
// request_id when rendering a single inbox row, and by task_id when
// rendering an Issue overview that may aggregate multiple approvals.

import { get } from "./client";

export type ApprovalOutcome =
  | "executed_ok"
  | "executed_failed"
  | "rejected"
  | "timed_out"
  | "cancelled";

export interface ApprovalAuditEntry {
  approval_request_id: string;
  task_id?: string;
  platform?: string;
  action_id?: string;
  connection_key?: string;
  requested_at?: string;
  answered_at?: string;
  executed_at?: string;
  outcome?: ApprovalOutcome | string;
  outcome_summary?: string;
  outcome_chat_message_id?: string;
  actor?: string;
  channel?: string;
  created_at: string;
}

interface ApprovalAuditResponse {
  entries: ApprovalAuditEntry[];
}

export async function getApprovalAuditByRequest(
  requestId: string,
): Promise<ApprovalAuditEntry[]> {
  if (!requestId) return [];
  const resp = await get<ApprovalAuditResponse>("/approval-audit", {
    request_id: requestId,
  });
  return resp.entries ?? [];
}

export async function getApprovalAuditByTask(
  taskId: string,
): Promise<ApprovalAuditEntry[]> {
  if (!taskId) return [];
  const resp = await get<ApprovalAuditResponse>("/approval-audit", {
    task_id: taskId,
  });
  return resp.entries ?? [];
}
