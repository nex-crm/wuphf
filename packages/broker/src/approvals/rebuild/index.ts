import type Database from "better-sqlite3";

import type { EventLog } from "../../event-log/index.ts";
import { type ApprovalProjectionRebuildResult, createApprovalProjection } from "../projections.ts";

export type { ApprovalProjectionRebuildResult } from "../projections.ts";

export function rebuildApprovalsProjectionFromLog(
  db: Database.Database,
  eventLog: EventLog,
): ApprovalProjectionRebuildResult {
  return createApprovalProjection(db).rebuildFromLog(eventLog);
}
