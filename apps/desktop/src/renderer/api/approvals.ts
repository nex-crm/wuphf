import type {
  ApprovalGetResponse,
  ApprovalListResponse,
  ApprovalRequestId,
} from "@wuphf/protocol/browser";
import { approvalGetResponseFromJson, approvalListResponseFromJson } from "@wuphf/protocol/browser";

import type { BrokerApiClient } from "./client.ts";

export function listApprovals(client: BrokerApiClient): Promise<ApprovalListResponse> {
  return client.getJson("/api/v1/approvals", approvalListResponseFromJson);
}

export function getApproval(
  client: BrokerApiClient,
  approvalId: ApprovalRequestId,
): Promise<ApprovalGetResponse> {
  return client.getJson(
    `/api/v1/approvals/${encodeURIComponent(approvalId)}`,
    approvalGetResponseFromJson,
  );
}
