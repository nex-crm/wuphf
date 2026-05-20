import { type EventLsn, parseLsn } from "./event-lsn.ts";
import type {
  ApprovalStreamEventValidationResult,
  ThreadStreamEventValidationResult,
} from "./ipc.ts";
import {
  isApprovalRequestId,
  isReceiptId,
  isThreadId,
  type ReceiptValidationError,
} from "./receipt-types.ts";

export type {
  ApprovalDecision,
  ApprovalDecisionRecord,
  ApprovalRequest,
  ApprovalRequestStatus,
  ApprovalValidationError,
  ApprovalValidationResult,
} from "./approval-request.ts";
export {
  APPROVAL_REQUEST_SCHEMA_VERSION,
  APPROVAL_REQUEST_STATUS_VALUES,
  approvalRequestFromJson,
  approvalRequestFromJsonValue,
  approvalRequestToJson,
  approvalRequestToJsonValue,
  validateApprovalRequest,
} from "./approval-request.ts";
export type { JsonValue } from "./canonical-json.ts";
export { canonicalJSON } from "./canonical-json.ts";
export type { EventLsn } from "./event-lsn.ts";
export {
  compareLsn,
  isAfter,
  isBefore,
  isEqualLsn,
  lsnFromV1Number,
  nextLsn,
  parseLsn,
} from "./event-lsn.ts";
export type {
  ApprovalStreamEvent,
  ApprovalStreamEventKind,
  ApprovalStreamEventValidationError,
  ApprovalStreamEventValidationResult,
  ThreadStreamEvent,
  ThreadStreamEventKind,
  ThreadStreamEventValidationError,
  ThreadStreamEventValidationResult,
} from "./ipc.ts";
export {
  APPROVAL_DECISION_VALUES,
  APPROVAL_ROLE_VALUES,
  RISK_CLASS_VALUES,
} from "./receipt-literals.ts";
export type {
  ApprovalRequestId,
  IdempotencyKey,
  ProviderKind,
  ReceiptId,
  RiskClass,
  SignerIdentity,
  TaskId,
  ThreadId,
  ThreadSpecRevisionId,
  WriteId,
} from "./receipt-types.ts";
export {
  asApprovalRequestId,
  asIdempotencyKey,
  asProviderKind,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  asThreadSpecRevisionId,
  asWriteId,
  IDEMPOTENCY_KEY_RE,
  isApprovalRequestId,
  isIdempotencyKey,
  isProviderKind,
  isReceiptId,
  isSignerIdentity,
  isTaskId,
  isThreadId,
  isThreadSpecRevisionId,
  isWriteId,
  MINIMUM_PROTOCOL_VERSION_FOR_PROVIDER_KIND,
  PROVIDER_KIND_VALUES,
} from "./receipt-types.ts";
export type {
  ApprovalDecisionRequest,
  ApprovalDecisionResponse,
  ApprovalDecisionSummary,
  ApprovalGetResponse,
  ApprovalListResponse,
  ApprovalRequestCreateRequest,
  ApprovalRequestCreateResponse,
  ApprovalView,
  RouteEnvelopeSchemaVersion,
  RouteError,
  ThreadAttentionReason,
  ThreadBoardColumn,
  ThreadCreateRequest,
  ThreadCurrentSeat,
  ThreadEffectiveStatus,
  ThreadGetResponse,
  ThreadListResponse,
  ThreadMutationResponse,
  ThreadPinnedApprovalsResponse,
  ThreadSpecEditRequest,
  ThreadStatusChangeRequest,
  ThreadView,
} from "./route-envelopes.ts";
export {
  approvalDecisionRequestFromJson,
  approvalDecisionRequestToJsonValue,
  approvalDecisionResponseFromJson,
  approvalDecisionResponseToJsonValue,
  approvalGetResponseFromJson,
  approvalGetResponseToJsonValue,
  approvalListResponseFromJson,
  approvalListResponseToJsonValue,
  approvalRequestCreateRequestFromJson,
  approvalRequestCreateRequestToJsonValue,
  approvalRequestCreateResponseFromJson,
  approvalRequestCreateResponseToJsonValue,
  approvalViewFromJson,
  approvalViewToJsonValue,
  ROUTE_ENVELOPE_SCHEMA_VERSION,
  routeErrorFromJson,
  routeErrorToJsonValue,
  THREAD_ATTENTION_REASON_VALUES,
  THREAD_BOARD_COLUMN_VALUES,
  THREAD_CURRENT_SEAT_VALUES,
  THREAD_EFFECTIVE_STATUS_VALUES,
  threadCreateRequestFromJson,
  threadCreateRequestToJsonValue,
  threadGetResponseFromJson,
  threadGetResponseToJsonValue,
  threadListResponseFromJson,
  threadListResponseToJsonValue,
  threadMutationResponseFromJson,
  threadMutationResponseToJsonValue,
  threadPinnedApprovalsResponseFromJson,
  threadPinnedApprovalsResponseToJsonValue,
  threadSpecEditRequestFromJson,
  threadSpecEditRequestToJsonValue,
  threadStatusChangeRequestFromJson,
  threadStatusChangeRequestToJsonValue,
  threadViewFromJson,
  threadViewToJsonValue,
  validateApprovalView,
} from "./route-envelopes.ts";
export type { Sha256Hex } from "./sha256.ts";
export { asSha256Hex, isSha256Hex, SHA256_HEX_RE } from "./sha256.ts";
export type {
  ApprovalClaim,
  ApprovalClaimId,
  ApprovalClaimJsonValue,
  ApprovalClaimKind,
  ApprovalScope,
  ApprovalScopeJsonValue,
  ApprovalTokenId,
  CostSpikeAcknowledgementClaim,
  CostSpikeAcknowledgementScope,
  CredentialGrantToAgentClaim,
  CredentialGrantToAgentScope,
  EndpointAllowlistExtensionClaim,
  EndpointAllowlistExtensionScope,
  ReceiptCoSignClaim,
  ReceiptCoSignScope,
  SignedApprovalToken,
  SignedApprovalTokenJsonValue,
  TimestampMs,
  WebAuthnAssertion,
  WebAuthnAssertionJsonValue,
} from "./signed-approval-token.ts";
export {
  APPROVAL_CLAIM_KIND_VALUES,
  APPROVAL_TOKEN_SCHEMA_VERSION,
  approvalClaimFromJson,
  approvalClaimToJsonValue,
  approvalScopeFromJson,
  approvalScopeToJsonValue,
  asApprovalClaimId,
  asApprovalTokenId,
  asTimestampMs,
  isApprovalClaimId,
  isApprovalTokenId,
  isReceiptCoSignClaim,
  isReceiptCoSignScope,
  isTimestampMs,
  signedApprovalTokenFromJson,
  signedApprovalTokenToJsonValue,
  webAuthnAssertionFromJson,
  webAuthnAssertionToJsonValue,
} from "./signed-approval-token.ts";
export type {
  Thread,
  ThreadExternalRefs,
  ThreadSpecRevision,
  ThreadStatus,
  ThreadValidationError,
  ThreadValidationResult,
} from "./thread-browser.ts";
export {
  THREAD_STATUS_VALUES,
  threadExternalRefsFromJsonValue,
  threadExternalRefsToJsonValue,
  threadFromJson,
  threadFromJsonValue,
  threadSpecRevisionFromJsonValue,
  threadSpecRevisionToJsonValue,
  threadToJson,
  threadToJsonValue,
  validateThread,
  validateThreadExternalRefs,
  validateThreadSpecRevision,
} from "./thread-browser.ts";

const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;
const THREAD_STREAM_EVENT_KIND_VALUES = [
  "thread.created",
  "thread.updated",
  "thread.pinned_approvals.changed",
] as const;
const APPROVAL_STREAM_EVENT_KIND_VALUES = ["approval.requested", "approval.decided"] as const;
const THREAD_STREAM_EVENT_KEYS: ReadonlySet<string> = new Set([
  "id",
  "kind",
  "emittedAt",
  "receiptId",
  "payload",
]);
const APPROVAL_STREAM_EVENT_KEYS: ReadonlySet<string> = THREAD_STREAM_EVENT_KEYS;
const THREAD_INVALIDATION_PAYLOAD_KEYS: ReadonlySet<string> = new Set(["threadId", "headLsn"]);
const APPROVAL_INVALIDATION_PAYLOAD_KEYS: ReadonlySet<string> = new Set([
  "requestId",
  "threadId",
  "headLsn",
]);

export function validateThreadStreamEvent(input: unknown): ThreadStreamEventValidationResult {
  const errors: ReceiptValidationError[] = [];
  if (!isRecord(input)) {
    addValidationError(errors, "", "must be an object");
    return { ok: false, errors };
  }
  validateKnownKeys(input, "", THREAD_STREAM_EVENT_KEYS, errors);
  validateRequiredField(input, "id", "", errors, (value, path) => {
    validateNonEmptyString(value, path, errors);
  });
  validateRequiredField(input, "kind", "", errors, (value, path) => {
    if (!isLiteralString(value, THREAD_STREAM_EVENT_KIND_VALUES)) {
      addValidationError(errors, path, "must be a thread stream event kind");
    }
  });
  validateRequiredField(input, "emittedAt", "", errors, (value, path) => {
    validateIsoInstant(value, path, errors);
  });
  validateOptionalField(input, "receiptId", "", errors, (value, path) => {
    if (!isReceiptId(value)) {
      addValidationError(errors, path, "must be an uppercase ULID ReceiptId");
    }
  });
  validateRequiredField(input, "payload", "", errors, (value, path) => {
    validateThreadInvalidationPayload(value, path, errors);
  });
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateApprovalStreamEvent(input: unknown): ApprovalStreamEventValidationResult {
  const errors: ReceiptValidationError[] = [];
  if (!isRecord(input)) {
    addValidationError(errors, "", "must be an object");
    return { ok: false, errors };
  }
  validateKnownKeys(input, "", APPROVAL_STREAM_EVENT_KEYS, errors);
  validateRequiredField(input, "id", "", errors, (value, path) => {
    validateNonEmptyString(value, path, errors);
  });
  validateRequiredField(input, "kind", "", errors, (value, path) => {
    if (!isLiteralString(value, APPROVAL_STREAM_EVENT_KIND_VALUES)) {
      addValidationError(errors, path, "must be an approval stream event kind");
    }
  });
  validateRequiredField(input, "emittedAt", "", errors, (value, path) => {
    validateIsoInstant(value, path, errors);
  });
  validateOptionalField(input, "receiptId", "", errors, (value, path) => {
    if (!isReceiptId(value)) {
      addValidationError(errors, path, "must be an uppercase ULID ReceiptId");
    }
  });
  validateRequiredField(input, "payload", "", errors, (value, path) => {
    validateApprovalInvalidationPayload(value, path, errors);
  });
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

function validateThreadInvalidationPayload(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isRecord(value)) {
    addValidationError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, THREAD_INVALIDATION_PAYLOAD_KEYS, errors);
  validateRequiredField(value, "threadId", path, errors, (field, fieldPath) => {
    if (!isThreadId(field)) {
      addValidationError(errors, fieldPath, "must be an uppercase ULID ThreadId");
    }
  });
  validateRequiredField(value, "headLsn", path, errors, (field, fieldPath) => {
    validateEventLsn(field, fieldPath, errors);
  });
}

function validateApprovalInvalidationPayload(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isRecord(value)) {
    addValidationError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, APPROVAL_INVALIDATION_PAYLOAD_KEYS, errors);
  validateRequiredField(value, "requestId", path, errors, (field, fieldPath) => {
    if (!isApprovalRequestId(field)) {
      addValidationError(errors, fieldPath, "must be an uppercase ULID ApprovalRequestId");
    }
  });
  validateOptionalField(value, "threadId", path, errors, (field, fieldPath) => {
    if (!isThreadId(field)) {
      addValidationError(errors, fieldPath, "must be an uppercase ULID ThreadId");
    }
  });
  validateRequiredField(value, "headLsn", path, errors, (field, fieldPath) => {
    validateEventLsn(field, fieldPath, errors);
  });
}

function validateRequiredField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  errors: ReceiptValidationError[],
  validator: (value: unknown, path: string) => void,
): void {
  const fieldPath = pointer(basePath, key);
  if (!hasOwn(record, key)) {
    addValidationError(errors, fieldPath, "is required");
    return;
  }
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    addValidationError(errors, fieldPath, "must be a data property");
    return;
  }
  if (descriptor.value === undefined) {
    addValidationError(errors, fieldPath, "is required");
    return;
  }
  validator(descriptor.value, fieldPath);
}

function validateOptionalField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  errors: ReceiptValidationError[],
  validator: (value: unknown, path: string) => void,
): void {
  if (!hasOwn(record, key)) return;
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  const fieldPath = pointer(basePath, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    addValidationError(errors, fieldPath, "must be a data property");
    return;
  }
  if (descriptor.value === undefined) return;
  validator(descriptor.value, fieldPath);
}

function validateKnownKeys(
  record: Readonly<Record<string, unknown>>,
  path: string,
  allowed: ReadonlySet<string>,
  errors: ReceiptValidationError[],
): void {
  for (const key of Object.keys(record)) {
    if (!allowed.has(key)) {
      addValidationError(errors, pointer(path, key), "is not allowed");
    }
  }
}

function validateNonEmptyString(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (typeof value !== "string" || value.length === 0) {
    addValidationError(errors, path, "must be a non-empty string");
  }
}

function validateIsoInstant(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (typeof value !== "string" || !ISO_DATE_RE.test(value)) {
    addValidationError(errors, path, "must be an ISO 8601 string");
    return;
  }
  const date = new Date(value);
  if (!Number.isFinite(date.valueOf()) || date.toISOString() !== value) {
    addValidationError(errors, path, "must be a valid ISO 8601 instant");
  }
}

function validateEventLsn(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (typeof value !== "string") {
    addValidationError(errors, path, "must be an EventLsn string");
    return;
  }
  try {
    parseLsn(value as EventLsn);
  } catch (error) {
    addValidationError(
      errors,
      path,
      error instanceof Error ? error.message : "must be a valid EventLsn",
    );
  }
}

function isLiteralString<const T extends string>(
  value: unknown,
  allowed: readonly T[],
): value is T {
  return typeof value === "string" && allowed.includes(value as T);
}

function isRecord(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasOwn(record: Readonly<Record<string, unknown>>, key: string): boolean {
  return Object.hasOwn(record, key);
}

function pointer(basePath: string, key: string): string {
  return `${basePath}/${key}`;
}

function addValidationError(errors: ReceiptValidationError[], path: string, message: string): void {
  errors.push({ path, message });
}
