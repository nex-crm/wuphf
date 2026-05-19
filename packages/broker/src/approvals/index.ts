// Public surface of `@wuphf/broker/approvals`.

import type Database from "better-sqlite3";

import type { EventLog as ApprovalEventLog } from "../event-log/index.ts";
import {
  type ApprovalAppender as ApprovalAppenderInstance,
  createApprovalAppender,
} from "./appender.ts";
import {
  type ApprovalProjection as ApprovalProjectionInstance,
  createApprovalProjection,
} from "./projections.ts";

export type {
  AppendArgs,
  EventLog,
  EventLogRecord,
  EventType,
  OpenDatabaseArgs,
} from "../event-log/index.ts";
export {
  CURRENT_SCHEMA_VERSION,
  createEventLog,
  openDatabase,
  runMigrations,
} from "../event-log/index.ts";

export interface ApprovalSubsystem {
  readonly appender: ApprovalAppenderInstance;
  readonly projection: ApprovalProjectionInstance;
}

export function createApprovalSubsystem(
  db: Database.Database,
  eventLog: ApprovalEventLog,
): ApprovalSubsystem {
  const projection = createApprovalProjection(db);
  return {
    appender: createApprovalAppender(db, eventLog, projection),
    projection,
  };
}

export type {
  ApprovalAppender,
  ApprovalAppendResult,
  IdempotentApprovalAppendResult,
  IdempotentApprovalDecisionArgs,
  IdempotentApprovalRequestArgs,
} from "./appender.ts";
export {
  ApprovalDecisionInvalidError,
  ApprovalIdempotencyConflictError,
  ApprovalRequestAlreadyDecidedError,
  ApprovalRequestAlreadyExistsError,
  ApprovalRequestNotFoundError,
  ApprovalTokenAlreadyUsedError,
  createApprovalAppender,
} from "./appender.ts";
export type {
  ApprovalCommand,
  ApprovalIdempotencyParseError,
  ParsedApprovalIdempotencyKey,
} from "./idempotency.ts";
export {
  APPROVAL_COMMAND_VALUES,
  DEFAULT_APPROVAL_IDEMPOTENCY_TTL_MS,
  parseApprovalIdempotencyKey,
} from "./idempotency.ts";
export type {
  ApprovalListFilter,
  ApprovalListPage,
  ApprovalListPageOptions,
  ApprovalProjection,
  ApprovalProjectionEvent,
  ApprovalProjectionRebuildResult,
  FoldedApprovalRow,
} from "./projections.ts";
export {
  approvalFromRequested,
  approvalWithDecision,
  createApprovalProjection,
  foldApprovalFromLog,
  statusForDecision,
} from "./projections.ts";
export { rebuildApprovalsProjectionFromLog } from "./rebuild/index.ts";
