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
  it("builds stable thread and approval keys", () => {
    expect(threadQueryKeys.list()).toEqual(["threads", "list"]);
    expect(threadQueryKeys.detail("thread-1")).toEqual(["threads", "detail", "thread-1"]);
    expect(threadQueryKeys.pinnedApprovals("thread-1")).toEqual([
      "threads",
      "detail",
      "thread-1",
      "pinned-approvals",
    ]);
    expect(approvalQueryKeys.list()).toEqual(["approvals", "list"]);
    expect(approvalQueryKeys.detail("approval-1")).toEqual(["approvals", "detail", "approval-1"]);
  });

  it("binds query functions to the broker client", async () => {
    const getJson = vi.fn<(path: string) => void>();
    const client: BrokerApiClient = {
      async getJson<T>(path: string): Promise<T> {
        getJson(path);
        return { ok: true } as T;
      },
      async postJson<T>(): Promise<T> {
        return { ok: true } as T;
      },
    };

    await threadListQuery(client).queryFn();
    await threadDetailQuery(client, "thread-1").queryFn();
    await threadPinnedApprovalsQuery(client, "thread-1").queryFn();
    await approvalListQuery(client).queryFn();
    await approvalDetailQuery(client, "approval-1").queryFn();

    expect(getJson).toHaveBeenCalledWith("/api/v1/threads");
    expect(getJson).toHaveBeenCalledWith("/api/v1/threads/thread-1");
    expect(getJson).toHaveBeenCalledWith("/api/v1/threads/thread-1/pinned-approvals");
    expect(getJson).toHaveBeenCalledWith("/api/v1/approvals");
    expect(getJson).toHaveBeenCalledWith("/api/v1/approvals/approval-1");
  });

  it("uses the desktop query defaults", () => {
    const queryClient = createDesktopQueryClient();

    expect(queryClient.getDefaultOptions().queries?.staleTime).toBe(2_000);
    expect(queryClient.getDefaultOptions().queries?.retry).toBe(1);
  });
});
