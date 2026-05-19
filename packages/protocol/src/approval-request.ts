import { canonicalJSON } from "./canonical-json.ts";
import {
  type ApprovalEvent,
  type ApprovalRequestId,
  asApprovalRequestId,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  asThreadId,
  isApprovalRequestId,
  isReceiptId,
  isSignerIdentity,
  isTaskId,
  isThreadId,
  type ReceiptId,
  type ReceiptValidationError,
  type ReceiptValidationResult,
  type RiskClass,
  type SignerIdentity,
  type TaskId,
  type ThreadId,
} from "./receipt.ts";
import { APPROVAL_DECISION_VALUES, RISK_CLASS_VALUES } from "./receipt-literals.ts";
import {
  addError,
  assertKnownKeys,
  formatValidationErrors,
  hasOwn,
  isRecord,
  omitUndefined,
  pointer,
  recordValue,
  requireRecord,
} from "./receipt-utils.ts";
import {
  type ApprovalClaim,
  type ApprovalScope,
  approvalClaimFromJson,
  approvalClaimToJsonValue,
  approvalScopeFromJson,
  approvalScopeToJsonValue,
  type SignedApprovalToken,
  signedApprovalTokenFromJson,
  signedApprovalTokenToJsonValue,
} from "./signed-approval-token.ts";

export const APPROVAL_REQUEST_SCHEMA_VERSION = 1;

export const APPROVAL_REQUEST_STATUS_VALUES = [
  "pending",
  "approved",
  "rejected",
  "abstained",
] as const;

export type ApprovalRequestStatus = (typeof APPROVAL_REQUEST_STATUS_VALUES)[number];
export type ApprovalDecision = ApprovalEvent["decision"];

export interface ApprovalDecisionRecord {
  readonly decision: ApprovalDecision;
  readonly decidedBy: SignerIdentity;
  readonly decidedAt: Date;
  readonly token?: SignedApprovalToken | undefined;
}

export interface ApprovalRequest {
  readonly id: ApprovalRequestId;
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
  readonly riskClass: RiskClass;
  readonly threadId?: ThreadId | undefined;
  readonly taskId?: TaskId | undefined;
  readonly receiptId?: ReceiptId | undefined;
  readonly requestedBy: SignerIdentity;
  readonly requestedAt: Date;
  readonly status: ApprovalRequestStatus;
  readonly decision?: ApprovalDecisionRecord | undefined;
  readonly schemaVersion: 1;
}

export interface ApprovalRequestedAuditPayload {
  readonly requestId: ApprovalRequestId;
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
  readonly riskClass: RiskClass;
  readonly threadId?: ThreadId | undefined;
  readonly taskId?: TaskId | undefined;
  readonly receiptId?: ReceiptId | undefined;
  readonly requestedBy: SignerIdentity;
  readonly requestedAt: Date;
}

export interface ApprovalDecidedAuditPayload {
  readonly requestId: ApprovalRequestId;
  readonly decision: ApprovalDecision;
  readonly decidedBy: SignerIdentity;
  readonly decidedAt: Date;
  readonly token?: SignedApprovalToken | undefined;
}

export type ApprovalAuditPayload = ApprovalRequestedAuditPayload | ApprovalDecidedAuditPayload;

export type ApprovalAuditEventKind = "approval_requested" | "approval_decided";

export type ApprovalValidationError = ReceiptValidationError;
export type ApprovalValidationResult = ReceiptValidationResult;

type ApprovalRequestWire = Readonly<
  Record<
    | "request_id"
    | "claim"
    | "scope"
    | "risk_class"
    | "thread_id"
    | "task_id"
    | "receipt_id"
    | "requested_by"
    | "requested_at"
    | "status"
    | "decision"
    | "schema_version",
    unknown
  >
>;

type ApprovalDecisionRecordWire = Readonly<
  Record<"decision" | "decided_by" | "decided_at" | "token", unknown>
>;

const APPROVAL_REQUEST_KEYS_TUPLE = [
  "id",
  "claim",
  "scope",
  "riskClass",
  "threadId",
  "taskId",
  "receiptId",
  "requestedBy",
  "requestedAt",
  "status",
  "decision",
  "schemaVersion",
] as const satisfies readonly (keyof ApprovalRequest)[];
const APPROVAL_REQUEST_KEYS: ReadonlySet<string> = new Set<string>(APPROVAL_REQUEST_KEYS_TUPLE);

const APPROVAL_DECISION_RECORD_KEYS_TUPLE = [
  "decision",
  "decidedBy",
  "decidedAt",
  "token",
] as const satisfies readonly (keyof ApprovalDecisionRecord)[];
const APPROVAL_DECISION_RECORD_KEYS: ReadonlySet<string> = new Set<string>(
  APPROVAL_DECISION_RECORD_KEYS_TUPLE,
);

const APPROVAL_REQUESTED_AUDIT_PAYLOAD_KEYS_TUPLE = [
  "requestId",
  "claim",
  "scope",
  "riskClass",
  "threadId",
  "taskId",
  "receiptId",
  "requestedBy",
  "requestedAt",
] as const satisfies readonly (keyof ApprovalRequestedAuditPayload)[];
const APPROVAL_REQUESTED_AUDIT_PAYLOAD_KEYS: ReadonlySet<string> = new Set<string>(
  APPROVAL_REQUESTED_AUDIT_PAYLOAD_KEYS_TUPLE,
);

const APPROVAL_DECIDED_AUDIT_PAYLOAD_KEYS_TUPLE = [
  "requestId",
  "decision",
  "decidedBy",
  "decidedAt",
  "token",
] as const satisfies readonly (keyof ApprovalDecidedAuditPayload)[];
const APPROVAL_DECIDED_AUDIT_PAYLOAD_KEYS: ReadonlySet<string> = new Set<string>(
  APPROVAL_DECIDED_AUDIT_PAYLOAD_KEYS_TUPLE,
);

const APPROVAL_REQUEST_WIRE_KEYS_TUPLE = [
  "request_id",
  "claim",
  "scope",
  "risk_class",
  "thread_id",
  "task_id",
  "receipt_id",
  "requested_by",
  "requested_at",
  "status",
  "decision",
  "schema_version",
] as const satisfies readonly (keyof ApprovalRequestWire)[];
const APPROVAL_REQUEST_WIRE_KEYS: ReadonlySet<string> = new Set<string>(
  APPROVAL_REQUEST_WIRE_KEYS_TUPLE,
);

const APPROVAL_DECISION_RECORD_WIRE_KEYS_TUPLE = [
  "decision",
  "decided_by",
  "decided_at",
  "token",
] as const satisfies readonly (keyof ApprovalDecisionRecordWire)[];
const APPROVAL_DECISION_RECORD_WIRE_KEYS: ReadonlySet<string> = new Set<string>(
  APPROVAL_DECISION_RECORD_WIRE_KEYS_TUPLE,
);

const APPROVAL_REQUEST_STATUS_SET: ReadonlySet<string> = new Set<string>(
  APPROVAL_REQUEST_STATUS_VALUES,
);
const APPROVAL_DECISION_SET: ReadonlySet<string> = new Set<string>(APPROVAL_DECISION_VALUES);
const APPROVAL_AUDIT_EVENT_KIND_SET: ReadonlySet<string> = new Set<string>([
  "approval_requested",
  "approval_decided",
]);
const RISK_CLASS_SET: ReadonlySet<string> = new Set<string>(RISK_CLASS_VALUES);
const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;
const TEXT_ENCODER = new TextEncoder();

export function approvalRequestToJson(request: ApprovalRequest): string {
  const validation = validateApprovalRequest(request);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return canonicalJSON(approvalRequestToJsonValue(request));
}

export function approvalRequestFromJson(json: string): ApprovalRequest {
  return approvalRequestFromJsonValue(JSON.parse(json));
}

export function approvalRequestToJsonValue(request: ApprovalRequest): Record<string, unknown> {
  return omitUndefined({
    request_id: request.id,
    claim: approvalClaimToJsonValue(request.claim),
    scope: approvalScopeToJsonValue(request.scope),
    risk_class: request.riskClass,
    thread_id: request.threadId,
    task_id: request.taskId,
    receipt_id: request.receiptId,
    requested_by: request.requestedBy,
    requested_at: request.requestedAt.toISOString(),
    status: request.status,
    decision:
      request.decision === undefined
        ? undefined
        : approvalDecisionRecordToJsonValue(request.decision),
    schema_version: request.schemaVersion,
  });
}

export function approvalRequestFromJsonValue(value: unknown): ApprovalRequest {
  const record = requireRecord(value, "approvalRequest");
  assertKnownKeys(record, "approvalRequest", APPROVAL_REQUEST_WIRE_KEYS);
  const threadId = optionalStringFromJson(record, "thread_id", "approvalRequest");
  const taskId = optionalStringFromJson(record, "task_id", "approvalRequest");
  const receiptId = optionalStringFromJson(record, "receipt_id", "approvalRequest");
  const request: ApprovalRequest = {
    id: asApprovalRequestIdAt(
      requiredStringFromJson(record, "request_id", "approvalRequest"),
      "approvalRequest.request_id",
    ),
    claim: approvalClaimFromJson(
      requiredFieldFromJson(record, "claim", "approvalRequest"),
      "approvalRequest.claim",
    ),
    scope: approvalScopeFromJson(
      requiredFieldFromJson(record, "scope", "approvalRequest"),
      "approvalRequest.scope",
    ),
    riskClass: riskClassFromJson(
      requiredStringFromJson(record, "risk_class", "approvalRequest"),
      "approvalRequest.risk_class",
    ),
    ...(threadId === undefined
      ? {}
      : { threadId: asThreadIdAt(threadId, "approvalRequest.thread_id") }),
    ...(taskId === undefined ? {} : { taskId: asTaskIdAt(taskId, "approvalRequest.task_id") }),
    ...(receiptId === undefined
      ? {}
      : { receiptId: asReceiptIdAt(receiptId, "approvalRequest.receipt_id") }),
    requestedBy: asSignerIdentityAt(
      requiredStringFromJson(record, "requested_by", "approvalRequest"),
      "approvalRequest.requested_by",
    ),
    requestedAt: requiredDateFromJson(record, "requested_at", "approvalRequest"),
    status: approvalRequestStatusFromJson(
      requiredStringFromJson(record, "status", "approvalRequest"),
      "approvalRequest.status",
    ),
    schemaVersion: requiredSchemaVersionFromJson(record, "schema_version", "approvalRequest"),
  };
  const decision = optionalDecisionRecordFromJson(record, "decision", "approvalRequest");
  const decoded = decision === undefined ? request : { ...request, decision };
  const validation = validateApprovalRequest(decoded);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return decoded;
}

export function approvalDecisionRecordToJsonValue(
  decision: ApprovalDecisionRecord,
): Record<string, unknown> {
  return omitUndefined({
    decision: decision.decision,
    decided_by: decision.decidedBy,
    decided_at: decision.decidedAt.toISOString(),
    token:
      decision.token === undefined ? undefined : signedApprovalTokenToJsonValue(decision.token),
  });
}

export function approvalAuditPayloadToJsonValue(
  kind: ApprovalAuditEventKind,
  payload: ApprovalAuditPayload,
): Record<string, unknown> {
  if (kind === "approval_requested") {
    const requested = payload as ApprovalRequestedAuditPayload;
    return omitUndefined({
      requestId: requested.requestId,
      claim: approvalClaimToJsonValue(requested.claim),
      scope: approvalScopeToJsonValue(requested.scope),
      riskClass: requested.riskClass,
      threadId: requested.threadId,
      taskId: requested.taskId,
      receiptId: requested.receiptId,
      requestedBy: requested.requestedBy,
      requestedAt: requested.requestedAt.toISOString(),
    });
  }
  if (kind === "approval_decided") {
    const decided = payload as ApprovalDecidedAuditPayload;
    return omitUndefined({
      requestId: decided.requestId,
      decision: decided.decision,
      decidedBy: decided.decidedBy,
      decidedAt: decided.decidedAt.toISOString(),
      token:
        decided.token === undefined ? undefined : signedApprovalTokenToJsonValue(decided.token),
    });
  }
  throw new Error(unknownApprovalAuditEventKindMessage(kind));
}

export function approvalAuditPayloadFromJsonValue(
  kind: ApprovalAuditEventKind,
  value: unknown,
): ApprovalAuditPayload {
  if (kind === "approval_requested") return approvalRequestedAuditPayloadFromJsonValue(value);
  if (kind === "approval_decided") return approvalDecidedAuditPayloadFromJsonValue(value);
  throw new Error(unknownApprovalAuditEventKindMessage(kind));
}

export function approvalAuditPayloadToBytes(
  kind: ApprovalAuditEventKind,
  payload: ApprovalAuditPayload,
): Uint8Array {
  const validation = validateApprovalAuditPayloadForKind(kind, payload);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return TEXT_ENCODER.encode(canonicalJSON(approvalAuditPayloadToJsonValue(kind, payload)));
}

export function validateApprovalRequest(input: unknown): ApprovalValidationResult {
  const errors: ApprovalValidationError[] = [];
  validateApprovalRequestValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateApprovalDecisionRecord(input: unknown): ApprovalValidationResult {
  const errors: ApprovalValidationError[] = [];
  validateApprovalDecisionRecordValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateApprovalRequestedAuditPayload(input: unknown): ApprovalValidationResult {
  const errors: ApprovalValidationError[] = [];
  validateApprovalRequestedAuditPayloadValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateApprovalDecidedAuditPayload(input: unknown): ApprovalValidationResult {
  const errors: ApprovalValidationError[] = [];
  validateApprovalDecidedAuditPayloadValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateApprovalAuditPayloadForKind(
  kind: ApprovalAuditEventKind,
  payload: unknown,
): ApprovalValidationResult {
  if (kind === "approval_requested") return validateApprovalRequestedAuditPayload(payload);
  if (kind === "approval_decided") return validateApprovalDecidedAuditPayload(payload);
  throw new Error(unknownApprovalAuditEventKindMessage(kind));
}

export function isApprovalAuditEventKind(value: unknown): value is ApprovalAuditEventKind {
  return typeof value === "string" && APPROVAL_AUDIT_EVENT_KIND_SET.has(value);
}

function validateApprovalRequestValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, APPROVAL_REQUEST_KEYS, errors);
  validateRequired(value, "id", path, errors, validateApprovalRequestIdValue);
  validateRequired(value, "claim", path, errors, validateApprovalClaimValue);
  validateRequired(value, "scope", path, errors, validateApprovalScopeValue);
  validateRequired(value, "riskClass", path, errors, validateRiskClassValue);
  validateOptional(value, "threadId", path, errors, validateThreadIdValue);
  validateOptional(value, "taskId", path, errors, validateTaskIdValue);
  validateOptional(value, "receiptId", path, errors, validateReceiptIdValue);
  validateRequired(value, "requestedBy", path, errors, validateSignerIdentityValue);
  validateRequired(value, "requestedAt", path, errors, validateDateValue);
  validateRequired(value, "status", path, errors, validateApprovalRequestStatusValue);
  validateOptional(value, "decision", path, errors, validateApprovalDecisionRecordValue);
  validateRequired(value, "schemaVersion", path, errors, validateSchemaVersionValue);
  validateClaimScopeBindingValue(
    recordValue(value, "claim"),
    recordValue(value, "scope"),
    pointer(path, "scope"),
    errors,
  );

  const status = recordValue(value, "status");
  const decision = recordValue(value, "decision");
  if (status === "pending" && decision !== undefined) {
    addError(errors, pointer(path, "decision"), "must be absent when status is pending");
  }
  if (isNonPendingApprovalRequestStatus(status) && decision === undefined) {
    addError(errors, pointer(path, "decision"), "is required when status is not pending");
  }
  if (isRecord(decision) && isApprovalRequestStatus(status)) {
    const decisionValue = recordValue(decision, "decision");
    if (
      isApprovalDecision(decisionValue) &&
      status !== approvalRequestStatusForDecision(decisionValue)
    ) {
      addError(errors, pointer(pointer(path, "decision"), "decision"), "must match status");
    }
  }
}

function validateApprovalDecisionRecordValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, APPROVAL_DECISION_RECORD_KEYS, errors);
  validateRequired(value, "decision", path, errors, validateApprovalDecisionValue);
  validateRequired(value, "decidedBy", path, errors, validateSignerIdentityValue);
  validateRequired(value, "decidedAt", path, errors, validateDateValue);
  validateOptional(value, "token", path, errors, validateSignedApprovalTokenValue);
}

function validateApprovalRequestedAuditPayloadValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, APPROVAL_REQUESTED_AUDIT_PAYLOAD_KEYS, errors);
  validateRequired(value, "requestId", path, errors, validateApprovalRequestIdValue);
  validateRequired(value, "claim", path, errors, validateApprovalClaimValue);
  validateRequired(value, "scope", path, errors, validateApprovalScopeValue);
  validateRequired(value, "riskClass", path, errors, validateRiskClassValue);
  validateOptional(value, "threadId", path, errors, validateThreadIdValue);
  validateOptional(value, "taskId", path, errors, validateTaskIdValue);
  validateOptional(value, "receiptId", path, errors, validateReceiptIdValue);
  validateRequired(value, "requestedBy", path, errors, validateSignerIdentityValue);
  validateRequired(value, "requestedAt", path, errors, validateDateValue);
  validateClaimScopeBindingValue(
    recordValue(value, "claim"),
    recordValue(value, "scope"),
    pointer(path, "scope"),
    errors,
  );
}

function validateApprovalDecidedAuditPayloadValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, APPROVAL_DECIDED_AUDIT_PAYLOAD_KEYS, errors);
  validateRequired(value, "requestId", path, errors, validateApprovalRequestIdValue);
  validateRequired(value, "decision", path, errors, validateApprovalDecisionValue);
  validateRequired(value, "decidedBy", path, errors, validateSignerIdentityValue);
  validateRequired(value, "decidedAt", path, errors, validateDateValue);
  validateOptional(value, "token", path, errors, validateSignedApprovalTokenValue);
}

function approvalRequestedAuditPayloadFromJsonValue(value: unknown): ApprovalRequestedAuditPayload {
  const record = requireRecord(value, "approvalRequestedAuditPayload");
  assertKnownKeys(record, "approvalRequestedAuditPayload", APPROVAL_REQUESTED_AUDIT_PAYLOAD_KEYS);
  const threadId = optionalStringFromJson(record, "threadId", "approvalRequestedAuditPayload");
  const taskId = optionalStringFromJson(record, "taskId", "approvalRequestedAuditPayload");
  const receiptId = optionalStringFromJson(record, "receiptId", "approvalRequestedAuditPayload");
  const payload: ApprovalRequestedAuditPayload = {
    requestId: asApprovalRequestIdAt(
      requiredStringFromJson(record, "requestId", "approvalRequestedAuditPayload"),
      "approvalRequestedAuditPayload.requestId",
    ),
    claim: approvalClaimFromJson(
      requiredFieldFromJson(record, "claim", "approvalRequestedAuditPayload"),
      "approvalRequestedAuditPayload.claim",
    ),
    scope: approvalScopeFromJson(
      requiredFieldFromJson(record, "scope", "approvalRequestedAuditPayload"),
      "approvalRequestedAuditPayload.scope",
    ),
    riskClass: riskClassFromJson(
      requiredStringFromJson(record, "riskClass", "approvalRequestedAuditPayload"),
      "approvalRequestedAuditPayload.riskClass",
    ),
    ...(threadId === undefined
      ? {}
      : { threadId: asThreadIdAt(threadId, "approvalRequestedAuditPayload.threadId") }),
    ...(taskId === undefined
      ? {}
      : { taskId: asTaskIdAt(taskId, "approvalRequestedAuditPayload.taskId") }),
    ...(receiptId === undefined
      ? {}
      : { receiptId: asReceiptIdAt(receiptId, "approvalRequestedAuditPayload.receiptId") }),
    requestedBy: asSignerIdentityAt(
      requiredStringFromJson(record, "requestedBy", "approvalRequestedAuditPayload"),
      "approvalRequestedAuditPayload.requestedBy",
    ),
    requestedAt: requiredDateFromJson(record, "requestedAt", "approvalRequestedAuditPayload"),
  };
  const validation = validateApprovalRequestedAuditPayload(payload);
  if (!validation.ok) throw new Error(formatValidationErrors(validation.errors));
  return payload;
}

function approvalDecidedAuditPayloadFromJsonValue(value: unknown): ApprovalDecidedAuditPayload {
  const record = requireRecord(value, "approvalDecidedAuditPayload");
  assertKnownKeys(record, "approvalDecidedAuditPayload", APPROVAL_DECIDED_AUDIT_PAYLOAD_KEYS);
  const token = optionalSignedApprovalTokenFromJson(record, "token", "approvalDecidedAuditPayload");
  const payload: ApprovalDecidedAuditPayload = {
    requestId: asApprovalRequestIdAt(
      requiredStringFromJson(record, "requestId", "approvalDecidedAuditPayload"),
      "approvalDecidedAuditPayload.requestId",
    ),
    decision: approvalDecisionFromJson(
      requiredStringFromJson(record, "decision", "approvalDecidedAuditPayload"),
      "approvalDecidedAuditPayload.decision",
    ),
    decidedBy: asSignerIdentityAt(
      requiredStringFromJson(record, "decidedBy", "approvalDecidedAuditPayload"),
      "approvalDecidedAuditPayload.decidedBy",
    ),
    decidedAt: requiredDateFromJson(record, "decidedAt", "approvalDecidedAuditPayload"),
    ...(token === undefined ? {} : { token }),
  };
  const validation = validateApprovalDecidedAuditPayload(payload);
  if (!validation.ok) throw new Error(formatValidationErrors(validation.errors));
  return payload;
}

function optionalDecisionRecordFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): ApprovalDecisionRecord | undefined {
  if (!hasOwn(record, key)) return undefined;
  const value = recordValue(record, key);
  if (value === undefined) return undefined;
  const path = `${basePath}.${key}`;
  const decisionRecord = requireRecord(value, path);
  assertKnownKeys(decisionRecord, path, APPROVAL_DECISION_RECORD_WIRE_KEYS);
  const token = optionalSignedApprovalTokenFromJson(decisionRecord, "token", path);
  return {
    decision: approvalDecisionFromJson(
      requiredStringFromJson(decisionRecord, "decision", path),
      `${path}.decision`,
    ),
    decidedBy: asSignerIdentityAt(
      requiredStringFromJson(decisionRecord, "decided_by", path),
      `${path}.decided_by`,
    ),
    decidedAt: requiredDateFromJson(decisionRecord, "decided_at", path),
    ...(token === undefined ? {} : { token }),
  };
}

function optionalSignedApprovalTokenFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): SignedApprovalToken | undefined {
  if (!hasOwn(record, key)) return undefined;
  const value = recordValue(record, key);
  if (value === undefined) return undefined;
  return signedApprovalTokenFromJson(value, `${basePath}.${key}`);
}

function validateKnownKeys(
  record: Readonly<Record<string, unknown>>,
  basePath: string,
  allowed: ReadonlySet<string>,
  errors: ApprovalValidationError[],
): void {
  for (const key of Object.keys(record)) {
    if (!allowed.has(key)) addError(errors, pointer(basePath, key), "is not allowed");
  }
}

function validateRequired(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  errors: ApprovalValidationError[],
  validator: (value: unknown, path: string, errors: ApprovalValidationError[]) => void,
): void {
  const fieldPath = pointer(basePath, key);
  if (!hasOwn(record, key)) {
    addError(errors, fieldPath, "is required");
    return;
  }
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    addError(errors, fieldPath, "must be a data property");
    return;
  }
  if (descriptor.value === undefined) {
    addError(errors, fieldPath, "is required");
    return;
  }
  validator(descriptor.value, fieldPath, errors);
}

function validateOptional(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  errors: ApprovalValidationError[],
  validator: (value: unknown, path: string, errors: ApprovalValidationError[]) => void,
): void {
  if (!hasOwn(record, key)) return;
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    addError(errors, pointer(basePath, key), "must be a data property");
    return;
  }
  if (descriptor.value === undefined) return;
  validator(descriptor.value, pointer(basePath, key), errors);
}

function validateApprovalRequestIdValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isApprovalRequestId(value)) {
    addError(errors, path, "must be an uppercase ULID ApprovalRequestId");
  }
}

function validateApprovalClaimValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  try {
    approvalClaimFromJson(value, pathToJsonPath(path, "approvalClaim"));
  } catch (err) {
    addError(errors, path, err instanceof Error ? err.message : "must be an ApprovalClaim");
  }
}

function validateApprovalScopeValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  try {
    approvalScopeFromJson(value, pathToJsonPath(path, "approvalScope"));
  } catch (err) {
    addError(errors, path, err instanceof Error ? err.message : "must be an ApprovalScope");
  }
}

function validateSignedApprovalTokenValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  try {
    signedApprovalTokenFromJson(value, pathToJsonPath(path, "signedApprovalToken"));
  } catch (err) {
    addError(errors, path, err instanceof Error ? err.message : "must be a SignedApprovalToken");
  }
}

function validateRiskClassValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (typeof value !== "string" || !RISK_CLASS_SET.has(value)) {
    addError(errors, path, "must be a valid risk class");
  }
}

function validateThreadIdValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isThreadId(value)) addError(errors, path, "must be an uppercase ULID ThreadId");
}

function validateTaskIdValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isTaskId(value)) addError(errors, path, "must be an uppercase ULID TaskId");
}

function validateReceiptIdValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isReceiptId(value)) addError(errors, path, "must be an uppercase ULID ReceiptId");
}

function validateSignerIdentityValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isSignerIdentity(value)) {
    addError(errors, path, "must be a bounded non-empty SignerIdentity");
  }
}

function validateDateValue(value: unknown, path: string, errors: ApprovalValidationError[]): void {
  if (!(value instanceof Date) || Number.isNaN(value.getTime())) {
    addError(errors, path, "must be a valid Date");
  }
}

function validateApprovalRequestStatusValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isApprovalRequestStatus(value)) {
    addError(errors, path, "must be a valid approval request status");
  }
}

function validateApprovalDecisionValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isApprovalDecision(value)) {
    addError(errors, path, "must be a valid approval decision");
  }
}

function validateSchemaVersionValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (value !== APPROVAL_REQUEST_SCHEMA_VERSION) addError(errors, path, "must be 1");
}

function validateClaimScopeBindingValue(
  claim: unknown,
  scope: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  let decodedClaim: ApprovalClaim;
  let decodedScope: ApprovalScope;
  try {
    decodedClaim = approvalClaimFromJson(claim, "approvalRequest.claim");
    decodedScope = approvalScopeFromJson(scope, "approvalRequest.scope");
  } catch {
    return;
  }
  if (decodedClaim.claimId !== decodedScope.claimId) {
    addError(errors, pointer(path, "claimId"), "must match claim.claimId");
  }
  if (decodedClaim.kind !== decodedScope.claimKind) {
    addError(errors, pointer(path, "claimKind"), "must match claim.kind");
    return;
  }
  if (decodedClaim.kind === "cost_spike_acknowledgement") {
    if (decodedScope.claimKind !== decodedClaim.kind) return;
    validateSame(decodedScope.agentId, decodedClaim.agentId, path, "agentId", errors);
    validateSame(
      decodedScope.costCeilingId,
      decodedClaim.costCeilingId,
      path,
      "costCeilingId",
      errors,
    );
    return;
  }
  if (decodedClaim.kind === "endpoint_allowlist_extension") {
    if (decodedScope.claimKind !== decodedClaim.kind) return;
    validateSame(decodedScope.agentId, decodedClaim.agentId, path, "agentId", errors);
    validateSame(
      decodedScope.providerKind,
      decodedClaim.providerKind,
      path,
      "providerKind",
      errors,
    );
    validateSame(
      decodedScope.endpointOrigin,
      decodedClaim.endpointOrigin,
      path,
      "endpointOrigin",
      errors,
    );
    return;
  }
  if (decodedClaim.kind === "credential_grant_to_agent") {
    if (decodedScope.claimKind !== decodedClaim.kind) return;
    validateSame(
      decodedScope.granteeAgentId,
      decodedClaim.granteeAgentId,
      path,
      "granteeAgentId",
      errors,
    );
    validateSame(
      decodedScope.credentialHandleId,
      decodedClaim.credentialHandleId,
      path,
      "credentialHandleId",
      errors,
    );
    return;
  }
  if (decodedScope.claimKind !== decodedClaim.kind) return;
  validateSame(decodedScope.receiptId, decodedClaim.receiptId, path, "receiptId", errors);
  validateSame(decodedScope.writeId, decodedClaim.writeId, path, "writeId", errors);
  validateSame(
    decodedScope.frozenArgsHash,
    decodedClaim.frozenArgsHash,
    path,
    "frozenArgsHash",
    errors,
  );
}

function validateSame(
  left: unknown,
  right: unknown,
  path: string,
  field: string,
  errors: ApprovalValidationError[],
): void {
  if (left !== right) addError(errors, pointer(path, field), `must match claim.${field}`);
}

function isApprovalRequestStatus(value: unknown): value is ApprovalRequestStatus {
  return typeof value === "string" && APPROVAL_REQUEST_STATUS_SET.has(value);
}

function isNonPendingApprovalRequestStatus(
  value: unknown,
): value is Exclude<ApprovalRequestStatus, "pending"> {
  return value === "approved" || value === "rejected" || value === "abstained";
}

function isApprovalDecision(value: unknown): value is ApprovalDecision {
  return typeof value === "string" && APPROVAL_DECISION_SET.has(value);
}

function approvalRequestStatusForDecision(decision: ApprovalDecision): ApprovalRequestStatus {
  if (decision === "approve") return "approved";
  if (decision === "reject") return "rejected";
  return "abstained";
}

function unknownApprovalAuditEventKindMessage(kind: unknown): string {
  return `unknown ApprovalAuditEventKind: ${String(kind)}`;
}

function requiredFieldFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): unknown {
  if (!hasOwn(record, key)) {
    throw new Error(`${basePath}.${key}: is required`);
  }
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    throw new Error(`${basePath}.${key}: must be a data property`);
  }
  if (descriptor.value === undefined) {
    throw new Error(`${basePath}.${key}: is required`);
  }
  return descriptor.value;
}

function requiredStringFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): string {
  const value = requiredFieldFromJson(record, key, basePath);
  if (typeof value !== "string") {
    throw new Error(`${basePath}.${key}: must be a string`);
  }
  return value;
}

function optionalStringFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): string | undefined {
  if (!hasOwn(record, key)) return undefined;
  const value = recordValue(record, key);
  if (value === undefined) return undefined;
  if (typeof value !== "string") {
    throw new Error(`${basePath}.${key}: must be a string`);
  }
  return value;
}

function requiredDateFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): Date {
  const value = requiredStringFromJson(record, key, basePath);
  return dateFromJson(value, `${basePath}.${key}`);
}

function dateFromJson(value: string, path: string): Date {
  if (!ISO_DATE_RE.test(value)) {
    throw new Error(`${path}: must be an ISO 8601 string`);
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.toISOString() !== value) {
    throw new Error(`${path}: must be a valid ISO 8601 instant`);
  }
  return date;
}

function requiredSchemaVersionFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): 1 {
  const value = requiredFieldFromJson(record, key, basePath);
  if (value !== APPROVAL_REQUEST_SCHEMA_VERSION) {
    throw new Error(`${basePath}.${key}: must be 1`);
  }
  return APPROVAL_REQUEST_SCHEMA_VERSION;
}

function approvalRequestStatusFromJson(value: string, path: string): ApprovalRequestStatus {
  if (!APPROVAL_REQUEST_STATUS_SET.has(value)) {
    throw new Error(`${path}: must be one of ${APPROVAL_REQUEST_STATUS_VALUES.join(", ")}`);
  }
  return value as ApprovalRequestStatus;
}

function approvalDecisionFromJson(value: string, path: string): ApprovalDecision {
  if (!APPROVAL_DECISION_SET.has(value)) {
    throw new Error(`${path}: must be one of ${APPROVAL_DECISION_VALUES.join(", ")}`);
  }
  return value as ApprovalDecision;
}

function riskClassFromJson(value: string, path: string): RiskClass {
  if (!RISK_CLASS_SET.has(value)) {
    throw new Error(`${path}: must be one of ${RISK_CLASS_VALUES.join(", ")}`);
  }
  return value as RiskClass;
}

function asApprovalRequestIdAt(value: string, path: string): ApprovalRequestId {
  return decodeBrandAt(value, path, asApprovalRequestId);
}

function asThreadIdAt(value: string, path: string): ThreadId {
  return decodeBrandAt(value, path, asThreadId);
}

function asTaskIdAt(value: string, path: string): TaskId {
  return decodeBrandAt(value, path, asTaskId);
}

function asReceiptIdAt(value: string, path: string): ReceiptId {
  return decodeBrandAt(value, path, asReceiptId);
}

function asSignerIdentityAt(value: string, path: string): SignerIdentity {
  return decodeBrandAt(value, path, asSignerIdentity);
}

function decodeBrandAt<T>(value: string, path: string, decode: (value: string) => T): T {
  try {
    return decode(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function pathToJsonPath(path: string, fallback: string): string {
  if (path === "") return fallback;
  return path
    .split("/")
    .filter((segment) => segment.length > 0)
    .map((segment) => segment.replace(/~1/g, "/").replace(/~0/g, "~"))
    .join(".");
}
