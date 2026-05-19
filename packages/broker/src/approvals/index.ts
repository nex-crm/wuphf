// Public surface of `@wuphf/broker/approvals`.

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
export type { ApprovalProjectionRebuildResult as ApprovalReplayRebuildResult } from "./replay-check/index.ts";
export { rebuildApprovalsProjectionFromLog } from "./replay-check/index.ts";
