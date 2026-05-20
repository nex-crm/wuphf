import type { ThreadView } from "@wuphf/protocol/browser";
import {
  asReceiptId,
  asSha256Hex,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  asThreadSpecRevisionId,
  threadListResponseToJsonValue,
} from "@wuphf/protocol/browser";

const THREAD_ID = asThreadId("01ARZ3NDEKTSV4RRFFQ69G5FAY");
const TASK_ID = asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW");
const REVISION_ID = asThreadSpecRevisionId("01BRZ3NDEKTSV4RRFFQ69G5FA0");
const SIGNER = asSignerIdentity("operator");
const CREATED_AT = new Date("2026-01-01T00:00:00.000Z");
const UPDATED_AT = new Date("2026-01-02T00:00:00.000Z");

const SPEC_CONTENT = { goal: "fixture" } as const;
// A fixed 64-char lowercase hex used purely as a stand-in for tests;
// the renderer doesn't recompute the hash, it only displays / passes
// the ThreadView through. The real hash is computed broker-side.
const FIXTURE_CONTENT_HASH = asSha256Hex(
  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
);

// Renderer-side fixture for `ThreadView`. Kept local to apps/desktop
// because moving it into a shared package would pull in protocol's
// full test surface.
//
// The protocol codec enforces invariants between `effectiveStatus`,
// `boardColumn`, `currentSeat`, and `attentionReason`. Callers may
// override `boardColumn` to position the fixture on the work board;
// the fixture derives a consistent `effectiveStatus`/`currentSeat`/
// `attentionReason` so the codec accepts the wire form.
type ThreadViewFixtureOverrides = Partial<ThreadView>;

export function sampleThreadView(overrides: ThreadViewFixtureOverrides = {}): ThreadView {
  const boardColumn = overrides.boardColumn ?? "running";
  const effectiveStatus = overrides.effectiveStatus ?? defaultEffectiveStatusForColumn(boardColumn);
  const currentSeat =
    overrides.currentSeat ?? (effectiveStatus === "needs_attention" ? "human" : "agent");
  const attentionReason =
    effectiveStatus === "needs_attention"
      ? (overrides.attentionReason ?? "pending_approval")
      : undefined;

  const base: ThreadView = {
    id: THREAD_ID,
    title: "Approval request protocol",
    status: "open",
    spec: {
      revisionId: REVISION_ID,
      threadId: THREAD_ID,
      content: SPEC_CONTENT,
      contentHash: FIXTURE_CONTENT_HASH,
      authoredBy: SIGNER,
      authoredAt: CREATED_AT,
    },
    externalRefs: { sourceUrls: [], entityIds: [] },
    taskIds: [TASK_ID],
    createdBy: SIGNER,
    createdAt: CREATED_AT,
    updatedAt: UPDATED_AT,
    effectiveStatus,
    boardColumn,
    currentSeat,
    pendingApprovalCount: 0,
    ...(attentionReason !== undefined ? { attentionReason } : {}),
  };

  // Apply caller overrides last so the derived fields can still be
  // overridden explicitly when a test wants to assert a specific
  // codec rejection or pre-codec render path.
  return {
    ...base,
    ...overrides,
  };
}

function defaultEffectiveStatusForColumn(
  column: ThreadView["boardColumn"],
): ThreadView["effectiveStatus"] {
  switch (column) {
    case "needs_me":
      return "needs_attention";
    case "review":
      return "needs_review";
    case "done":
      return "merged";
    case "running":
      return "in_progress";
  }
}

export const SAMPLE_RECEIPT_ID = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");

// Encode an array of ThreadView fixtures into the JSON wire shape the
// renderer's protocol codec consumes. Tests that mock `fetch` use this
// so the response goes through the real `threadListResponseFromJson`
// validator, not a hand-rolled shortcut.
export function threadListWireFromViews(threads: readonly ThreadView[]): unknown {
  return threadListResponseToJsonValue({ schemaVersion: 1, threads });
}
