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
  ReceiptStoreBusyError,
  ReceiptStoreFullError,
  ReceiptStoreUnavailableError,
  resolveListLimit,
} from "./receipt-store.ts";

// Default receipt count cap for the durable store. Sized 10x the
// `InMemoryReceiptStore` default to reflect the additional headroom of
// disk-backed storage, while still bounding an authenticated hostile
// client's disk-fill DoS (security triangulation R2-S1). Hosts can raise
// or lower via `SqliteReceiptStore.open({ path, maxReceipts })`.
const DEFAULT_SQLITE_MAX_RECEIPTS = 100_000;

export interface SqliteReceiptStoreConfig {
  readonly path: string;
  /**
   * Maximum number of receipts the store will accept. `put` throws
   * `ReceiptStoreFullError` once the projection table holds this many
   * rows. Defaults to `DEFAULT_SQLITE_MAX_RECEIPTS` (100_000).
   */
  readonly maxReceipts?: number;
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
  private readonly maxReceipts: number;
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
      return new SqliteReceiptStore(db, undefined, config.maxReceipts);
    } catch (err) {
      db.close();
      throw err;
    }
  }

  constructor(
    private readonly db: Database.Database,
    eventLog: EventLog = createEventLog(db),
    maxReceipts: number | undefined = undefined,
  ) {
    this.eventLog = eventLog;
    const requestedMax = maxReceipts ?? DEFAULT_SQLITE_MAX_RECEIPTS;
    if (!Number.isInteger(requestedMax) || requestedMax <= 0) {
      throw new Error(
        `SqliteReceiptStore: maxReceipts must be a positive integer, got ${requestedMax}`,
      );
    }
    this.maxReceipts = requestedMax;
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
      // Cap check runs AFTER the existence check (mirrors the in-memory
      // store, T12 + R2-S1): a duplicate POST against a store at capacity
      // still returns 409, not 507. The count query inside the
      // `BEGIN IMMEDIATE` transaction is serialized against concurrent
      // writers, so the check + insert are atomic.
      const countRow = this.countStmt.get();
      if (countRow !== undefined && countRow.count >= this.maxReceipts) {
        throw new ReceiptStoreFullError(`SqliteReceiptStore at capacity (${this.maxReceipts})`);
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
      // (security triangulation T12).
      if (isSqliteFullError(err)) {
        throw new ReceiptStoreFullError("SqliteReceiptStore: database full (SQLITE_FULL)");
      }
      // sre triangulation R2-SRE2: classify the remaining SQLite error
      // codes so the route can map them to 503 (busy = retryable;
      // readonly/IOERR = persistent) instead of a generic 500. The
      // operator's on-call view goes from "internal_error" to a
      // structured reason that tells them whether a retry will help.
      if (isSqliteBusyError(err)) {
        throw new ReceiptStoreBusyError("SqliteReceiptStore: database busy (SQLITE_BUSY/LOCKED)");
      }
      if (isSqliteUnavailableError(err)) {
        throw new ReceiptStoreUnavailableError(
          `SqliteReceiptStore: storage error (${
            err instanceof BetterSqlite3.SqliteError ? err.code : "unknown"
          })`,
        );
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

function isSqliteBusyError(err: unknown): boolean {
  if (!(err instanceof BetterSqlite3.SqliteError)) return false;
  // SQLITE_BUSY / SQLITE_LOCKED + extended-code variants. The base
  // codes also surface as extended codes like `SQLITE_BUSY_SNAPSHOT`.
  return (
    err.code === "SQLITE_BUSY" ||
    err.code === "SQLITE_LOCKED" ||
    err.code.startsWith("SQLITE_BUSY_") ||
    err.code.startsWith("SQLITE_LOCKED_")
  );
}

function isSqliteUnavailableError(err: unknown): boolean {
  if (!(err instanceof BetterSqlite3.SqliteError)) return false;
  // Persistent / operator-intervention failure modes: read-only DB,
  // I/O errors at any layer, corruption, can't-open. SQLITE_FULL is
  // handled separately because it maps to 507 (out of space, distinct
  // from "store is broken").
  return (
    err.code === "SQLITE_READONLY" ||
    err.code === "SQLITE_CANTOPEN" ||
    err.code === "SQLITE_CORRUPT" ||
    err.code.startsWith("SQLITE_READONLY_") ||
    err.code.startsWith("SQLITE_IOERR") ||
    err.code.startsWith("SQLITE_CANTOPEN_") ||
    err.code.startsWith("SQLITE_CORRUPT_")
  );
}
