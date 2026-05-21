import { asApprovalRequestId, asThreadId } from "@wuphf/protocol/browser";
import { describe, expect, it, vi } from "vitest";

import type { BrokerApiClient } from "../../src/renderer/api/client.ts";
import {
  approvalDetailQuery,
  approvalListQuery,
  approvalQueryKeys,
  threadDetailQuery,
  threadListQuery,
  threadPinnedApprovalsQuery,
  threadQueryKeys,
} from "../../src/renderer/query/queries.ts";
import { createDesktopQueryClient } from "../../src/renderer/query/queryClient.ts";

describe("query helpers", () => {
  const threadId = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
  const approvalId = asApprovalRequestId("01CRZ3NDEKTSV4RRFFQ69G5FAV");

  it("builds stable thread and approval keys", () => {
    expect(threadQueryKeys.list()).toEqual(["threads", "list"]);
    expect(threadQueryKeys.detail(threadId)).toEqual(["threads", "detail", threadId]);
    expect(threadQueryKeys.pinnedApprovals(threadId)).toEqual([
      "threads",
      "detail",
      threadId,
      "pinned-approvals",
    ]);
    expect(approvalQueryKeys.list()).toEqual(["approvals", "list"]);
    expect(approvalQueryKeys.detail(approvalId)).toEqual(["approvals", "detail", approvalId]);
  });

  it("binds query functions to the broker client", async () => {
    const getJson = vi.fn<(path: string) => void>();
    const client: BrokerApiClient = {
      async getJson<T>(path: string): Promise<T> {
        getJson(path);
        return { ok: true } as T;
      },
      async postJson<TReq, TRes>(
        _path: string,
        _request: TReq,
        _requestToJsonValue: (value: TReq) => unknown,
      ): Promise<TRes> {
        return { ok: true } as TRes;
      },
    };

    await threadListQuery(client).queryFn();
    await threadDetailQuery(client, threadId).queryFn();
    await threadPinnedApprovalsQuery(client, threadId).queryFn();
    await approvalListQuery(client).queryFn();
    await approvalDetailQuery(client, approvalId).queryFn();

    expect(getJson).toHaveBeenCalledWith("/api/v1/threads");
    expect(getJson).toHaveBeenCalledWith(`/api/v1/threads/${threadId}`);
    expect(getJson).toHaveBeenCalledWith(`/api/v1/threads/${threadId}/pinned-approvals`);
    expect(getJson).toHaveBeenCalledWith("/api/v1/approvals");
    expect(getJson).toHaveBeenCalledWith(`/api/v1/approvals/${approvalId}`);
  });

  it("uses the desktop query defaults", () => {
    const queryClient = createDesktopQueryClient();

    expect(queryClient.getDefaultOptions().queries?.staleTime).toBe(2_000);
    expect(queryClient.getDefaultOptions().queries?.retry).toBe(1);
    expect(queryClient.getDefaultOptions().queries?.refetchOnReconnect).toBe(false);
    expect(queryClient.getDefaultOptions().queries?.refetchOnWindowFocus).toBe(false);
  });
});
