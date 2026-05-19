import type {
  ReceiptStatus,
  ThreadAttentionReason,
  ThreadBoardColumn,
  ThreadCurrentSeat,
  ThreadEffectiveStatus,
  ThreadStatus,
} from "@wuphf/protocol";

export interface ThreadEffectiveStatusInput {
  readonly storedStatus: ThreadStatus;
  readonly pendingApprovalCount: number;
  readonly latestReceiptStatus?: ReceiptStatus | undefined;
}

export interface ThreadEffectiveStatusResult {
  readonly effectiveStatus: ThreadEffectiveStatus;
  readonly attentionReason?: ThreadAttentionReason | undefined;
  readonly boardColumn: ThreadBoardColumn;
  readonly currentSeat: ThreadCurrentSeat;
}

export function deriveThreadEffectiveStatus(
  input: ThreadEffectiveStatusInput,
): ThreadEffectiveStatusResult {
  const effective = deriveEffectiveStatus(input);
  return {
    effectiveStatus: effective.effectiveStatus,
    ...(effective.attentionReason === undefined
      ? {}
      : { attentionReason: effective.attentionReason }),
    boardColumn: boardColumnForEffectiveStatus(effective.effectiveStatus),
    currentSeat:
      effective.effectiveStatus === "needs_attention" || input.storedStatus === "needs_review"
        ? "human"
        : "agent",
  };
}

function deriveEffectiveStatus(
  input: ThreadEffectiveStatusInput,
): Pick<ThreadEffectiveStatusResult, "effectiveStatus" | "attentionReason"> {
  if (input.storedStatus === "merged" || input.storedStatus === "closed") {
    return { effectiveStatus: input.storedStatus };
  }
  if (input.pendingApprovalCount > 0) {
    return { effectiveStatus: "needs_attention", attentionReason: "pending_approval" };
  }
  if (input.latestReceiptStatus === "error") {
    return { effectiveStatus: "needs_attention", attentionReason: "failed" };
  }
  if (input.latestReceiptStatus === "stalled") {
    return { effectiveStatus: "needs_attention", attentionReason: "stalled" };
  }
  return { effectiveStatus: input.storedStatus };
}

function boardColumnForEffectiveStatus(status: ThreadEffectiveStatus): ThreadBoardColumn {
  if (status === "needs_attention") return "needs_me";
  if (status === "needs_review") return "review";
  if (status === "merged" || status === "closed") return "done";
  return "running";
}
