// Public surface of `@wuphf/broker/approvals`.

import type { DatabaseSync } from "node:sqlite";

import type { EventLog as ApprovalEventLog } from "../event-log/index.ts";
import {
  type ApprovalAppender as ApprovalAppenderInstance,
  type ApprovalAppenderOptions,
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

export interface ApprovalSubsystemOptions {
  readonly threadRefValidator?: ApprovalAppenderOptions["threadRefValidator"];
}

export function createApprovalSubsystem(
  db: DatabaseSync,
  eventLog: ApprovalEventLog,
  options: ApprovalSubsystemOptions = {},
): ApprovalSubsystem {
  const projection = createApprovalProjection(db);
  return {
    appender: createApprovalAppender(db, eventLog, projection, {
      ...(options.threadRefValidator === undefined
        ? {}
        : { threadRefValidator: options.threadRefValidator }),
    }),
    projection,
  };
}

export type {
  ApprovalAppender,
  ApprovalAppenderOptions,
  ApprovalAppendResult,
  IdempotentApprovalAppendResult,
  IdempotentApprovalDecisionArgs,
  IdempotentApprovalRequestArgs,
} from "./appender.ts";
export {
  ApprovalDecisionInvalidError,
  ApprovalIdempotencyConflictError,
  ApprovalPendingLimitExceededError,
  ApprovalRequestAlreadyDecidedError,
  ApprovalRequestAlreadyExistsError,
  ApprovalRequestNotFoundError,
  ApprovalThreadNotFoundError,
  ApprovalTokenAlreadyUsedError,
  ApprovalTokenIssuedToMismatchError,
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
  ApprovalPendingByThreadSnapshot,
  ApprovalProjection,
  ApprovalProjectionEvent,
  ApprovalProjectionRebuildResult,
  FoldedApprovalRow,
} from "./projections.ts";
export {
  ApprovalPendingSnapshotOverflowError,
  ApprovalReplayPendingLimitExceededError,
  ApprovalReplayThreadNotFoundError,
  approvalFromRequested,
  approvalWithDecision,
  createApprovalProjection,
  foldApprovalFromLog,
  statusForDecision,
} from "./projections.ts";
export type { ApprovalProjectionSnapshotRow, ApprovalReplayEventRow } from "./rebuild/index.ts";
export {
  ApprovalRebuildThreadProjectionNotReadyError,
  rebuildApprovalsProjectionFromLog,
  replayApprovalsProjectionSnapshot,
  snapshotApprovalsProjection,
} from "./rebuild/index.ts";
