import {
  type EventLsn,
  lsnFromV1Number,
  parseLsn,
  type ThreadCreatedAuditPayload,
  type ThreadId,
  threadAuditPayloadFromJsonValue,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import type { EventLog } from "../../event-log/index.ts";
import { type ThreadStateStore, threadAuditKindForEventType } from "../../threads/projections.ts";
import { type ApprovalProjectionRebuildResult, createApprovalProjection } from "../projections.ts";

export type { ApprovalProjectionRebuildResult } from "../projections.ts";

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
  db: Database.Database,
  eventLog: EventLog,
  threadStateStore: ThreadStateStore,
): ApprovalProjectionRebuildResult {
  assertThreadProjectionReady(eventLog, threadStateStore);
  return createApprovalProjection(db).rebuildFromLog(eventLog);
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
        const parsed = JSON.parse(record.payload.toString("utf8")) as unknown;
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
