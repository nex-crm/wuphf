import { DatabaseSync } from "node:sqlite";
import {
  type EventLsn,
  lsnFromV1Number,
  parseLsn,
  type ThreadCreatedAuditPayload,
  type ThreadId,
  threadAuditPayloadFromJsonValue,
} from "@wuphf/protocol";

import { createEventLog, type EventLog, runMigrations } from "../../event-log/index.ts";
import { createTransaction } from "../../internal/sqlite-transaction.ts";
import {
  createThreadStateStore,
  type ThreadStateStore,
  threadAuditKindForEventType,
} from "../../threads/projections.ts";
import { type ApprovalProjectionRebuildResult, createApprovalProjection } from "../projections.ts";

export type { ApprovalProjectionRebuildResult } from "../projections.ts";

export interface ApprovalProjectionSnapshotRow {
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

export interface ApprovalReplayEventRow {
  readonly lsn: number;
  readonly tsMs: number;
  readonly type: string;
  readonly payload: Uint8Array;
}

const THREAD_PROJECTION_CHECK_BATCH_SIZE = 500;

export interface ApprovalRebuildThreadProjectionNotReadyArgs {
  readonly expectedThreadCount: number;
  readonly actualThreadCount: number;
  readonly expectedHeadLsn: EventLsn;
  readonly actualHeadLsn: EventLsn;
}

export class ApprovalRebuildThreadProjectionNotReadyError extends Error {
  override readonly name = "ApprovalRebuildThreadProjectionNotReadyError";
  readonly expectedThreadCount: number;
  readonly actualThreadCount: number;
  readonly expectedHeadLsn: EventLsn;
  readonly actualHeadLsn: EventLsn;

  constructor(args: ApprovalRebuildThreadProjectionNotReadyArgs) {
    super(
      `approval rebuild requires rebuilt threads projection: expected ${args.expectedThreadCount} threads through ${args.expectedHeadLsn}, got ${args.actualThreadCount} through ${args.actualHeadLsn}`,
    );
    this.expectedThreadCount = args.expectedThreadCount;
    this.actualThreadCount = args.actualThreadCount;
    this.expectedHeadLsn = args.expectedHeadLsn;
    this.actualHeadLsn = args.actualHeadLsn;
  }
}

export function rebuildApprovalsProjectionFromLog(
  db: DatabaseSync,
  eventLog: EventLog,
  threadStateStore: ThreadStateStore,
): ApprovalProjectionRebuildResult {
  assertThreadProjectionReady(eventLog, threadStateStore);
  return createApprovalProjection(db).rebuildFromLog(eventLog);
}

export function snapshotApprovalsProjection(
  db: DatabaseSync,
): readonly ApprovalProjectionSnapshotRow[] {
  return db
    .prepare(
      `SELECT approval_id AS approvalId, status, head_lsn AS headLsn, claim, scope,
              risk_class AS riskClass, thread_id AS threadId, task_id AS taskId,
              receipt_id AS receiptId, requested_by AS requestedBy,
              requested_at_ms AS requestedAtMs, decided_by AS decidedBy,
              decided_at_ms AS decidedAtMs, decision, token, token_id AS tokenId
       FROM pending_approvals
       ORDER BY approval_id ASC`,
    )
    .all() as unknown as ApprovalProjectionSnapshotRow[];
}

export function replayApprovalsProjectionSnapshot(
  rows: readonly ApprovalReplayEventRow[],
): readonly ApprovalProjectionSnapshotRow[] {
  const replayDb = new DatabaseSync(":memory:");
  try {
    runMigrations(replayDb);
    const insertEventStmt = replayDb.prepare(
      "INSERT INTO event_log (lsn, ts_ms, type, payload) VALUES (?, ?, ?, ?)",
    );
    const insertEvents = createTransaction(replayDb, () => {
      for (const row of rows) {
        insertEventStmt.run(row.lsn, row.tsMs, row.type, row.payload);
      }
    });
    insertEvents.immediate();
    const eventLog = createEventLog(replayDb);
    const threadStateStore = createThreadStateStore(replayDb);
    threadStateStore.rebuildFromLog(eventLog);
    rebuildApprovalsProjectionFromLog(replayDb, eventLog, threadStateStore);
    return snapshotApprovalsProjection(replayDb);
  } finally {
    replayDb.close();
  }
}

function assertThreadProjectionReady(eventLog: EventLog, threadStateStore: ThreadStateStore): void {
  const expected = expectedThreadProjectionState(eventLog);
  const rows = threadStateStore.list();
  const actualHeadLsn = rows.reduce((max, row) => Math.max(max, parseLsn(row.headLsn).localLsn), 0);
  if (rows.length !== expected.threadCount || actualHeadLsn !== expected.headLsn) {
    throw new ApprovalRebuildThreadProjectionNotReadyError({
      expectedThreadCount: expected.threadCount,
      actualThreadCount: rows.length,
      expectedHeadLsn: lsnFromV1Number(expected.headLsn),
      actualHeadLsn: lsnFromV1Number(actualHeadLsn),
    });
  }
}

function expectedThreadProjectionState(eventLog: EventLog): {
  readonly threadCount: number;
  readonly headLsn: number;
} {
  const threadIds = new Set<ThreadId>();
  let headLsn = 0;
  let cursor = 0;
  for (;;) {
    const batch = eventLog.readFromLsn(cursor, THREAD_PROJECTION_CHECK_BATCH_SIZE);
    if (batch.length === 0) break;
    for (const record of batch) {
      const kind = threadAuditKindForEventType(record.type);
      if (kind === null) continue;
      headLsn = record.lsn;
      if (kind === "thread_created") {
        const parsed = JSON.parse(new TextDecoder().decode(record.payload)) as unknown;
        const payload = threadAuditPayloadFromJsonValue(kind, parsed) as ThreadCreatedAuditPayload;
        threadIds.add(payload.threadId);
      }
    }
    const last = batch.at(-1);
    if (last === undefined) break;
    cursor = last.lsn;
    if (batch.length < THREAD_PROJECTION_CHECK_BATCH_SIZE) break;
  }
  return { threadCount: threadIds.size, headLsn };
}
