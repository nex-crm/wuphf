import {
  type ReceiptId,
  type ReceiptSnapshot,
  receiptFromJson,
  receiptToJson,
  type ThreadId,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";
import BetterSqlite3 from "better-sqlite3";

import { createEventLog, type EventLog, openDatabase, runMigrations } from "./event-log/index.ts";
import {
  decodeListCursor,
  encodeListCursor,
  type ListFilter,
  type ListPage,
  type ReceiptStore,
  ReceiptStoreFullError,
  resolveListLimit,
} from "./receipt-store.ts";

export interface SqliteReceiptStoreConfig {
  readonly path: string;
}

interface ReceiptExistsRow {
  readonly present: 1;
}

interface ProjectionPayloadRow {
  readonly lsn: number;
  readonly payload: Buffer;
}

interface CountRow {
  readonly count: number;
}

type ProjectionInsertParams = [ReceiptId, ThreadId | null, number, number, Buffer];

export class SqliteReceiptStore implements ReceiptStore {
  private readonly eventLog: EventLog;
  private readonly receiptExistsStmt: Database.Statement<[ReceiptId], ReceiptExistsRow>;
  private readonly insertProjectionStmt: Database.Statement<ProjectionInsertParams>;
  private readonly getPayloadStmt: Database.Statement<[ReceiptId], ProjectionPayloadRow>;
  private readonly listAllStmt: Database.Statement<[number, number], ProjectionPayloadRow>;
  private readonly listThreadStmt: Database.Statement<
    [ThreadId, number, number],
    ProjectionPayloadRow
  >;
  private readonly countStmt: Database.Statement<[], CountRow>;
  private readonly putTransaction: Database.Transaction<
    (receipt: ReceiptSnapshot) => { readonly existed: boolean }
  >;
  private closed = false;

  static open(config: SqliteReceiptStoreConfig): SqliteReceiptStore {
    const db = openDatabase(config);
    try {
      runMigrations(db);
      return new SqliteReceiptStore(db);
    } catch (err) {
      db.close();
      throw err;
    }
  }

  constructor(
    private readonly db: Database.Database,
    eventLog: EventLog = createEventLog(db),
  ) {
    this.eventLog = eventLog;
    this.receiptExistsStmt = db.prepare<[ReceiptId], ReceiptExistsRow>(
      "SELECT 1 AS present FROM receipts_projection WHERE receipt_id = ?",
    );
    this.insertProjectionStmt = db.prepare<ProjectionInsertParams>(
      "INSERT INTO receipts_projection (receipt_id, thread_id, schema_version, lsn, payload) VALUES (?, ?, ?, ?, ?)",
    );
    this.getPayloadStmt = db.prepare<[ReceiptId], ProjectionPayloadRow>(
      "SELECT lsn, payload FROM receipts_projection WHERE receipt_id = ?",
    );
    this.listAllStmt = db.prepare<[number, number], ProjectionPayloadRow>(
      "SELECT lsn, payload FROM receipts_projection WHERE lsn > ? ORDER BY lsn ASC LIMIT ?",
    );
    this.listThreadStmt = db.prepare<[ThreadId, number, number], ProjectionPayloadRow>(
      "SELECT lsn, payload FROM receipts_projection WHERE thread_id = ? AND lsn > ? ORDER BY lsn ASC LIMIT ?",
    );
    this.countStmt = db.prepare<[], CountRow>("SELECT COUNT(*) AS count FROM receipts_projection");
    this.putTransaction = db.transaction((receipt: ReceiptSnapshot) => {
      if (this.receiptExistsStmt.get(receipt.id) !== undefined) {
        return { existed: true };
      }

      const payload = Buffer.from(receiptToJson(receipt), "utf8");
      const lsn = this.eventLog.append({ type: "receipt.put", payload });
      this.insertProjectionStmt.run(
        receipt.id,
        projectionThreadId(receipt),
        receipt.schemaVersion,
        lsn,
        payload,
      );
      return { existed: false };
    });
  }

  async put(receipt: ReceiptSnapshot): Promise<{ readonly existed: boolean }> {
    try {
      return this.putTransaction.immediate(receipt);
    } catch (err) {
      if (isReceiptIdConstraintError(err)) {
        return { existed: true };
      }
      // SQLITE_FULL = filesystem out of space (or page-cache limit hit).
      // Surface as `ReceiptStoreFullError` so the HTTP route reuses the
      // same 507 path the in-memory store uses for its byte-count cap
      // (security triangulation T12). Without this mapping the route
      // would log a 500 and the operator would have to read the stack
      // trace to learn that a disk-full condition was the cause.
      if (isSqliteFullError(err)) {
        throw new ReceiptStoreFullError("SqliteReceiptStore: database full (SQLITE_FULL)");
      }
      throw err;
    }
  }

  async get(id: ReceiptId): Promise<ReceiptSnapshot | null> {
    const row = this.getPayloadStmt.get(id);
    if (row === undefined) {
      return null;
    }
    return receiptFromJson(row.payload.toString("utf8"));
  }

  async list(filter?: ListFilter): Promise<ListPage> {
    const limit = resolveListLimit(filter?.limit);
    const afterLsn = filter?.cursor !== undefined ? decodeListCursor(filter.cursor) : 0;
    const rows =
      filter?.threadId === undefined
        ? this.listAllStmt.all(afterLsn, limit + 1)
        : this.listThreadStmt.all(filter.threadId, afterLsn, limit + 1);
    const visibleRows = rows.slice(0, limit);
    const items = visibleRows.map((row) => receiptFromJson(row.payload.toString("utf8")));
    const lastRow = visibleRows.at(-1);

    return {
      items,
      nextCursor:
        rows.length > limit && lastRow !== undefined ? encodeListCursor(lastRow.lsn) : null,
    };
  }

  size(): number {
    const row = this.countStmt.get();
    if (row === undefined) {
      throw new Error("receipts_projection count query returned no row");
    }
    return row.count;
  }

  close(): void {
    if (this.closed) {
      return;
    }
    this.db.close();
    this.closed = true;
  }
}

function projectionThreadId(receipt: ReceiptSnapshot): ThreadId | null {
  if (receipt.schemaVersion === 2 && receipt.threadId !== undefined) {
    return receipt.threadId;
  }
  return null;
}

function isReceiptIdConstraintError(err: unknown): boolean {
  return (
    err instanceof BetterSqlite3.SqliteError &&
    (err.code === "SQLITE_CONSTRAINT_PRIMARYKEY" || err.code === "SQLITE_CONSTRAINT_UNIQUE") &&
    err.message.includes("receipts_projection.receipt_id")
  );
}

function isSqliteFullError(err: unknown): boolean {
  return err instanceof BetterSqlite3.SqliteError && err.code === "SQLITE_FULL";
}
