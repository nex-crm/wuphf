import {
  asSha256Hex,
  asSignerIdentity,
  asThreadId,
  asThreadSpecRevisionId,
  canonicalJSON,
  type EventLsn,
  type JsonValue,
  lsnFromV1Number,
  type Sha256Hex,
  type SignerIdentity,
  THREAD_STATUS_VALUES,
  type Thread,
  type ThreadAuditEventKind,
  type ThreadCreatedAuditPayload,
  type ThreadExternalRefs,
  type ThreadId,
  type ThreadSpecEditedAuditPayload,
  type ThreadSpecRevision,
  type ThreadStatus,
  type ThreadStatusChangedAuditPayload,
  threadAuditPayloadFromJsonValue,
  threadExternalRefsFromJsonValue,
  threadExternalRefsToJsonValue,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import type { EventLog, EventLogRecord, EventType } from "../event-log/index.ts";

const THREAD_EVENT_BATCH_SIZE = 500;
const THREAD_STATUS_SET: ReadonlySet<string> = new Set<string>(THREAD_STATUS_VALUES);

export interface ThreadStateRow {
  readonly id: ThreadId;
  readonly title: string;
  readonly status: ThreadStatus;
  readonly headLsn: EventLsn;
  readonly createdBy: SignerIdentity;
  readonly createdAt: Date;
  readonly updatedAt: Date;
  readonly closedAt?: Date | undefined;
  readonly spec: ThreadSpecRevision;
  readonly externalRefs: ThreadExternalRefs;
}

export interface ThreadStateStore {
  applyEvent(record: EventLogRecord): void;
  rebuildFromLog(eventLog: EventLog, fromLsn?: number): void;
  getById(threadId: ThreadId): ThreadStateRow | null;
  list(filter?: { readonly status?: ThreadStatus }): readonly ThreadStateRow[];
}

interface ThreadDbRow {
  readonly threadId: string;
  readonly title: string;
  readonly status: string;
  readonly headLsn: number;
  readonly createdBy: string;
  readonly createdAtMs: number;
  readonly updatedAtMs: number;
  readonly closedAtMs: number | null;
  readonly specRevisionId: string | null;
  readonly specBaseRevisionId: string | null;
  readonly specContent: string | null;
  readonly specContentHash: string | null;
  readonly specAuthoredBy: string | null;
  readonly specAuthoredAtMs: number | null;
  readonly externalRefs: string;
}

type DecodedThreadEvent =
  | {
      readonly kind: "thread_created";
      readonly payload: ThreadCreatedAuditPayload;
    }
  | {
      readonly kind: "thread_spec_edited";
      readonly payload: ThreadSpecEditedAuditPayload;
    }
  | {
      readonly kind: "thread_status_changed";
      readonly payload: ThreadStatusChangedAuditPayload;
    };

export function createThreadStateStore(db: Database.Database): ThreadStateStore {
  const insertCreatedStmt = db.prepare<
    [string, string, string, number, string, number, number, string]
  >(
    `INSERT INTO threads
       (thread_id, title, status, head_lsn, created_by, created_at_ms, updated_at_ms, external_refs)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
  );
  const updateSpecStmt = db.prepare<
    [number, number, string, string | null, string, string, string, number, string]
  >(
    `UPDATE threads SET
       head_lsn = ?,
       updated_at_ms = ?,
       spec_revision_id = ?,
       spec_base_revision_id = ?,
       spec_content = ?,
       spec_content_hash = ?,
       spec_authored_by = ?,
       spec_authored_at_ms = ?
     WHERE thread_id = ?`,
  );
  const updateStatusStmt = db.prepare<[string, number, number, number | null, string]>(
    `UPDATE threads SET
       status = ?,
       head_lsn = ?,
       updated_at_ms = ?,
       closed_at_ms = ?
     WHERE thread_id = ?`,
  );
  const getByIdStmt = db.prepare<[string], ThreadDbRow>(
    `SELECT thread_id AS threadId,
            title,
            status,
            head_lsn AS headLsn,
            created_by AS createdBy,
            created_at_ms AS createdAtMs,
            updated_at_ms AS updatedAtMs,
            closed_at_ms AS closedAtMs,
            spec_revision_id AS specRevisionId,
            spec_base_revision_id AS specBaseRevisionId,
            spec_content AS specContent,
            spec_content_hash AS specContentHash,
            spec_authored_by AS specAuthoredBy,
            spec_authored_at_ms AS specAuthoredAtMs,
            external_refs AS externalRefs
     FROM threads WHERE thread_id = ?`,
  );
  const listAllStmt = db.prepare<[], ThreadDbRow>(
    `SELECT thread_id AS threadId,
            title,
            status,
            head_lsn AS headLsn,
            created_by AS createdBy,
            created_at_ms AS createdAtMs,
            updated_at_ms AS updatedAtMs,
            closed_at_ms AS closedAtMs,
            spec_revision_id AS specRevisionId,
            spec_base_revision_id AS specBaseRevisionId,
            spec_content AS specContent,
            spec_content_hash AS specContentHash,
            spec_authored_by AS specAuthoredBy,
            spec_authored_at_ms AS specAuthoredAtMs,
            external_refs AS externalRefs
     FROM threads ORDER BY head_lsn ASC`,
  );
  const listByStatusStmt = db.prepare<[string], ThreadDbRow>(
    `SELECT thread_id AS threadId,
            title,
            status,
            head_lsn AS headLsn,
            created_by AS createdBy,
            created_at_ms AS createdAtMs,
            updated_at_ms AS updatedAtMs,
            closed_at_ms AS closedAtMs,
            spec_revision_id AS specRevisionId,
            spec_base_revision_id AS specBaseRevisionId,
            spec_content AS specContent,
            spec_content_hash AS specContentHash,
            spec_authored_by AS specAuthoredBy,
            spec_authored_at_ms AS specAuthoredAtMs,
            external_refs AS externalRefs
     FROM threads WHERE status = ? ORDER BY head_lsn ASC`,
  );
  const clearStmt = db.prepare<[]>("DELETE FROM threads");

  const applyEventInner = (record: EventLogRecord): void => {
    const decoded = decodeThreadEvent(record);
    if (decoded === null) return;
    if (decoded.kind === "thread_created") {
      const createdAtMs = decoded.payload.createdAt.getTime();
      insertCreatedStmt.run(
        decoded.payload.threadId,
        decoded.payload.title,
        "open",
        record.lsn,
        decoded.payload.createdBy,
        createdAtMs,
        createdAtMs,
        canonicalJSON(threadExternalRefsToJsonValue(decoded.payload.externalRefs)),
      );
      return;
    }
    if (decoded.kind === "thread_spec_edited") {
      const authoredAtMs = decoded.payload.authoredAt.getTime();
      const result = updateSpecStmt.run(
        record.lsn,
        authoredAtMs,
        decoded.payload.revisionId,
        decoded.payload.baseRevisionId ?? null,
        canonicalJSON(decoded.payload.content),
        decoded.payload.contentHash,
        decoded.payload.authoredBy,
        authoredAtMs,
        decoded.payload.threadId,
      );
      if (result.changes !== 1) {
        throw new Error("thread projection spec edit referenced a missing thread");
      }
      return;
    }
    const changedAtMs = decoded.payload.changedAt.getTime();
    const closedAtMs = isTerminalStatus(decoded.payload.toStatus) ? changedAtMs : null;
    const result = updateStatusStmt.run(
      decoded.payload.toStatus,
      record.lsn,
      changedAtMs,
      closedAtMs,
      decoded.payload.threadId,
    );
    if (result.changes !== 1) {
      throw new Error("thread projection status change referenced a missing thread");
    }
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
      const batch = eventLog.readFromLsn(cursor, THREAD_EVENT_BATCH_SIZE);
      if (batch.length === 0) break;
      for (const record of batch) {
        applyEventInner(record);
      }
      const last = batch.at(-1);
      if (last === undefined) break;
      cursor = last.lsn;
      if (batch.length < THREAD_EVENT_BATCH_SIZE) break;
    }
  });

  return {
    applyEvent(record: EventLogRecord): void {
      applyEventInner(record);
    },
    rebuildFromLog(eventLog: EventLog, fromLsn = 0): void {
      rebuildTransaction.immediate(eventLog, fromLsn);
    },
    getById(threadId: ThreadId): ThreadStateRow | null {
      const row = getByIdStmt.get(threadId);
      return row === undefined ? null : toThreadStateRow(row);
    },
    list(filter?: { readonly status?: ThreadStatus }): readonly ThreadStateRow[] {
      const rows =
        filter?.status === undefined ? listAllStmt.all() : listByStatusStmt.all(filter.status);
      return rows.map(toThreadStateRow);
    },
  };
}

export function threadAuditKindForEventType(type: EventType): ThreadAuditEventKind | null {
  if (type === "thread.created") return "thread_created";
  if (type === "thread.spec_edited") return "thread_spec_edited";
  if (type === "thread.status_changed") return "thread_status_changed";
  return null;
}

export function threadStateRowToThread(row: ThreadStateRow, taskIds: Thread["taskIds"]): Thread {
  return {
    id: row.id,
    title: row.title,
    status: row.status,
    spec: row.spec,
    externalRefs: row.externalRefs,
    taskIds,
    createdBy: row.createdBy,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
    ...(row.closedAt === undefined ? {} : { closedAt: row.closedAt }),
  };
}

function decodeThreadEvent(record: EventLogRecord): DecodedThreadEvent | null {
  const kind = threadAuditKindForEventType(record.type);
  if (kind === null) return null;
  const parsed = JSON.parse(record.payload.toString("utf8")) as unknown;
  const payload = threadAuditPayloadFromJsonValue(kind, parsed);
  if (kind === "thread_created") {
    return { kind, payload: payload as ThreadCreatedAuditPayload };
  }
  if (kind === "thread_spec_edited") {
    return { kind, payload: payload as ThreadSpecEditedAuditPayload };
  }
  return { kind, payload: payload as ThreadStatusChangedAuditPayload };
}

function toThreadStateRow(row: ThreadDbRow): ThreadStateRow {
  if (
    row.specRevisionId === null ||
    row.specContent === null ||
    row.specContentHash === null ||
    row.specAuthoredBy === null ||
    row.specAuthoredAtMs === null
  ) {
    throw new Error(`thread projection row ${row.threadId} has no current spec revision`);
  }
  if (!isThreadStatus(row.status)) {
    throw new Error(`thread projection row ${row.threadId} has invalid status ${row.status}`);
  }
  const content = JSON.parse(row.specContent) as JsonValue;
  const spec: ThreadSpecRevision = {
    revisionId: asThreadSpecRevisionId(row.specRevisionId),
    threadId: asThreadId(row.threadId),
    ...(row.specBaseRevisionId === null
      ? {}
      : { baseRevisionId: asThreadSpecRevisionId(row.specBaseRevisionId) }),
    content,
    contentHash: asSha256Hex(row.specContentHash) as Sha256Hex,
    authoredBy: asSignerIdentity(row.specAuthoredBy),
    authoredAt: new Date(row.specAuthoredAtMs),
  };
  return {
    id: asThreadId(row.threadId),
    title: row.title,
    status: row.status,
    headLsn: lsnFromV1Number(row.headLsn),
    createdBy: asSignerIdentity(row.createdBy),
    createdAt: new Date(row.createdAtMs),
    updatedAt: new Date(row.updatedAtMs),
    ...(row.closedAtMs === null ? {} : { closedAt: new Date(row.closedAtMs) }),
    spec,
    externalRefs: threadExternalRefsFromJsonValue(JSON.parse(row.externalRefs)),
  };
}

function isThreadStatus(value: string): value is ThreadStatus {
  return THREAD_STATUS_SET.has(value);
}

function isTerminalStatus(status: ThreadStatus): boolean {
  return status === "merged" || status === "closed";
}
