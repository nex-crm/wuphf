import type {
  ThreadGetResponse,
  ThreadListResponse,
  ThreadPinnedApprovalsResponse,
} from "@wuphf/protocol/browser";
import {
  threadGetResponseFromJson,
  threadListResponseFromJson,
  threadPinnedApprovalsResponseFromJson,
} from "@wuphf/protocol/browser";

import type { BrokerApiClient } from "./client.ts";

export function listThreads(client: BrokerApiClient): Promise<ThreadListResponse> {
  return client.getJson("/api/v1/threads", threadListResponseFromJson);
}

export function getThread(client: BrokerApiClient, threadId: string): Promise<ThreadGetResponse> {
  return client.getJson(
    `/api/v1/threads/${encodeURIComponent(threadId)}`,
    threadGetResponseFromJson,
  );
}

export function getThreadPinnedApprovals(
  client: BrokerApiClient,
  threadId: string,
): Promise<ThreadPinnedApprovalsResponse> {
  return client.getJson(
    `/api/v1/threads/${encodeURIComponent(threadId)}/pinned-approvals`,
    threadPinnedApprovalsResponseFromJson,
  );
}
