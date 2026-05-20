import {
  type ApprovalRequest,
  type EventLsn,
  lsnFromV1Number,
  parseLsn,
  THREAD_BOARD_COLUMN_VALUES,
  THREAD_EFFECTIVE_STATUS_VALUES,
  type Thread,
  type ThreadBoardColumn,
  type ThreadEffectiveStatus,
  type ThreadId,
  type ThreadView,
  validateThread,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import { deriveThreadEffectiveStatus } from "./effective-status.ts";
import {
  type ThreadStateRow,
  type ThreadStateStore,
  threadStateRowToThread,
} from "./projections.ts";
import type { ThreadReceiptIndexStore } from "./receipt-index.ts";

export const THREAD_EFFECTIVE_STATUS_SET: ReadonlySet<string> = new Set<string>(
  THREAD_EFFECTIVE_STATUS_VALUES,
);
export const THREAD_BOARD_COLUMN_SET: ReadonlySet<string> = new Set<string>(
  THREAD_BOARD_COLUMN_VALUES,
);

export interface ThreadApprovalQuery {
  countPendingByThread(threadId: ThreadId): number;
  latestHeadLsnByThread(threadId: ThreadId): EventLsn | null;
  pendingByThreadSnapshot(threadId: ThreadId): ThreadApprovalQuerySnapshot;
}

export interface ThreadApprovalQueryRow {
  readonly approval: ApprovalRequest;
  readonly headLsn: EventLsn;
}

export interface ThreadApprovalQuerySnapshot {
  readonly rows: readonly ThreadApprovalQueryRow[];
  readonly headLsn: EventLsn | null;
}

export type ThreadStatusFilter =
  | { readonly kind: "effective_status"; readonly value: ThreadEffectiveStatus }
  | { readonly kind: "board_column"; readonly value: ThreadBoardColumn };

export interface ThreadListViewArgs {
  readonly limit: number;
  readonly filter?: ThreadStatusFilter;
  readonly afterViewLsn?: number;
  readonly approvals: ThreadApprovalQuery | null;
}

export interface ThreadListViewPage {
  readonly threads: readonly ThreadView[];
  readonly nextCursor?: EventLsn;
}

export interface ThreadViewStore {
  listThreadViews(args: ThreadListViewArgs): ThreadListViewPage;
}

interface ThreadListViewItem {
  readonly thread: ThreadView;
  readonly viewLsn: number;
}

interface ThreadViewDeps {
  readonly receiptIndex: ThreadReceiptIndexStore;
  readonly approvals: ThreadApprovalQuery | null;
}

export function createThreadViewStore(
  db: Database.Database,
  state: ThreadStateStore,
  receiptIndex: ThreadReceiptIndexStore,
): ThreadViewStore {
  const listThreadViewsTransaction = db.transaction((args: ThreadListViewArgs) =>
    listThreadViewPage(state, receiptIndex, args),
  );

  return {
    listThreadViews(args: ThreadListViewArgs): ThreadListViewPage {
      return listThreadViewsTransaction.deferred(args);
    },
  };
}

export function threadViewFromRow(row: ThreadStateRow, deps: ThreadViewDeps): ThreadView {
  return threadViewItemFromRow(row, deps).thread;
}

function listThreadViewPage(
  state: ThreadStateStore,
  receiptIndex: ThreadReceiptIndexStore,
  args: ThreadListViewArgs,
): ThreadListViewPage {
  if (!Number.isSafeInteger(args.limit) || args.limit < 1) {
    throw new Error("thread view page limit must be a positive safe integer");
  }
  const afterViewLsn = args.afterViewLsn ?? 0;
  const deps = { receiptIndex, approvals: args.approvals };
  const candidates = state
    .list()
    .map((row) => threadViewItemFromRow(row, deps))
    .filter((item) => item.viewLsn > afterViewLsn)
    .filter((item) => threadMatchesStatusFilter(item.thread, args.filter))
    .sort((left, right) => left.viewLsn - right.viewLsn);
  const visibleItems = candidates.slice(0, args.limit);
  const last = visibleItems.at(-1);
  return {
    threads: visibleItems.map((item) => item.thread),
    ...(candidates.length > args.limit && last !== undefined
      ? { nextCursor: lsnFromV1Number(last.viewLsn) }
      : {}),
  };
}

function threadViewItemFromRow(row: ThreadStateRow, deps: ThreadViewDeps): ThreadListViewItem {
  const refs = deps.receiptIndex.refsForThread(row.id);
  const thread = threadStateRowToThread(row, refs.taskIds);
  assertThreadValid(thread);
  const pendingApprovalCount = deps.approvals?.countPendingByThread(row.id) ?? 0;
  const latestReceipt = deps.receiptIndex.latestForThread(row.id);
  const approvalHeadLsn = deps.approvals?.latestHeadLsnByThread(row.id) ?? null;
  const viewLsn = Math.max(
    parseLsn(row.headLsn).localLsn,
    latestReceipt?.lsn ?? 0,
    approvalHeadLsn === null ? 0 : parseLsn(approvalHeadLsn).localLsn,
  );
  return {
    viewLsn,
    thread: {
      ...thread,
      ...deriveThreadEffectiveStatus({
        storedStatus: row.status,
        pendingApprovalCount,
        latestReceiptStatus: latestReceipt?.status,
      }),
      pendingApprovalCount,
    },
  };
}

function assertThreadValid(thread: Thread): void {
  const threadValidation = validateThread(thread);
  if (!threadValidation.ok) {
    throw new Error(
      `thread projection failed validation: ${threadValidation.errors
        .map((error) => `${error.path}: ${error.message}`)
        .join("; ")}`,
    );
  }
}

function threadMatchesStatusFilter(thread: ThreadView, filter: ThreadStatusFilter | undefined) {
  if (filter === undefined) return true;
  if (filter.kind === "effective_status") return thread.effectiveStatus === filter.value;
  return thread.boardColumn === filter.value;
}
