import { getApproval, listApprovals } from "../api/approvals.ts";
import type { BrokerApiClient } from "../api/client.ts";
import { getThread, getThreadPinnedApprovals, listThreads } from "../api/threads.ts";

export const threadQueryKeys = {
  all: ["threads"] as const,
  list: () => [...threadQueryKeys.all, "list"] as const,
  detail: (threadId: string) => [...threadQueryKeys.all, "detail", threadId] as const,
  pinnedApprovals: (threadId: string) =>
    [...threadQueryKeys.detail(threadId), "pinned-approvals"] as const,
};

export const approvalQueryKeys = {
  all: ["approvals"] as const,
  list: () => [...approvalQueryKeys.all, "list"] as const,
  detail: (approvalId: string) => [...approvalQueryKeys.all, "detail", approvalId] as const,
};

export function threadListQuery(client: BrokerApiClient) {
  return {
    queryKey: threadQueryKeys.list(),
    queryFn: () => listThreads(client),
  };
}

export function threadDetailQuery(client: BrokerApiClient, threadId: string) {
  return {
    queryKey: threadQueryKeys.detail(threadId),
    queryFn: () => getThread(client, threadId),
  };
}

export function threadPinnedApprovalsQuery(client: BrokerApiClient, threadId: string) {
  return {
    queryKey: threadQueryKeys.pinnedApprovals(threadId),
    queryFn: () => getThreadPinnedApprovals(client, threadId),
  };
}

export function approvalListQuery(client: BrokerApiClient) {
  return {
    queryKey: approvalQueryKeys.list(),
    queryFn: () => listApprovals(client),
  };
}

export function approvalDetailQuery(client: BrokerApiClient, approvalId: string) {
  return {
    queryKey: approvalQueryKeys.detail(approvalId),
    queryFn: () => getApproval(client, approvalId),
  };
}
