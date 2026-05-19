import type { ReceiptStatus, ThreadStatus } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { deriveThreadEffectiveStatus } from "../../src/threads/index.ts";

describe("thread effective status derivation", () => {
  it.each([
    {
      name: "terminal stored status wins over pending approvals and failed receipts",
      storedStatus: "merged",
      pendingApprovalCount: 2,
      latestReceiptStatus: "error",
      expected: {
        effectiveStatus: "merged",
        boardColumn: "done",
        currentSeat: "agent",
      },
    },
    {
      name: "pending approvals require human attention",
      storedStatus: "open",
      pendingApprovalCount: 1,
      latestReceiptStatus: "error",
      expected: {
        effectiveStatus: "needs_attention",
        attentionReason: "pending_approval",
        boardColumn: "needs_me",
        currentSeat: "human",
      },
    },
    {
      name: "latest error receipt requires human attention",
      storedStatus: "in_progress",
      pendingApprovalCount: 0,
      latestReceiptStatus: "error",
      expected: {
        effectiveStatus: "needs_attention",
        attentionReason: "failed",
        boardColumn: "needs_me",
        currentSeat: "human",
      },
    },
    {
      name: "latest stalled receipt requires human attention",
      storedStatus: "open",
      pendingApprovalCount: 0,
      latestReceiptStatus: "stalled",
      expected: {
        effectiveStatus: "needs_attention",
        attentionReason: "stalled",
        boardColumn: "needs_me",
        currentSeat: "human",
      },
    },
    {
      name: "open passes through to the running board column",
      storedStatus: "open",
      pendingApprovalCount: 0,
      latestReceiptStatus: "ok",
      expected: {
        effectiveStatus: "open",
        boardColumn: "running",
        currentSeat: "agent",
      },
    },
    {
      name: "needs_review passes through with the human as current seat",
      storedStatus: "needs_review",
      pendingApprovalCount: 0,
      latestReceiptStatus: "ok",
      expected: {
        effectiveStatus: "needs_review",
        boardColumn: "review",
        currentSeat: "human",
      },
    },
  ] satisfies readonly {
    readonly name: string;
    readonly storedStatus: ThreadStatus;
    readonly pendingApprovalCount: number;
    readonly latestReceiptStatus: ReceiptStatus;
    readonly expected: ReturnType<typeof deriveThreadEffectiveStatus>;
  }[])("$name", ({ storedStatus, pendingApprovalCount, latestReceiptStatus, expected }) => {
    expect(
      deriveThreadEffectiveStatus({
        storedStatus,
        pendingApprovalCount,
        latestReceiptStatus,
      }),
    ).toEqual(expected);
  });
});
