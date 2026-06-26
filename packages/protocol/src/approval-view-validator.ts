import {
  APPROVAL_REQUEST_STATUS_VALUES,
  type ApprovalDecision,
  type ApprovalRequestStatus,
  type ApprovalValidationError,
  type ApprovalValidationResult,
} from "./approval-request.ts";
import { APPROVAL_DECISION_VALUES, RISK_CLASS_VALUES } from "./receipt-literals.ts";
import {
  isApprovalRequestId,
  isReceiptId,
  isSignerIdentity,
  isTaskId,
  isThreadId,
} from "./receipt-types.ts";
import { addError, hasOwn, isRecord, pointer, recordValue } from "./receipt-utils.ts";
import type { ApprovalDecisionSummary, ApprovalView } from "./route-envelopes.ts";
import {
  type ApprovalClaim,
  type ApprovalScope,
  approvalClaimFromJson,
  approvalScopeFromJson,
} from "./signed-approval-token.ts";

const APPROVAL_VIEW_SCHEMA_VERSION = 1;

const APPROVAL_VIEW_KEYS_TUPLE = [
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
  "decisionSummary",
  "schemaVersion",
] as const satisfies readonly (keyof ApprovalView)[];
const APPROVAL_DECISION_SUMMARY_KEYS_TUPLE = [
  "decision",
  "decidedBy",
  "decidedAt",
] as const satisfies readonly (keyof ApprovalDecisionSummary)[];

const APPROVAL_VIEW_KEYS: ReadonlySet<string> = new Set(APPROVAL_VIEW_KEYS_TUPLE);
const APPROVAL_DECISION_SUMMARY_KEYS: ReadonlySet<string> = new Set(
  APPROVAL_DECISION_SUMMARY_KEYS_TUPLE,
);
const APPROVAL_REQUEST_STATUS_SET: ReadonlySet<string> = new Set<string>(
  APPROVAL_REQUEST_STATUS_VALUES,
);
const APPROVAL_DECISION_SET: ReadonlySet<string> = new Set<string>(APPROVAL_DECISION_VALUES);
const RISK_CLASS_SET: ReadonlySet<string> = new Set<string>(RISK_CLASS_VALUES);

export function validateApprovalView(input: unknown): ApprovalValidationResult {
  const errors: ApprovalValidationError[] = [];
  validateApprovalViewValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

function validateApprovalViewValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeysForResult(value, path, APPROVAL_VIEW_KEYS, errors);
  validateRequiredForResult(value, "id", path, errors, validateApprovalRequestIdValue);
  validateRequiredForResult(value, "claim", path, errors, validateApprovalClaimValue);
  validateRequiredForResult(value, "scope", path, errors, validateApprovalScopeValue);
  validateRequiredForResult(value, "riskClass", path, errors, validateRiskClassValue);
  validateOptionalForResult(value, "threadId", path, errors, validateThreadIdValue);
  validateOptionalForResult(value, "taskId", path, errors, validateTaskIdValue);
  validateOptionalForResult(value, "receiptId", path, errors, validateReceiptIdValue);
  validateRequiredForResult(value, "requestedBy", path, errors, validateSignerIdentityValue);
  validateRequiredForResult(value, "requestedAt", path, errors, validateDateValue);
  validateRequiredForResult(value, "status", path, errors, validateApprovalRequestStatusValue);
  validateOptionalForResult(
    value,
    "decisionSummary",
    path,
    errors,
    validateApprovalDecisionSummaryValue,
  );
  validateRequiredForResult(value, "schemaVersion", path, errors, validateSchemaVersionValue);
  validateApprovalViewBindingsValue(value, path, errors);
  validateApprovalViewDecisionCouplingValue(value, path, errors);
}

function validateApprovalDecisionSummaryValue(
  value: unknown,
  path: string,
  errors: ApprovalValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeysForResult(value, path, APPROVAL_DECISION_SUMMARY_KEYS, errors);
  validateRequiredForResult(value, "decision", path, errors, validateApprovalDecisionValue);
  validateRequiredForResult(value, "decidedBy", path, errors, validateSignerIdentityValue);
  validateRequiredForResult(value, "decidedAt", path, errors, validateDateValue);
}

function validateApprovalViewDecisionCouplingValue(
  view: Readonly<Record<string, unknown>>,
  path: string,
  errors: ApprovalValidationError[],
): void {
  const status = recordValue(view, "status");
  const summary = recordValue(view, "decisionSummary");
  if (status === "pending" && summary !== undefined) {
    addError(errors, pointer(path, "decisionSummary"), "must be absent when status is pending");
  }
  if (isNonPendingApprovalRequestStatus(status) && summary === undefined) {
    addError(errors, pointer(path, "decisionSummary"), "is required when status is not pending");
  }
  if (isRecord(summary) && isApprovalRequestStatus(status)) {
    const decision = recordValue(summary, "decision");
    if (isApprovalDecision(decision) && status !== approvalRequestStatusForDecision(decision)) {
      addError(errors, pointer(pointer(path, "decisionSummary"), "decision"), "must match status");
    }
  }
}

function validateApprovalViewBindingsValue(
  view: Readonly<Record<string, unknown>>,
  path: string,
  errors: ApprovalValidationError[],
): void {
  const decoded = decodeApprovalClaimScopePair(
    recordValue(view, "claim"),
    recordValue(view, "scope"),
  );
  if (decoded === undefined) return;
  const { claim, scope } = decoded;
  if (claim.claimId !== scope.claimId) {
    addError(errors, pointer(pointer(path, "scope"), "claimId"), "must match claim.claimId");
  }
  if (claim.kind !== scope.claimKind) {
    addError(errors, pointer(pointer(path, "scope"), "claimKind"), "must match claim.kind");
    return;
  }
  validateApprovalClaimScopeFields(claim, scope, pointer(path, "scope"), errors);
  if (claim.kind !== "receipt_co_sign") return;
  if (recordValue(view, "receiptId") !== claim.receiptId) {
    addError(errors, pointer(path, "receiptId"), "must match claim.receiptId");
  }
  if (recordValue(view, "riskClass") !== claim.riskClass) {
    addError(errors, pointer(path, "riskClass"), "must match claim.riskClass");
  }
}

function validateApprovalClaimScopeFields(
  claim: ApprovalClaim,
  scope: ApprovalScope,
  path: string,
  errors: ApprovalValidationError[],
): void {
  switch (claim.kind) {
    case "cost_spike_acknowledgement":
      if (scope.claimKind !== claim.kind) return;
      validateSameValue(scope.agentId, claim.agentId, path, "agentId", errors);
      validateSameValue(scope.costCeilingId, claim.costCeilingId, path, "costCeilingId", errors);
      return;
    case "endpoint_allowlist_extension":
      if (scope.claimKind !== claim.kind) return;
      validateSameValue(scope.agentId, claim.agentId, path, "agentId", errors);
      validateSameValue(scope.providerKind, claim.providerKind, path, "providerKind", errors);
      validateSameValue(scope.endpointOrigin, claim.endpointOrigin, path, "endpointOrigin", errors);
      return;
    case "credential_grant_to_agent":
      if (scope.claimKind !== claim.kind) return;
      validateSameValue(scope.granteeAgentId, claim.granteeAgentId, path, "granteeAgentId", errors);
      validateSameValue(
        scope.credentialHandleId,
        claim.credentialHandleId,
        path,
        "credentialHandleId",
        errors,
      );
      return;
    case "receipt_co_sign":
      if (scope.claimKind !== claim.kind) return;
      validateSameValue(scope.receiptId, claim.receiptId, path, "receiptId", errors);
      validateSameValue(scope.writeId, claim.writeId, path, "writeId", errors);
      validateSameValue(scope.frozenArgsHash, claim.frozenArgsHash, path, "frozenArgsHash", errors);
      return;
  }
}

function validateKnownKeysForResult(
  record: Readonly<Record<string, unknown>>,
  basePath: string,
  allowed: ReadonlySet<string>,
  errors: ApprovalValidationError[],
): void {
  for (const key of Object.keys(record)) {
    if (!allowed.has(key)) addError(errors, pointer(basePath, key), "is not allowed");
  }
}

function validateRequiredForResult(
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

function validateOptionalForResult(
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
    approvalClaimFromJson(value, path);
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
    approvalScopeFromJson(value, path);
  } catch (err) {
    addError(errors, path, err instanceof Error ? err.message : "must be an ApprovalScope");
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
  if (value !== APPROVAL_VIEW_SCHEMA_VERSION) {
    addError(errors, path, `must be ${APPROVAL_VIEW_SCHEMA_VERSION}`);
  }
}

function validateSameValue(
  left: unknown,
  right: unknown,
  path: string,
  field: string,
  errors: ApprovalValidationError[],
): void {
  if (left !== right) addError(errors, pointer(path, field), `must match claim.${field}`);
}

function decodeApprovalClaimScopePair(
  claim: unknown,
  scope: unknown,
): { readonly claim: ApprovalClaim; readonly scope: ApprovalScope } | undefined {
  try {
    return {
      claim: approvalClaimFromJson(claim, "approvalView.claim"),
      scope: approvalScopeFromJson(scope, "approvalView.scope"),
    };
  } catch {
    return undefined;
  }
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
