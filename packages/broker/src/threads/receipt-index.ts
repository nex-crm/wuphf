import type { DatabaseSync } from "node:sqlite";
import {
  MAX_THREAD_TASK_IDS,
  type ReceiptId,
  type ReceiptSnapshot,
  type ReceiptStatus,
  receiptFromJson,
  type TaskId,
  type ThreadId,
} from "@wuphf/protocol";

import type { EventLog, EventLogRecord } from "../event-log/index.ts";
import { createTransaction } from "../internal/sqlite-transaction.ts";
import {
  decodeListCursor,
  encodeListCursor,
  type ListFilter,
  ReceiptStoreUnavailableError,
  resolveListLimit,
} from "../receipt-store.ts";

const RECEIPT_EVENT_BATCH_SIZE = 500;

export interface ThreadReceiptIndexEntry {
  readonly receiptId: ReceiptId;
  readonly taskId: TaskId;
  readonly lsn: number;
}

export interface ThreadReceiptIndexPage {
  readonly items: readonly ThreadReceiptIndexEntry[];
  readonly nextCursor: string | null;
}

export interface ThreadReceiptIndexRefs {
  readonly receiptIds: readonly ReceiptId[];
  readonly taskIds: readonly TaskId[];
}

export interface ThreadLatestReceipt {
  readonly receiptId: ReceiptId;
  readonly taskId: TaskId;
  readonly lsn: number;
  readonly status: ReceiptStatus;
}

export interface ThreadReceiptIndexStore {
  applyEvent(record: EventLogRecord): void;
  clear(): void;
  rebuildFromLog(eventLog: EventLog, fromLsn?: number): void;
  list(threadId: ThreadId, filter?: Pick<ListFilter, "cursor" | "limit">): ThreadReceiptIndexPage;
  refsForThread(threadId: ThreadId): ThreadReceiptIndexRefs;
  latestForThread(threadId: ThreadId): ThreadLatestReceipt | null;
}

interface ThreadReceiptIndexRow {
  readonly receiptId: string;
  readonly taskId: string;
  readonly lsn: number;
}

interface ThreadLatestReceiptRow extends ThreadReceiptIndexRow {
  readonly payload: Uint8Array;
}

interface ThreadTaskIndexRow {
  readonly taskId: string;
}

export function createThreadReceiptIndexStore(db: DatabaseSync): ThreadReceiptIndexStore {
  const insertStmt = db.prepare(
    `INSERT INTO thread_receipts (thread_id, receipt_id, task_id, lsn)
     VALUES (?, ?, ?, ?)`,
  );
  const listStmt = db.prepare(
    `SELECT receipt_id AS receiptId, task_id AS taskId, lsn
     FROM thread_receipts
     WHERE thread_id = ? AND lsn > ?
     ORDER BY lsn ASC
     LIMIT ?`,
  );
  const taskRefsStmt = db.prepare(
    `SELECT task_id AS taskId
     FROM (
       SELECT task_id, MIN(lsn) AS first_lsn
       FROM thread_receipts
       WHERE thread_id = ?
       GROUP BY task_id
       ORDER BY first_lsn ASC
       LIMIT ?
     )
     ORDER BY first_lsn ASC`,
  );
  const receiptRefsStmt = db.prepare(
    `SELECT receipt_id AS receiptId, task_id AS taskId, lsn
     FROM thread_receipts
     WHERE thread_id = ?
     ORDER BY lsn ASC
     LIMIT ?`,
  );
  const latestStmt = db.prepare(
    `SELECT tr.receipt_id AS receiptId, tr.task_id AS taskId, tr.lsn, rp.payload
     FROM thread_receipts AS tr
     INNER JOIN receipts_projection AS rp ON rp.receipt_id = tr.receipt_id
     WHERE tr.thread_id = ?
     ORDER BY tr.lsn DESC
     LIMIT 1`,
  );
  const clearStmt = db.prepare("DELETE FROM thread_receipts");

  // Replay-only writer. The live receipt.put transaction owns thread_receipts
  // insertion so route writes cannot accidentally double-apply this index.
  const applyReplayReceipt = (receipt: ReceiptSnapshot, lsn: number): void => {
    if (receipt.schemaVersion !== 2 || receipt.threadId === undefined) return;
    assertLsn(lsn);
    insertStmt.run(receipt.threadId, receipt.id, receipt.taskId, lsn);
  };

  const rebuildTransaction = createTransaction(db, (eventLog: EventLog, fromLsn: number): void => {
    if (!Number.isSafeInteger(fromLsn) || fromLsn < 0) {
      throw new Error(
        `rebuildFromLog: fromLsn must be a non-negative safe integer, got ${fromLsn}`,
      );
    }
    if (fromLsn === 0) {
      clearStmt.run();
    }
    let cursor = fromLsn;
    for (;;) {
      const batch = eventLog.readFromLsn(cursor, RECEIPT_EVENT_BATCH_SIZE);
      if (batch.length === 0) break;
      for (const record of batch) {
        applyEventInner(record);
      }
      const last = batch.at(-1);
      if (last === undefined) break;
      cursor = last.lsn;
      if (batch.length < RECEIPT_EVENT_BATCH_SIZE) break;
    }
  });

  const applyEventInner = (record: EventLogRecord): void => {
    if (record.type !== "receipt.put") return;
    const receipt = receiptFromJson(new TextDecoder().decode(record.payload));
    applyReplayReceipt(receipt, record.lsn);
  };

  return {
    applyEvent(record: EventLogRecord): void {
      applyEventInner(record);
    },
    clear(): void {
      clearStmt.run();
    },
    rebuildFromLog(eventLog: EventLog, fromLsn = 0): void {
      rebuildTransaction.immediate(eventLog, fromLsn);
    },
    list(
      threadId: ThreadId,
      filter?: Pick<ListFilter, "cursor" | "limit">,
    ): ThreadReceiptIndexPage {
      const limit = resolveListLimit(filter?.limit);
      const afterLsn = filter?.cursor === undefined ? 0 : decodeListCursor(filter.cursor);
      const rows = listStmt.all(
        threadId,
        afterLsn,
        limit + 1,
      ) as unknown as ThreadReceiptIndexRow[];
      const visibleRows = rows.slice(0, limit);
      const lastRow = visibleRows.at(-1);
      return {
        items: visibleRows.map(rowToThreadReceiptIndexEntry),
        nextCursor:
          rows.length > limit && lastRow !== undefined ? encodeListCursor(lastRow.lsn) : null,
      };
    },
    refsForThread(threadId: ThreadId): ThreadReceiptIndexRefs {
      const receiptRows = receiptRefsStmt.all(
        threadId,
        MAX_THREAD_TASK_IDS,
      ) as unknown as ThreadReceiptIndexRow[];
      const taskRows = taskRefsStmt.all(
        threadId,
        MAX_THREAD_TASK_IDS,
      ) as unknown as ThreadTaskIndexRow[];
      return {
        receiptIds: receiptRows.map((row) => row.receiptId as ReceiptId),
        taskIds: taskRows.map((row) => row.taskId as TaskId),
      };
    },
    latestForThread(threadId: ThreadId): ThreadLatestReceipt | null {
      const row = latestStmt.get(threadId) as ThreadLatestReceiptRow | undefined;
      if (row === undefined) return null;
      const receipt = receiptFromJson(new TextDecoder().decode(row.payload));
      return {
        receiptId: row.receiptId as ReceiptId,
        taskId: row.taskId as TaskId,
        lsn: row.lsn,
        status: receipt.status,
      };
    },
  };
}

function rowToThreadReceiptIndexEntry(row: ThreadReceiptIndexRow): ThreadReceiptIndexEntry {
  return {
    receiptId: row.receiptId as ReceiptId,
    taskId: row.taskId as TaskId,
    lsn: row.lsn,
  };
}

function assertLsn(lsn: number): void {
  if (!Number.isSafeInteger(lsn) || lsn <= 0) {
    throw new ReceiptStoreUnavailableError(`thread receipt index lsn is invalid: ${lsn}`);
  }
}
