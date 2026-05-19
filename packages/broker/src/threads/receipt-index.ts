import {
  MAX_THREAD_TASK_IDS,
  type ReceiptId,
  type ReceiptSnapshot,
  receiptFromJson,
  type TaskId,
  type ThreadId,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import type { EventLog, EventLogRecord } from "../event-log/index.ts";
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

export interface ThreadReceiptIndexStore {
  applyReceipt(receipt: ReceiptSnapshot, lsn: number): void;
  applyEvent(record: EventLogRecord): void;
  clear(): void;
  rebuildFromLog(eventLog: EventLog, fromLsn?: number): void;
  list(threadId: ThreadId, filter?: Pick<ListFilter, "cursor" | "limit">): ThreadReceiptIndexPage;
  refsForThread(threadId: ThreadId): ThreadReceiptIndexRefs;
}

interface ThreadReceiptIndexRow {
  readonly receiptId: string;
  readonly taskId: string;
  readonly lsn: number;
}

interface ThreadTaskIndexRow {
  readonly taskId: string;
}

export function createThreadReceiptIndexStore(db: Database.Database): ThreadReceiptIndexStore {
  const insertStmt = db.prepare<[string, string, string, number]>(
    `INSERT INTO thread_receipts (thread_id, receipt_id, task_id, lsn)
     VALUES (?, ?, ?, ?)`,
  );
  const listStmt = db.prepare<[string, number, number], ThreadReceiptIndexRow>(
    `SELECT receipt_id AS receiptId, task_id AS taskId, lsn
     FROM thread_receipts
     WHERE thread_id = ? AND lsn > ?
     ORDER BY lsn ASC
     LIMIT ?`,
  );
  const taskRefsStmt = db.prepare<[string, number], ThreadTaskIndexRow>(
    `SELECT task_id AS taskId
     FROM (
       SELECT task_id, MIN(lsn) AS first_lsn
       FROM thread_receipts
       WHERE thread_id = ?
       GROUP BY task_id
       ORDER BY first_lsn ASC
       LIMIT ?
     )`,
  );
  const receiptRefsStmt = db.prepare<[string, number], ThreadReceiptIndexRow>(
    `SELECT receipt_id AS receiptId, task_id AS taskId, lsn
     FROM thread_receipts
     WHERE thread_id = ?
     ORDER BY lsn ASC
     LIMIT ?`,
  );
  const clearStmt = db.prepare<[]>("DELETE FROM thread_receipts");

  const applyReceiptInner = (receipt: ReceiptSnapshot, lsn: number): void => {
    if (receipt.schemaVersion !== 2 || receipt.threadId === undefined) return;
    assertLsn(lsn);
    insertStmt.run(receipt.threadId, receipt.id, receipt.taskId, lsn);
  };

  const rebuildTransaction = db.transaction((eventLog: EventLog, fromLsn: number): void => {
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
    const receipt = receiptFromJson(record.payload.toString("utf8"));
    applyReceiptInner(receipt, record.lsn);
  };

  return {
    applyReceipt(receipt: ReceiptSnapshot, lsn: number): void {
      applyReceiptInner(receipt, lsn);
    },
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
      const rows = listStmt.all(threadId, afterLsn, limit + 1);
      const visibleRows = rows.slice(0, limit);
      const lastRow = visibleRows.at(-1);
      return {
        items: visibleRows.map(rowToThreadReceiptIndexEntry),
        nextCursor:
          rows.length > limit && lastRow !== undefined ? encodeListCursor(lastRow.lsn) : null,
      };
    },
    refsForThread(threadId: ThreadId): ThreadReceiptIndexRefs {
      const receiptRows = receiptRefsStmt.all(threadId, MAX_THREAD_TASK_IDS);
      const taskRows = taskRefsStmt.all(threadId, MAX_THREAD_TASK_IDS);
      return {
        receiptIds: receiptRows.map((row) => row.receiptId as ReceiptId),
        taskIds: taskRows.map((row) => row.taskId as TaskId),
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
