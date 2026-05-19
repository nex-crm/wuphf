import type Database from "better-sqlite3";
import BetterSqlite3 from "better-sqlite3";

import { createEventLog, type EventLog, runMigrations } from "../../event-log/index.ts";
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
  readonly payload: Buffer;
}

export function rebuildApprovalsProjectionFromLog(
  db: Database.Database,
  eventLog: EventLog,
): ApprovalProjectionRebuildResult {
  return createApprovalProjection(db).rebuildFromLog(eventLog);
}

export function snapshotApprovalsProjection(
  db: Database.Database,
): readonly ApprovalProjectionSnapshotRow[] {
  return db
    .prepare<[], ApprovalProjectionSnapshotRow>(
      `SELECT approval_id AS approvalId, status, head_lsn AS headLsn, claim, scope,
              risk_class AS riskClass, thread_id AS threadId, task_id AS taskId,
              receipt_id AS receiptId, requested_by AS requestedBy,
              requested_at_ms AS requestedAtMs, decided_by AS decidedBy,
              decided_at_ms AS decidedAtMs, decision, token, token_id AS tokenId
       FROM pending_approvals
       ORDER BY approval_id ASC`,
    )
    .all();
}

export function replayApprovalsProjectionSnapshot(
  rows: readonly ApprovalReplayEventRow[],
): readonly ApprovalProjectionSnapshotRow[] {
  const replayDb = new BetterSqlite3(":memory:");
  try {
    runMigrations(replayDb);
    const insertEventStmt = replayDb.prepare<[number, number, string, Buffer]>(
      "INSERT INTO event_log (lsn, ts_ms, type, payload) VALUES (?, ?, ?, ?)",
    );
    const insertEvents = replayDb.transaction(() => {
      for (const row of rows) {
        insertEventStmt.run(row.lsn, row.tsMs, row.type, Buffer.from(row.payload));
      }
    });
    insertEvents.immediate();
    rebuildApprovalsProjectionFromLog(replayDb, createEventLog(replayDb));
    return snapshotApprovalsProjection(replayDb);
  } finally {
    replayDb.close();
  }
}
