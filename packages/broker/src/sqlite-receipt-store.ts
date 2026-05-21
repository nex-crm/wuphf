import type { DatabaseSync, StatementSync } from "node:sqlite";
import {
  lsnFromV1Number,
  type ReceiptId,
  type ReceiptSnapshot,
  receiptFromJson,
  receiptToJson,
  type TaskId,
  type ThreadId,
} from "@wuphf/protocol";

import { createEventLog, type EventLog, openDatabase, runMigrations } from "./event-log/index.ts";
import {
  isSqliteBusyError,
  isSqliteConstraintError,
  isSqliteFullError,
  isSqliteUnavailableError,
  sqliteErrorLabel,
} from "./internal/sqlite-errors.ts";
import { createTransaction, type TransactionFn } from "./internal/sqlite-transaction.ts";
import { type TypedStatement, typed } from "./internal/typed-statement.ts";
import {
  decodeListCursor,
  encodeListCursor,
  type ListFilter,
  type ListPage,
  type ReceiptPutResult,
  type ReceiptStore,
  ReceiptStoreBusyError,
  ReceiptStoreFullError,
  ReceiptStoreUnavailableError,
  ReceiptThreadNotFoundError,
  resolveListLimit,
} from "./receipt-store.ts";

// Default receipt count cap for the durable store. Sized 10x the
// `InMemoryReceiptStore` default to reflect the additional headroom of
// disk-backed storage, while still bounding an authenticated hostile
// client's disk-fill DoS. Hosts can raise or lower via
// `SqliteReceiptStore.open({ path, maxReceipts })`.
const DEFAULT_SQLITE_MAX_RECEIPTS = 100_000;
const utf8Decoder = new TextDecoder("utf-8", { fatal: true });

export interface SqliteReceiptStoreConfig {
  readonly path: string;
  /**
   * Maximum number of receipts the store will accept. `put` throws
   * `ReceiptStoreFullError` once the projection table holds this many
   * rows. Defaults to `DEFAULT_SQLITE_MAX_RECEIPTS` (100_000).
   */
  readonly maxReceipts?: number;
  readonly defaultThreadId?: ThreadId;
}

export interface SqliteReceiptStoreFromDatabaseConfig {
  readonly maxReceipts?: number;
  readonly defaultThreadId?: ThreadId;
}

interface ReceiptExistsRow {
  readonly present: 1;
}

interface ProjectionPayloadRow {
  readonly lsn: number;
  readonly payload: Uint8Array;
}

interface CountRow {
  readonly count: number;
}

interface ExistsRow {
  readonly present: 1;
}

export class SqliteReceiptStore implements ReceiptStore {
  private readonly eventLog: EventLog;
  private readonly maxReceipts: number;
  private readonly receiptExistsStmt: StatementSync;
  private readonly threadExistsStmt: StatementSync;
  private readonly insertProjectionStmt: TypedStatement<
    [ReceiptId, ThreadId | null, number, number, Uint8Array],
    never
  >;
  private readonly insertThreadReceiptStmt: TypedStatement<
    [ThreadId, ReceiptId, TaskId | undefined, number],
    never
  >;
  private readonly getPayloadStmt: StatementSync;
  private readonly listAllStmt: StatementSync;
  private readonly listThreadStmt: StatementSync;
  private readonly countStmt: StatementSync;
  private readonly putTransaction: TransactionFn<[ReceiptSnapshot], ReceiptPutResult>;
  private defaultThreadId: ThreadId | null;
  private closed = false;

  static open(config: SqliteReceiptStoreConfig): SqliteReceiptStore {
    const db = openDatabase(config);
    try {
      runMigrations(db);
      return new SqliteReceiptStore(
        db,
        undefined,
        config.maxReceipts,
        config.defaultThreadId ?? null,
      );
    } catch (err) {
      db.close();
      throw err;
    }
  }

  static fromDatabase(
    db: DatabaseSync,
    eventLog: EventLog,
    config: SqliteReceiptStoreFromDatabaseConfig = {},
  ): SqliteReceiptStore {
    return new SqliteReceiptStore(db, eventLog, config.maxReceipts, config.defaultThreadId ?? null);
  }

  private constructor(
    private readonly db: DatabaseSync,
    eventLog: EventLog = createEventLog(db),
    maxReceipts: number | undefined = undefined,
    defaultThreadId: ThreadId | null = null,
  ) {
    this.eventLog = eventLog;
    this.defaultThreadId = defaultThreadId;
    const requestedMax = maxReceipts ?? DEFAULT_SQLITE_MAX_RECEIPTS;
    if (!Number.isInteger(requestedMax) || requestedMax <= 0) {
      throw new Error(
        `SqliteReceiptStore: maxReceipts must be a positive integer, got ${requestedMax}`,
      );
    }
    this.maxReceipts = requestedMax;
    this.receiptExistsStmt = db.prepare(
      "SELECT 1 AS present FROM receipts_projection WHERE receipt_id = ?",
    );
    this.threadExistsStmt = db.prepare("SELECT 1 AS present FROM threads WHERE thread_id = ?");
    this.insertProjectionStmt = typed<
      [ReceiptId, ThreadId | null, number, number, Uint8Array],
      never
    >(
      db.prepare(
        "INSERT INTO receipts_projection (receipt_id, thread_id, schema_version, lsn, payload) VALUES (?, ?, ?, ?, ?)",
      ),
    );
    this.insertThreadReceiptStmt = typed<[ThreadId, ReceiptId, TaskId | undefined, number], never>(
      db.prepare(
        `INSERT INTO thread_receipts (thread_id, receipt_id, task_id, lsn)
         VALUES (?, ?, ?, ?)`,
      ),
    );
    this.getPayloadStmt = db.prepare(
      "SELECT lsn, payload FROM receipts_projection WHERE receipt_id = ?",
    );
    this.listAllStmt = db.prepare(
      "SELECT lsn, payload FROM receipts_projection WHERE lsn > ? ORDER BY lsn ASC LIMIT ?",
    );
    this.listThreadStmt = db.prepare(
      "SELECT lsn, payload FROM receipts_projection WHERE thread_id = ? AND lsn > ? ORDER BY lsn ASC LIMIT ?",
    );
    this.countStmt = db.prepare("SELECT COUNT(*) AS count FROM receipts_projection");
    this.putTransaction = createTransaction(db, (receipt: ReceiptSnapshot) => {
      if ((this.receiptExistsStmt.get(receipt.id) as ReceiptExistsRow | undefined) !== undefined) {
        return { existed: true, lsn: null };
      }
      // Cap check runs AFTER the existence check (mirrors the in-memory
      // store): a duplicate POST against a store at capacity still
      // returns 409, not 507. The count query inside the
      // `BEGIN IMMEDIATE` transaction is serialized against concurrent
      // writers, so the check + insert are atomic.
      const countRow = this.countStmt.get() as CountRow | undefined;
      if (countRow !== undefined && countRow.count >= this.maxReceipts) {
        throw new ReceiptStoreFullError(`SqliteReceiptStore at capacity (${this.maxReceipts})`);
      }

      const threadId = projectionThreadId(receipt, this.defaultThreadId);
      if (
        threadId !== null &&
        (this.threadExistsStmt.get(threadId) as ExistsRow | undefined) === undefined
      ) {
        throw new ReceiptThreadNotFoundError(`thread ${threadId} not found`);
      }

      const payload = Buffer.from(receiptToJson(receipt), "utf8");
      const lsn = this.eventLog.append({ type: "receipt.put", payload });
      this.insertProjectionStmt.run(receipt.id, threadId, receipt.schemaVersion, lsn, payload);
      if (threadId !== null) {
        this.insertThreadReceiptStmt.run(threadId, receipt.id, receipt.taskId, lsn);
      }
      return { existed: false, lsn: lsnFromV1Number(lsn) };
    });
  }

  sharesProvenance(db: DatabaseSync, eventLog: EventLog): boolean {
    return this.db === db && this.eventLog === eventLog;
  }

  setDefaultThreadIdForThreadlessReceipts(threadId: ThreadId): void {
    this.defaultThreadId = threadId;
  }

  async put(receipt: ReceiptSnapshot): Promise<ReceiptPutResult> {
    try {
      return this.putTransaction.immediate(receipt);
    } catch (err) {
      if (isReceiptIdConstraintError(err)) {
        return { existed: true, lsn: null };
      }
      // SQLITE_FULL = filesystem out of space (or page-cache limit hit).
      // Surface as `ReceiptStoreFullError` so the HTTP route reuses the
      // same 507 path the in-memory store uses for its byte-count cap.
      if (isSqliteFullError(err)) {
        throw new ReceiptStoreFullError("SqliteReceiptStore: database full (SQLITE_FULL)");
      }
      // Classify the remaining SQLite error codes so the route can map
      // them to 503 (busy = retryable; readonly/IOERR = persistent)
      // instead of a generic 500. The operator's on-call view goes
      // from "internal_error" to a structured reason that tells them
      // whether a retry will help.
      if (isSqliteBusyError(err)) {
        throw new ReceiptStoreBusyError("SqliteReceiptStore: database busy (SQLITE_BUSY/LOCKED)");
      }
      if (isSqliteUnavailableError(err)) {
        throw new ReceiptStoreUnavailableError(
          `SqliteReceiptStore: storage error (${sqliteErrorLabel(err)})`,
        );
      }
      throw err;
    }
  }

  async get(id: ReceiptId): Promise<ReceiptSnapshot | null> {
    let row: ProjectionPayloadRow | undefined;
    try {
      row = this.getPayloadStmt.get(id) as ProjectionPayloadRow | undefined;
    } catch (err) {
      throw classifySqliteReadError(err);
    }
    if (row === undefined) {
      return null;
    }
    return receiptFromJson(utf8Decoder.decode(row.payload));
  }

  async list(filter?: ListFilter): Promise<ListPage> {
    const limit = resolveListLimit(filter?.limit);
    const afterLsn = filter?.cursor !== undefined ? decodeListCursor(filter.cursor) : 0;
    let rows: ProjectionPayloadRow[];
    try {
      rows =
        filter?.threadId === undefined
          ? (this.listAllStmt.all(afterLsn, limit + 1) as unknown as ProjectionPayloadRow[])
          : (this.listThreadStmt.all(
              filter.threadId,
              afterLsn,
              limit + 1,
            ) as unknown as ProjectionPayloadRow[]);
    } catch (err) {
      throw classifySqliteReadError(err);
    }
    const visibleRows = rows.slice(0, limit);
    const items = visibleRows.map((row) => receiptFromJson(utf8Decoder.decode(row.payload)));
    const lastRow = visibleRows.at(-1);

    return {
      items,
      nextCursor:
        rows.length > limit && lastRow !== undefined ? encodeListCursor(lastRow.lsn) : null,
    };
  }

  size(): number {
    let row: CountRow | undefined;
    try {
      row = this.countStmt.get() as CountRow | undefined;
    } catch (err) {
      throw classifySqliteReadError(err);
    }
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

// Map SQLite errors raised by read-path statements (`get`/`list`/`size`)
// into the same classified error hierarchy `put` uses. Without this,
// transient `SQLITE_BUSY` on a read returns a generic 500 from the
// route — clients can't distinguish "retry will help" from "DB is
// corrupt". `SQLITE_FULL` is not handled here because reads don't
// allocate pages.
function classifySqliteReadError(err: unknown): Error {
  if (isSqliteBusyError(err)) {
    return new ReceiptStoreBusyError("SqliteReceiptStore: database busy (SQLITE_BUSY/LOCKED)");
  }
  if (isSqliteUnavailableError(err)) {
    return new ReceiptStoreUnavailableError(
      `SqliteReceiptStore: storage error (${sqliteErrorLabel(err)})`,
    );
  }
  return err instanceof Error ? err : new Error(String(err));
}

function projectionThreadId(
  receipt: ReceiptSnapshot,
  defaultThreadId: ThreadId | null,
): ThreadId | null {
  if (receipt.schemaVersion !== 2) return null;
  return receipt.threadId ?? defaultThreadId;
}

function isReceiptIdConstraintError(err: unknown): boolean {
  return (
    isSqliteConstraintError(err) &&
    err instanceof Error &&
    err.message.includes("receipts_projection.receipt_id")
  );
}
