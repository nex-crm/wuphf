// Approval projection writer.
//
// `pending_approvals` is disposable folded state. The event log is the
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
  MAX_ROUTE_APPROVAL_LIST_ITEMS,
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

export interface ApprovalListPageOptions {
  readonly limit: number;
  readonly afterHeadLsn?: number;
}

export interface ApprovalListPage {
  readonly rows: readonly FoldedApprovalRow[];
  readonly nextCursor?: EventLsn;
}

export interface ApprovalPendingByThreadSnapshot {
  readonly rows: readonly FoldedApprovalRow[];
  readonly headLsn: EventLsn | null;
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
  sharesProvenance(db: Database.Database, eventLog: EventLog): boolean;
  applyEvent(event: ApprovalProjectionEvent): FoldedApprovalRow | null;
  rebuildFromLog(eventLog: EventLog): ApprovalProjectionRebuildResult;
  getById(id: ApprovalRequestId): FoldedApprovalRow | null;
  list(filter?: ApprovalListFilter): readonly FoldedApprovalRow[];
  listPage(filter: ApprovalListFilter | undefined, page: ApprovalListPageOptions): ApprovalListPage;
  countPendingByThread(threadId: ThreadId): number;
  listPendingByThread(threadId: ThreadId): readonly FoldedApprovalRow[];
  latestHeadLsnByThread(threadId: ThreadId): EventLsn | null;
  pendingByThreadSnapshot(threadId: ThreadId): ApprovalPendingByThreadSnapshot;
}

export class ApprovalPendingSnapshotOverflowError extends Error {
  readonly threadId: ThreadId;
  readonly count: number;

  constructor(threadId: ThreadId, count: number) {
    super(
      `thread ${threadId} has ${count} pending approvals; refusing partial pinned approvals snapshot`,
    );
    this.name = "ApprovalPendingSnapshotOverflowError";
    this.threadId = threadId;
    this.count = count;
  }
}

export class ApprovalReplayThreadNotFoundError extends Error {
  readonly threadId: ThreadId;
  readonly lsn: number;

  constructor(threadId: ThreadId, lsn: number) {
    super(`approval.requested at ${lsnFromV1Number(lsn)} references missing thread ${threadId}`);
    this.name = "ApprovalReplayThreadNotFoundError";
    this.threadId = threadId;
    this.lsn = lsn;
  }
}

export class ApprovalReplayPendingLimitExceededError extends Error {
  readonly threadId: ThreadId;
  readonly lsn: number;

  constructor(threadId: ThreadId, lsn: number) {
    super(
      `approval.requested at ${lsnFromV1Number(lsn)} exceeds ${MAX_ROUTE_APPROVAL_LIST_ITEMS} pending approvals for thread ${threadId}`,
    );
    this.name = "ApprovalReplayPendingLimitExceededError";
    this.threadId = threadId;
    this.lsn = lsn;
  }
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
  readonly tokenId: string | null;
}

interface ApprovalEventDbRow {
  readonly lsn: number;
  readonly type: string;
  readonly payload: Buffer;
}

interface CountRow {
  readonly count: number;
}

interface MaxLsnRow {
  readonly headLsn: number | null;
}

interface ExistsRow {
  readonly present: 1;
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
  string | null,
  string,
];

export function createApprovalProjection(db: Database.Database): ApprovalProjection {
  const selectByIdStmt = db.prepare<[string], ApprovalDbRow>(
    approvalSelectSql("WHERE approval_id = ?"),
  );
  const insertRequestedStmt = db.prepare<RequestUpsertParams>(
    `INSERT INTO pending_approvals
       (approval_id, status, head_lsn, claim, scope, risk_class, thread_id, task_id, receipt_id,
        requested_by, requested_at_ms, decided_by, decided_at_ms, decision, token, token_id)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, NULL)`,
  );
  const updateDecidedStmt = db.prepare<DecisionUpdateParams>(
    `UPDATE pending_approvals
     SET status = ?, head_lsn = ?, decided_by = ?, decided_at_ms = ?, decision = ?, token = ?,
         token_id = ?
     WHERE approval_id = ? AND status = 'pending'`,
  );
  const clearStmt = db.prepare<[]>("DELETE FROM pending_approvals");
  const countPendingByThreadStmt = db.prepare<[string], CountRow>(
    "SELECT COUNT(*) AS count FROM pending_approvals WHERE thread_id = ? AND status = 'pending'",
  );
  const latestHeadLsnByThreadStmt = db.prepare<[string], MaxLsnRow>(
    "SELECT MAX(head_lsn) AS headLsn FROM pending_approvals WHERE thread_id = ?",
  );
  const threadExistsStmt = db.prepare<[string], ExistsRow>(
    "SELECT 1 AS present FROM threads WHERE thread_id = ?",
  );

  const pendingCountByThread = (threadId: ThreadId): number => {
    const row = countPendingByThreadStmt.get(threadId);
    if (row === undefined) {
      throw new Error(`pending approval count query returned no row for ${threadId}`);
    }
    return row.count;
  };

  const assertReplayRequestedInvariants = (
    payload: ApprovalRequestedAuditPayload,
    lsn: number,
  ): void => {
    const threadId = payload.threadId;
    if (threadId === undefined) return;
    if (threadExistsStmt.get(threadId) === undefined) {
      throw new ApprovalReplayThreadNotFoundError(threadId, lsn);
    }
    if (pendingCountByThread(threadId) >= MAX_ROUTE_APPROVAL_LIST_ITEMS) {
      throw new ApprovalReplayPendingLimitExceededError(threadId, lsn);
    }
  };

  const applyRequested = (
    payload: ApprovalRequestedAuditPayload,
    lsn: number,
    options: { readonly replay: boolean },
  ): FoldedApprovalRow => {
    if (options.replay) {
      assertReplayRequestedInvariants(payload, lsn);
    }
    const approval = approvalFromRequested(payload);
    const wire = approvalRequestToJsonValue(approval) as unknown as ApprovalRequestJsonFields;
    insertRequestedStmt.run(
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
      throw new Error("pending_approvals insert produced no row");
    }
    return rowToFolded(row);
  };

  const applyDecided = (payload: ApprovalDecidedAuditPayload, lsn: number): FoldedApprovalRow => {
    const tokenJson = tokenColumn(payload);
    const tokenId = approvalTokenIdColumn(payload);
    const result = updateDecidedStmt.run(
      statusForDecision(payload.decision),
      lsn,
      payload.decidedBy,
      dateToMs(payload.decidedAt, "approval.decidedAt"),
      payload.decision,
      tokenJson,
      tokenId,
      payload.requestId,
    );
    if (result.changes !== 1) {
      throw new Error(`approval.decided has no pending request: ${payload.requestId}`);
    }
    const row = selectByIdStmt.get(payload.requestId);
    if (row === undefined) {
      throw new Error(`approval.decided update produced no row: ${payload.requestId}`);
    }
    return rowToFolded(row);
  };

  const applyEvent = (event: ApprovalProjectionEvent): FoldedApprovalRow | null => {
    const decoded = parseApprovalEvent(event);
    if (decoded === null) return null;
    if (decoded.kind === "approval_requested") {
      return applyRequested(decoded.payload, event.lsn, { replay: false });
    }
    return applyDecided(decoded.payload, event.lsn);
  };

  const applyReplayEvent = (event: ApprovalProjectionEvent): FoldedApprovalRow | null => {
    const decoded = parseApprovalEvent(event);
    if (decoded === null) return null;
    if (decoded.kind === "approval_requested") {
      return applyRequested(decoded.payload, event.lsn, { replay: true });
    }
    return applyDecided(decoded.payload, event.lsn);
  };

  const rebuildTransaction = db.transaction(
    (eventLog: EventLog): ApprovalProjectionRebuildResult => {
      clearStmt.run();
      let cursor = 0;
      let eventsApplied = 0;
      let highestLsn = 0;
      // TODO(approval-replay-repair): add offline repair or quarantine for
      // logs that fail replay-time approval invariants.
      for (;;) {
        const rows = eventLog.readFromLsn(cursor, APPROVAL_EVENT_BATCH_SIZE);
        if (rows.length === 0) break;
        for (const row of rows) {
          cursor = row.lsn;
          highestLsn = Math.max(highestLsn, row.lsn);
          if (applyReplayEvent(row) !== null) {
            eventsApplied += 1;
          }
        }
        if (rows.length < APPROVAL_EVENT_BATCH_SIZE) break;
      }
      return { eventsApplied, highestLsn: lsnFromV1Number(highestLsn) };
    },
  );
  const pendingByThreadSnapshotTransaction = db.transaction(
    (threadId: ThreadId): ApprovalPendingByThreadSnapshot => {
      const rows = listRows(
        db,
        { threadId, status: "pending" },
        {
          limit: MAX_ROUTE_APPROVAL_LIST_ITEMS + 1,
        },
      );
      if (rows.length > MAX_ROUTE_APPROVAL_LIST_ITEMS) {
        const count = pendingCountByThread(threadId);
        throw new ApprovalPendingSnapshotOverflowError(threadId, count);
      }
      const headLsn = latestHeadLsnByThreadStmt.get(threadId)?.headLsn ?? null;
      return {
        rows: rows.map(rowToFolded),
        headLsn: headLsn === null ? null : lsnFromV1Number(headLsn),
      };
    },
  );

  return {
    sharesProvenance(candidateDb: Database.Database, _eventLog: EventLog): boolean {
      return candidateDb === db;
    },
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
    listPage(
      filter: ApprovalListFilter | undefined,
      page: ApprovalListPageOptions,
    ): ApprovalListPage {
      if (!Number.isSafeInteger(page.limit) || page.limit < 1) {
        throw new Error("approval list page limit must be a positive safe integer");
      }
      const rows = listRows(db, filter, { ...page, limit: page.limit + 1 });
      const hasMore = rows.length > page.limit;
      const selectedRows = hasMore ? rows.slice(0, page.limit) : rows;
      const nextCursor = hasMore
        ? lsnFromV1Number(selectedRows[selectedRows.length - 1]?.headLsn ?? 0)
        : undefined;
      return {
        rows: selectedRows.map(rowToFolded),
        ...(nextCursor === undefined ? {} : { nextCursor }),
      };
    },
    countPendingByThread(threadId: ThreadId): number {
      return pendingCountByThread(threadId);
    },
    listPendingByThread(threadId: ThreadId): readonly FoldedApprovalRow[] {
      return listRows(
        db,
        { threadId, status: "pending" },
        {
          limit: MAX_ROUTE_APPROVAL_LIST_ITEMS,
        },
      ).map(rowToFolded);
    },
    latestHeadLsnByThread(threadId: ThreadId): EventLsn | null {
      const row = latestHeadLsnByThreadStmt.get(threadId);
      if (row === undefined || row.headLsn === null) return null;
      return lsnFromV1Number(row.headLsn);
    },
    pendingByThreadSnapshot(threadId: ThreadId): ApprovalPendingByThreadSnapshot {
      return pendingByThreadSnapshotTransaction.deferred(threadId);
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
      if (folded !== null) {
        throw new Error(`duplicate approval.requested event for ${requestId}`);
      }
      folded = {
        approval: approvalFromRequested(decoded.payload),
        headLsn: lsnFromV1Number(row.lsn),
      };
      continue;
    }
    if (folded === null || folded.approval.status !== "pending") {
      throw new Error(`approval.decided event has no pending request for ${requestId}`);
    }
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

function listRows(
  db: Database.Database,
  filter: ApprovalListFilter | undefined,
  page?: ApprovalListPageOptions,
): ApprovalDbRow[] {
  const clauses: string[] = [];
  const params: (number | string)[] = [];
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
  if (page?.afterHeadLsn !== undefined) {
    clauses.push("head_lsn > ?");
    params.push(page.afterHeadLsn);
  }
  const where = clauses.length === 0 ? "" : `WHERE ${clauses.join(" AND ")}`;
  const limit = page === undefined ? "" : " LIMIT ?";
  if (page !== undefined) params.push(page.limit);
  return db
    .prepare<(number | string)[], ApprovalDbRow>(
      `${approvalSelectSql(where)} ORDER BY head_lsn ASC${limit}`,
    )
    .all(...params);
}

function approvalSelectSql(where: string): string {
  return `SELECT approval_id AS approvalId, status, head_lsn AS headLsn,
                 claim, scope, risk_class AS riskClass, thread_id AS threadId,
                 task_id AS taskId, receipt_id AS receiptId,
                 requested_by AS requestedBy, requested_at_ms AS requestedAtMs,
                 decided_by AS decidedBy, decided_at_ms AS decidedAtMs,
                 decision, token, token_id AS tokenId
          FROM pending_approvals ${where}`;
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

function approvalTokenIdColumn(payload: ApprovalDecidedAuditPayload): string | null {
  const suppliedApprovalToken = payload.decision === "approve" ? payload.token : undefined;
  return suppliedApprovalToken?.tokenId ?? null;
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
      throw new Error(`pending_approvals decision fields are incomplete for ${row.approvalId}`);
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
