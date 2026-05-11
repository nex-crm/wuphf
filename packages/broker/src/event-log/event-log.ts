import type Database from "better-sqlite3";
import BetterSqlite3 from "better-sqlite3";

export type EventType = "receipt.put";

export interface EventLogRecord {
  readonly lsn: number;
  readonly tsMs: number;
  readonly type: EventType;
  readonly payload: Buffer;
}

export interface AppendArgs {
  readonly type: EventType;
  readonly payload: Buffer;
}

export interface EventLog {
  append(args: AppendArgs): number;
  readFromLsn(fromLsn: number, limit: number): readonly EventLogRecord[];
  highestLsn(): number;
}

export interface OpenDatabaseArgs {
  readonly path: string;
}

interface InsertedLsnRow {
  readonly lsn: number;
}

interface EventLogRow {
  readonly lsn: number;
  readonly tsMs: number;
  readonly type: string;
  readonly payload: Buffer;
}

interface HighestLsnRow {
  readonly lsn: number;
}

export function openDatabase(args: OpenDatabaseArgs): Database.Database {
  const db = new BetterSqlite3(args.path);
  db.pragma("journal_mode = WAL");
  db.pragma("synchronous = NORMAL");
  db.pragma("foreign_keys = ON");
  db.pragma("busy_timeout = 5000");
  return db;
}

export function createEventLog(db: Database.Database): EventLog {
  const appendStmt = db.prepare<[number, EventType, Buffer], InsertedLsnRow>(
    "INSERT INTO event_log (ts_ms, type, payload) VALUES (?, ?, ?) RETURNING lsn",
  );
  const readFromLsnStmt = db.prepare<[number, number], EventLogRow>(
    "SELECT lsn, ts_ms AS tsMs, type, payload FROM event_log WHERE lsn > ? ORDER BY lsn ASC LIMIT ?",
  );
  const highestLsnStmt = db.prepare<[], HighestLsnRow>(
    "SELECT COALESCE(MAX(lsn), 0) AS lsn FROM event_log",
  );

  return {
    append(args: AppendArgs): number {
      const row = appendStmt.get(Date.now(), args.type, args.payload);
      if (row === undefined) {
        throw new Error("event_log append returned no LSN");
      }
      return row.lsn;
    },

    readFromLsn(fromLsn: number, limit: number): readonly EventLogRecord[] {
      if (!Number.isInteger(fromLsn) || fromLsn < 0) {
        throw new Error(`fromLsn must be a non-negative integer, got ${fromLsn}`);
      }
      if (!Number.isInteger(limit) || limit < 0) {
        throw new Error(`limit must be a non-negative integer, got ${limit}`);
      }
      return readFromLsnStmt.all(fromLsn, limit).map(rowToEventLogRecord);
    },

    highestLsn(): number {
      const row = highestLsnStmt.get();
      if (row === undefined) {
        throw new Error("event_log highestLsn query returned no row");
      }
      return row.lsn;
    },
  };
}

function rowToEventLogRecord(row: EventLogRow): EventLogRecord {
  return {
    lsn: row.lsn,
    tsMs: row.tsMs,
    type: toEventType(row.type),
    payload: Buffer.from(row.payload),
  };
}

function toEventType(type: string): EventType {
  if (type === "receipt.put") {
    return type;
  }
  throw new Error(`Unknown event_log type: ${type}`);
}
