// Approval projection writer.
//
// `approval_requests` is disposable folded state. The event log is the
// source of truth; writes apply one event incrementally in the append
// transaction, and replay can rebuild the table from LSN 0.

import {
  APPROVAL_REQUEST_SCHEMA_VERSION,
  type ApprovalAuditPayload,
  type ApprovalDecidedAuditPayload,
  type ApprovalDecision,
  type ApprovalRequest,
  type ApprovalRequestedAuditPayload,
  type ApprovalRequestId,
  type ApprovalRequestStatus,
  approvalAuditPayloadFromJsonValue,
  approvalAuditPayloadToJsonValue,
  approvalRequestFromJsonValue,
  approvalRequestToJsonValue,
  canonicalJSON,
  type EventLsn,
  lsnFromV1Number,
  type TaskId,
  type ThreadId,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import type { EventLog, EventLogRecord, EventType } from "../event-log/index.ts";

const APPROVAL_EVENT_BATCH_SIZE = 1_000;

export interface FoldedApprovalRow {
  readonly approval: ApprovalRequest;
  readonly headLsn: EventLsn;
}

export interface ApprovalListFilter {
  readonly status?: ApprovalRequestStatus;
  readonly threadId?: ThreadId;
  readonly taskId?: TaskId;
}

export interface ApprovalProjectionEvent {
  readonly lsn: number;
  readonly type: EventType | string;
  readonly payload: Buffer;
}

export interface ApprovalProjectionRebuildResult {
  readonly eventsApplied: number;
  readonly highestLsn: EventLsn;
}

export interface ApprovalProjection {
  applyEvent(event: ApprovalProjectionEvent): FoldedApprovalRow | null;
  rebuildFromLog(eventLog: EventLog): ApprovalProjectionRebuildResult;
  getById(id: ApprovalRequestId): FoldedApprovalRow | null;
  list(filter?: ApprovalListFilter): readonly FoldedApprovalRow[];
}

interface ApprovalDbRow {
  readonly approvalId: string;
  readonly status: string;
  readonly headLsn: number;
  readonly claim: string;
  readonly scope: string;
  readonly riskClass: string;
  readonly threadId: string | null;
  readonly taskId: string | null;
  readonly receiptId: string | null;
  readonly requestedBy: string;
  readonly requestedAtMs: number;
  readonly decidedBy: string | null;
  readonly decidedAtMs: number | null;
  readonly decision: string | null;
  readonly token: string | null;
}

interface ApprovalEventDbRow {
  readonly lsn: number;
  readonly type: string;
  readonly payload: Buffer;
}

interface ApprovalRequestJsonFields {
  readonly claim: unknown;
  readonly scope: unknown;
}

interface ApprovalDecidedJsonFields {
  readonly token?: unknown;
}

interface ApprovalDecisionWireForRow {
  decision: string;
  decided_by: string;
  decided_at: string;
  token?: unknown;
}

interface ApprovalRequestWireForRow {
  request_id: string;
  claim: unknown;
  scope: unknown;
  risk_class: string;
  requested_by: string;
  requested_at: string;
  status: string;
  schema_version: 1;
  thread_id?: string;
  task_id?: string;
  receipt_id?: string;
  decision?: ApprovalDecisionWireForRow;
}

type RequestUpsertParams = [
  string,
  string,
  number,
  string,
  string,
  string,
  string | null,
  string | null,
  string | null,
  string,
  number,
];

type DecisionUpdateParams = [
  ApprovalRequestStatus,
  number,
  string,
  number,
  ApprovalDecision,
  string | null,
  string,
];

export function createApprovalProjection(db: Database.Database): ApprovalProjection {
  const selectByIdStmt = db.prepare<[string], ApprovalDbRow>(
    approvalSelectSql("WHERE approval_id = ?"),
  );
  const upsertRequestedStmt = db.prepare<RequestUpsertParams>(
    `INSERT INTO approval_requests
       (approval_id, status, head_lsn, claim, scope, risk_class, thread_id, task_id, receipt_id,
        requested_by, requested_at_ms, decided_by, decided_at_ms, decision, token)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL)
     ON CONFLICT (approval_id) DO UPDATE SET
       status = excluded.status,
       head_lsn = excluded.head_lsn,
       claim = excluded.claim,
       scope = excluded.scope,
       risk_class = excluded.risk_class,
       thread_id = excluded.thread_id,
       task_id = excluded.task_id,
       receipt_id = excluded.receipt_id,
       requested_by = excluded.requested_by,
       requested_at_ms = excluded.requested_at_ms,
       decided_by = NULL,
       decided_at_ms = NULL,
       decision = NULL,
       token = NULL`,
  );
  const updateDecidedStmt = db.prepare<DecisionUpdateParams>(
    `UPDATE approval_requests
     SET status = ?, head_lsn = ?, decided_by = ?, decided_at_ms = ?, decision = ?, token = ?
     WHERE approval_id = ?`,
  );
  const clearStmt = db.prepare<[]>("DELETE FROM approval_requests");

  const applyRequested = (
    payload: ApprovalRequestedAuditPayload,
    lsn: number,
  ): FoldedApprovalRow => {
    const approval = approvalFromRequested(payload);
    const wire = approvalRequestToJsonValue(approval) as unknown as ApprovalRequestJsonFields;
    upsertRequestedStmt.run(
      approval.id,
      approval.status,
      lsn,
      canonicalJSON(wire.claim),
      canonicalJSON(wire.scope),
      approval.riskClass,
      approval.threadId ?? null,
      approval.taskId ?? null,
      approval.receiptId ?? null,
      approval.requestedBy,
      dateToMs(approval.requestedAt, "approval.requestedAt"),
    );
    const row = selectByIdStmt.get(approval.id);
    if (row === undefined) {
      throw new Error("approval_requests upsert produced no row");
    }
    return rowToFolded(row);
  };

  const applyDecided = (
    payload: ApprovalDecidedAuditPayload,
    lsn: number,
  ): FoldedApprovalRow | null => {
    const tokenJson = tokenColumn(payload);
    updateDecidedStmt.run(
      statusForDecision(payload.decision),
      lsn,
      payload.decidedBy,
      dateToMs(payload.decidedAt, "approval.decidedAt"),
      payload.decision,
      tokenJson,
      payload.requestId,
    );
    const row = selectByIdStmt.get(payload.requestId);
    return row === undefined ? null : rowToFolded(row);
  };

  const applyEvent = (event: ApprovalProjectionEvent): FoldedApprovalRow | null => {
    const decoded = parseApprovalEvent(event);
    if (decoded === null) return null;
    if (decoded.kind === "approval_requested") {
      return applyRequested(decoded.payload, event.lsn);
    }
    return applyDecided(decoded.payload, event.lsn);
  };

  const rebuildTransaction = db.transaction(
    (eventLog: EventLog): ApprovalProjectionRebuildResult => {
      clearStmt.run();
      let cursor = 0;
      let eventsApplied = 0;
      let highestLsn = 0;
      for (;;) {
        const rows = eventLog.readFromLsn(cursor, APPROVAL_EVENT_BATCH_SIZE);
        if (rows.length === 0) break;
        for (const row of rows) {
          cursor = row.lsn;
          highestLsn = Math.max(highestLsn, row.lsn);
          if (applyEvent(row) !== null) {
            eventsApplied += 1;
          }
        }
        if (rows.length < APPROVAL_EVENT_BATCH_SIZE) break;
      }
      return { eventsApplied, highestLsn: lsnFromV1Number(highestLsn) };
    },
  );

  return {
    applyEvent,
    rebuildFromLog(eventLog: EventLog): ApprovalProjectionRebuildResult {
      return rebuildTransaction.immediate(eventLog);
    },
    getById(id: ApprovalRequestId): FoldedApprovalRow | null {
      const row = selectByIdStmt.get(id);
      return row === undefined ? null : rowToFolded(row);
    },
    list(filter?: ApprovalListFilter): readonly FoldedApprovalRow[] {
      const rows = listRows(db, filter);
      return rows.map(rowToFolded);
    },
  };
}

export function foldApprovalFromLog(
  db: Database.Database,
  requestId: ApprovalRequestId,
): FoldedApprovalRow | null {
  const rows = db
    .prepare<[], ApprovalEventDbRow>(
      `SELECT lsn, type, payload FROM event_log
       WHERE type IN ('approval.requested', 'approval.decided')
       ORDER BY lsn ASC`,
    )
    .all();
  let folded: FoldedApprovalRow | null = null;
  for (const row of rows) {
    const decoded = parseApprovalEvent(row);
    if (decoded === null || decoded.payload.requestId !== requestId) continue;
    if (decoded.kind === "approval_requested") {
      folded = {
        approval: approvalFromRequested(decoded.payload),
        headLsn: lsnFromV1Number(row.lsn),
      };
      continue;
    }
    if (folded === null) continue;
    folded = {
      approval: approvalWithDecision(folded.approval, decoded.payload),
      headLsn: lsnFromV1Number(row.lsn),
    };
  }
  return folded;
}

export function approvalFromRequested(payload: ApprovalRequestedAuditPayload): ApprovalRequest {
  return {
    id: payload.requestId,
    claim: payload.claim,
    scope: payload.scope,
    riskClass: payload.riskClass,
    ...(payload.threadId === undefined ? {} : { threadId: payload.threadId }),
    ...(payload.taskId === undefined ? {} : { taskId: payload.taskId }),
    ...(payload.receiptId === undefined ? {} : { receiptId: payload.receiptId }),
    requestedBy: payload.requestedBy,
    requestedAt: payload.requestedAt,
    status: "pending",
    schemaVersion: APPROVAL_REQUEST_SCHEMA_VERSION,
  };
}

export function approvalWithDecision(
  approval: ApprovalRequest,
  payload: ApprovalDecidedAuditPayload,
): ApprovalRequest {
  const suppliedApprovalToken = payload.decision === "approve" ? payload.token : undefined;
  return {
    ...approval,
    status: statusForDecision(payload.decision),
    decision: {
      decision: payload.decision,
      decidedBy: payload.decidedBy,
      decidedAt: payload.decidedAt,
      ...(suppliedApprovalToken === undefined ? {} : { token: suppliedApprovalToken }),
    },
  };
}

export function statusForDecision(
  decision: ApprovalDecision,
): Exclude<ApprovalRequestStatus, "pending"> {
  if (decision === "approve") return "approved";
  if (decision === "reject") return "rejected";
  return "abstained";
}

function listRows(db: Database.Database, filter: ApprovalListFilter | undefined): ApprovalDbRow[] {
  const clauses: string[] = [];
  const params: string[] = [];
  if (filter?.status !== undefined) {
    clauses.push("status = ?");
    params.push(filter.status);
  }
  if (filter?.threadId !== undefined) {
    clauses.push("thread_id = ?");
    params.push(filter.threadId);
  }
  if (filter?.taskId !== undefined) {
    clauses.push("task_id = ?");
    params.push(filter.taskId);
  }
  const where = clauses.length === 0 ? "" : `WHERE ${clauses.join(" AND ")}`;
  return db
    .prepare<string[], ApprovalDbRow>(`${approvalSelectSql(where)} ORDER BY head_lsn ASC`)
    .all(...params);
}

function approvalSelectSql(where: string): string {
  return `SELECT approval_id AS approvalId, status, head_lsn AS headLsn,
                 claim, scope, risk_class AS riskClass, thread_id AS threadId,
                 task_id AS taskId, receipt_id AS receiptId,
                 requested_by AS requestedBy, requested_at_ms AS requestedAtMs,
                 decided_by AS decidedBy, decided_at_ms AS decidedAtMs,
                 decision, token
          FROM approval_requests ${where}`;
}

function parseApprovalEvent(
  event: Pick<ApprovalProjectionEvent, "type" | "payload">,
):
  | { readonly kind: "approval_requested"; readonly payload: ApprovalRequestedAuditPayload }
  | { readonly kind: "approval_decided"; readonly payload: ApprovalDecidedAuditPayload }
  | null {
  if (event.type === "approval.requested") {
    return {
      kind: "approval_requested",
      payload: approvalAuditPayloadFromJsonValue(
        "approval_requested",
        JSON.parse(event.payload.toString("utf8")) as unknown,
      ) as ApprovalRequestedAuditPayload,
    };
  }
  if (event.type === "approval.decided") {
    return {
      kind: "approval_decided",
      payload: approvalAuditPayloadFromJsonValue(
        "approval_decided",
        JSON.parse(event.payload.toString("utf8")) as unknown,
      ) as ApprovalDecidedAuditPayload,
    };
  }
  return null;
}

function tokenColumn(payload: ApprovalDecidedAuditPayload): string | null {
  const suppliedApprovalToken = payload.decision === "approve" ? payload.token : undefined;
  if (suppliedApprovalToken === undefined) return null;
  const wire = approvalAuditPayloadToJsonValue(
    "approval_decided",
    payload,
  ) as ApprovalDecidedJsonFields;
  const suppliedWireApprovalToken = wire.token;
  if (suppliedWireApprovalToken === undefined) return null;
  return canonicalJSON(suppliedWireApprovalToken);
}

function rowToFolded(row: ApprovalDbRow): FoldedApprovalRow {
  const wire: ApprovalRequestWireForRow = {
    request_id: row.approvalId,
    claim: JSON.parse(row.claim) as unknown,
    scope: JSON.parse(row.scope) as unknown,
    risk_class: row.riskClass,
    requested_by: row.requestedBy,
    requested_at: new Date(row.requestedAtMs).toISOString(),
    status: row.status,
    schema_version: APPROVAL_REQUEST_SCHEMA_VERSION,
  };
  if (row.threadId !== null) wire.thread_id = row.threadId;
  if (row.taskId !== null) wire.task_id = row.taskId;
  if (row.receiptId !== null) wire.receipt_id = row.receiptId;
  if (row.decision !== null) {
    if (row.decidedBy === null || row.decidedAtMs === null) {
      throw new Error(`approval_requests decision fields are incomplete for ${row.approvalId}`);
    }
    const decision: ApprovalDecisionWireForRow = {
      decision: row.decision,
      decided_by: row.decidedBy,
      decided_at: new Date(row.decidedAtMs).toISOString(),
    };
    const storedApprovalTokenJson = row.token;
    if (storedApprovalTokenJson !== null) {
      decision.token = JSON.parse(storedApprovalTokenJson) as unknown;
    }
    wire.decision = decision;
  }
  return {
    approval: approvalRequestFromJsonValue(wire),
    headLsn: lsnFromV1Number(row.headLsn),
  };
}

function dateToMs(date: Date, path: string): number {
  const value = date.getTime();
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new Error(`${path}: timestamp must be a non-negative safe integer`);
  }
  return value;
}

export type { ApprovalAuditPayload, EventLogRecord };
