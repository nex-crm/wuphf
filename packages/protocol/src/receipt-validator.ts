// Receipt validator. Splits out from receipt.ts (which would otherwise be
// >1500 LOC and trip the file-size hook). Public surface is `validateReceipt`
// and `isReceiptSnapshot`; both are re-exported by receipt.ts.
//
// The validator is hand-rolled — no third-party schema lib at runtime — so
// the moat boundary (RFC §6) sits behind code we own end-to-end.

import { FrozenArgs } from "./frozen-args.ts";
import {
  isAgentSlug,
  isApprovalId,
  isProviderKind,
  isReceiptId,
  isTaskId,
  isToolCallId,
  type ReceiptSnapshot,
  type ReceiptValidationError,
  type ReceiptValidationResult,
  type RiskClass,
  type TriggerKind,
  type WriteResult,
} from "./receipt-types.ts";
import { addError, hasOwn, isRecord, pointer, recordValue } from "./receipt-utils.ts";
import { SanitizedString } from "./sanitized-string.ts";
import { isSha256Hex } from "./sha256.ts";

const RECEIPT_STATUS_VALUES = [
  "ok",
  "error",
  "stalled",
  "approval_pending",
  "rejected",
] as const satisfies readonly ReceiptSnapshot["status"][];
const RISK_CLASS_VALUES = [
  "low",
  "medium",
  "high",
  "critical",
] as const satisfies readonly RiskClass[];
const WRITE_RESULT_VALUES = [
  "applied",
  "rejected",
  "partial",
  "rollback",
] as const satisfies readonly WriteResult[];
const TRIGGER_KIND_VALUES = [
  "human_message",
  "scheduler",
  "mention",
  "webhook",
  "agent_message",
  "system",
] as const satisfies readonly TriggerKind[];
const APPROVAL_ROLE_VALUES = ["viewer", "approver", "host"] as const;
const APPROVAL_DECISION_VALUES = ["approve", "reject", "abstain"] as const;
const TOOL_CALL_STATUS_VALUES = ["ok", "error"] as const;
const FILE_CHANGE_MODE_VALUES = ["created", "modified", "deleted"] as const;
const MEMORY_STORE_VALUES = ["notebook", "wiki"] as const;
const BROKER_VERIFICATION_STATUS_VALUES = ["valid", "expired", "tampered"] as const;

export const RECEIPT_KEYS: ReadonlySet<string> = new Set<string>([
  "id",
  "agentSlug",
  "taskId",
  "triggerKind",
  "triggerRef",
  "startedAt",
  "finishedAt",
  "status",
  "providerKind",
  "model",
  "promptHash",
  "toolManifest",
  "toolCalls",
  "approvals",
  "filesChanged",
  "commits",
  "sourceReads",
  "writes",
  "inputTokens",
  "outputTokens",
  "cacheReadTokens",
  "cacheCreationTokens",
  "costUsd",
  "finalMessage",
  "error",
  "notebookWrites",
  "wikiWrites",
  "worktreePath",
  "gitHeadStart",
  "gitHeadEnd",
  "schemaVersion",
]);
export const SOURCE_READ_KEYS: ReadonlySet<string> = new Set<string>([
  "provider",
  "entityType",
  "entityId",
  "fetchedAt",
  "hash",
  "citation",
  "rawRef",
]);
export const TOOL_CALL_KEYS: ReadonlySet<string> = new Set<string>([
  "toolId",
  "toolName",
  "inputs",
  "output",
  "startedAt",
  "finishedAt",
  "status",
  "error",
]);
export const APPROVAL_EVENT_KEYS: ReadonlySet<string> = new Set<string>([
  "approvalId",
  "role",
  "decision",
  "signedToken",
  "decidedAt",
]);
export const FILE_CHANGE_KEYS: ReadonlySet<string> = new Set<string>([
  "path",
  "mode",
  "beforeHash",
  "afterHash",
  "linesAdded",
  "linesRemoved",
]);
export const COMMIT_REF_KEYS: ReadonlySet<string> = new Set<string>([
  "sha",
  "message",
  "author",
  "authorEmail",
  "parentSha",
  "signed",
]);
export const MEMORY_WRITE_KEYS: ReadonlySet<string> = new Set<string>([
  "store",
  "slug",
  "hash",
  "citation",
]);
export const SIGNED_APPROVAL_TOKEN_KEYS: ReadonlySet<string> = new Set<string>([
  "signerIdentity",
  "role",
  "receiptId",
  "frozenArgsHash",
  "riskClass",
  "expiresAt",
  "webauthnAssertion",
  "brokerVerificationStatus",
]);
export const EXTERNAL_WRITE_KEYS: ReadonlySet<string> = new Set<string>([
  "action",
  "target",
  "idempotencyKey",
  "proposedDiff",
  "appliedDiff",
  "approvalToken",
  "approvedAt",
  "result",
  "postWriteVerify",
]);

export function validateReceipt(input: unknown): ReceiptValidationResult {
  try {
    const errors: ReceiptValidationError[] = [];
    validateReceiptSnapshot(input, "", errors);
    return errors.length === 0 ? { ok: true } : { ok: false, errors };
  } catch (err) {
    return {
      ok: false,
      errors: [
        {
          path: "",
          message: err instanceof Error ? err.message : "receipt validation failed",
        },
      ],
    };
  }
}

export function isReceiptSnapshot(input: unknown): input is ReceiptSnapshot {
  return validateReceipt(input).ok;
}

export function validateKnownKeys(
  record: Readonly<Record<string, unknown>>,
  basePath: string,
  allowed: ReadonlySet<string>,
  errors: ReceiptValidationError[],
): void {
  for (const key of Object.keys(record)) {
    if (!allowed.has(key)) {
      addError(errors, pointer(basePath, key), "is not allowed");
    }
  }
}

function validateReceiptSnapshot(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, RECEIPT_KEYS, errors);

  validateRequired(value, "id", path, errors, validateReceiptIdValue);
  const receiptId = recordValue(value, "id");
  validateRequired(value, "agentSlug", path, errors, validateAgentSlugValue);
  validateRequired(value, "taskId", path, errors, validateTaskIdValue);
  validateRequired(value, "triggerKind", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, TRIGGER_KIND_VALUES, "must be a valid trigger kind"),
  );
  validateRequired(value, "triggerRef", path, errors, validateString);
  validateRequired(value, "startedAt", path, errors, validateDate);
  validateOptional(value, "finishedAt", path, errors, validateDate);
  validateRequired(value, "status", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, RECEIPT_STATUS_VALUES, "must be a valid receipt status"),
  );
  validateRequired(value, "providerKind", path, errors, validateProviderKindValue);
  validateRequired(value, "model", path, errors, validateString);
  validateRequired(value, "promptHash", path, errors, validateSha256HexValue);
  validateRequired(value, "toolManifest", path, errors, validateSha256HexValue);
  validateRequired(value, "toolCalls", path, errors, (v, p, e) =>
    validateArray(v, p, e, validateToolCall),
  );
  validateRequired(value, "approvals", path, errors, (v, p, e) =>
    validateArray(v, p, e, (item, itemPath, itemErrors) =>
      validateApprovalEvent(item, itemPath, itemErrors, receiptId),
    ),
  );
  validateRequired(value, "filesChanged", path, errors, (v, p, e) =>
    validateArray(v, p, e, validateFileChange),
  );
  validateRequired(value, "commits", path, errors, (v, p, e) =>
    validateArray(v, p, e, validateCommitRef),
  );
  validateRequired(value, "sourceReads", path, errors, (v, p, e) =>
    validateArray(v, p, e, validateSourceRead),
  );
  validateRequired(value, "writes", path, errors, (v, p, e) =>
    validateArray(v, p, e, (item, itemPath, itemErrors) =>
      validateExternalWrite(item, itemPath, itemErrors, receiptId),
    ),
  );
  validateRequired(value, "inputTokens", path, errors, validateNonNegativeInteger);
  validateRequired(value, "outputTokens", path, errors, validateNonNegativeInteger);
  validateRequired(value, "cacheReadTokens", path, errors, validateNonNegativeInteger);
  validateRequired(value, "cacheCreationTokens", path, errors, validateNonNegativeInteger);
  validateRequired(value, "costUsd", path, errors, validateNonNegativeFiniteNumber);
  validateOptional(value, "finalMessage", path, errors, validateSanitizedString);
  validateOptional(value, "error", path, errors, validateSanitizedString);
  validateRequired(value, "notebookWrites", path, errors, (v, p, e) =>
    validateArray(v, p, e, validateMemoryWriteRef),
  );
  validateRequired(value, "wikiWrites", path, errors, (v, p, e) =>
    validateArray(v, p, e, validateMemoryWriteRef),
  );
  validateOptional(value, "worktreePath", path, errors, validateString);
  validateOptional(value, "gitHeadStart", path, errors, validateString);
  validateOptional(value, "gitHeadEnd", path, errors, validateString);
  validateRequired(value, "schemaVersion", path, errors, (v, p, e) => {
    if (v !== 1) addError(e, p, "must be 1");
  });
}

function validateSourceRead(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, SOURCE_READ_KEYS, errors);
  validateRequired(value, "provider", path, errors, validateString);
  validateRequired(value, "entityType", path, errors, validateString);
  validateRequired(value, "entityId", path, errors, validateString);
  validateRequired(value, "fetchedAt", path, errors, validateDate);
  validateRequired(value, "hash", path, errors, validateSha256HexValue);
  validateRequired(value, "citation", path, errors, validateString);
  validateOptional(value, "rawRef", path, errors, validateString);
}

function validateToolCall(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, TOOL_CALL_KEYS, errors);
  validateRequired(value, "toolId", path, errors, validateToolCallIdValue);
  validateRequired(value, "toolName", path, errors, validateString);
  validateRequired(value, "inputs", path, errors, validateFrozenArgs);
  validateRequired(value, "output", path, errors, validateSanitizedString);
  validateRequired(value, "startedAt", path, errors, validateDate);
  validateRequired(value, "finishedAt", path, errors, validateDate);
  validateRequired(value, "status", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, TOOL_CALL_STATUS_VALUES, "must be a valid tool call status"),
  );
  validateOptional(value, "error", path, errors, validateSanitizedString);
}

function validateApprovalEvent(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
  receiptId: unknown,
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, APPROVAL_EVENT_KEYS, errors);
  validateRequired(value, "approvalId", path, errors, validateApprovalIdValue);
  validateRequired(value, "role", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, APPROVAL_ROLE_VALUES, "must be a valid approval role"),
  );
  validateRequired(value, "decision", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, APPROVAL_DECISION_VALUES, "must be a valid approval decision"),
  );
  validateRequired(value, "signedToken", path, errors, validateSignedApprovalToken);
  validateRequired(value, "decidedAt", path, errors, validateDate);

  // Cross-field invariant: the signed token must reference this receipt.
  const signedToken = recordValue(value, "signedToken");
  if (
    isRecord(signedToken) &&
    typeof receiptId === "string" &&
    recordValue(signedToken, "receiptId") !== receiptId
  ) {
    addError(
      errors,
      pointer(pointer(path, "signedToken"), "receiptId"),
      "must match enclosing receipt id",
    );
  }
}

function validateFileChange(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, FILE_CHANGE_KEYS, errors);
  validateRequired(value, "path", path, errors, validateString);
  validateRequired(value, "mode", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, FILE_CHANGE_MODE_VALUES, "must be a valid file change mode"),
  );
  validateOptional(value, "beforeHash", path, errors, validateSha256HexValue);
  validateRequired(value, "afterHash", path, errors, validateSha256HexValue);
  validateRequired(value, "linesAdded", path, errors, validateNonNegativeInteger);
  validateRequired(value, "linesRemoved", path, errors, validateNonNegativeInteger);
}

function validateCommitRef(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, COMMIT_REF_KEYS, errors);
  validateRequired(value, "sha", path, errors, validateString);
  validateRequired(value, "message", path, errors, validateSanitizedString);
  validateRequired(value, "author", path, errors, validateString);
  validateRequired(value, "authorEmail", path, errors, validateString);
  validateOptional(value, "parentSha", path, errors, validateString);
  validateRequired(value, "signed", path, errors, validateBoolean);
}

function validateMemoryWriteRef(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, MEMORY_WRITE_KEYS, errors);
  validateRequired(value, "store", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, MEMORY_STORE_VALUES, "must be notebook or wiki"),
  );
  validateRequired(value, "slug", path, errors, validateString);
  validateRequired(value, "hash", path, errors, validateSha256HexValue);
  validateRequired(value, "citation", path, errors, validateString);
}

function validateSignedApprovalToken(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, SIGNED_APPROVAL_TOKEN_KEYS, errors);
  validateRequired(value, "signerIdentity", path, errors, validateString);
  validateRequired(value, "role", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, APPROVAL_ROLE_VALUES, "must be a valid approval role"),
  );
  validateRequired(value, "receiptId", path, errors, validateReceiptIdValue);
  validateRequired(value, "frozenArgsHash", path, errors, validateSha256HexValue);
  validateRequired(value, "riskClass", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, RISK_CLASS_VALUES, "must be a valid risk class"),
  );
  validateRequired(value, "expiresAt", path, errors, validateDate);
  validateOptional(value, "webauthnAssertion", path, errors, validateString);
  validateRequired(value, "brokerVerificationStatus", path, errors, (v, p, e) =>
    validateLiteral(
      v,
      p,
      e,
      BROKER_VERIFICATION_STATUS_VALUES,
      "must be a valid broker verification status",
    ),
  );
  const riskClass = recordValue(value, "riskClass");
  const webauthnAssertion = recordValue(value, "webauthnAssertion");
  if (
    (riskClass === "high" || riskClass === "critical") &&
    (typeof webauthnAssertion !== "string" || webauthnAssertion.length === 0)
  ) {
    addError(
      errors,
      pointer(path, "webauthnAssertion"),
      "must be a non-empty string for high/critical risk",
    );
  }
}

function validateExternalWrite(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
  receiptId: unknown,
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, EXTERNAL_WRITE_KEYS, errors);
  validateRequired(value, "action", path, errors, validateString);
  validateRequired(value, "target", path, errors, validateString);
  validateRequired(value, "idempotencyKey", path, errors, validateString);
  validateRequired(value, "proposedDiff", path, errors, validateFrozenArgs);
  validateRequired(value, "appliedDiff", path, errors, (v, p, e) =>
    validateNullable(v, p, e, validateFrozenArgs),
  );
  validateRequired(value, "approvalToken", path, errors, (v, p, e) =>
    validateNullable(v, p, e, validateSignedApprovalToken),
  );
  validateOptional(value, "approvedAt", path, errors, validateDate);
  validateRequired(value, "result", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, WRITE_RESULT_VALUES, "must be a valid write result"),
  );
  validateRequired(value, "postWriteVerify", path, errors, (v, p, e) =>
    validateNullable(v, p, e, validateFrozenArgs),
  );

  // Cross-field invariants: when present, the approval token must reference
  // this receipt and bind to the proposedDiff hash. RFC §6 invariant chain.
  const approvalToken = recordValue(value, "approvalToken");
  const proposedDiff = recordValue(value, "proposedDiff");
  if (isRecord(approvalToken)) {
    const tokenPath = pointer(path, "approvalToken");
    if (typeof receiptId === "string" && recordValue(approvalToken, "receiptId") !== receiptId) {
      addError(errors, pointer(tokenPath, "receiptId"), "must match enclosing receipt id");
    }
    if (
      proposedDiff instanceof FrozenArgs &&
      recordValue(approvalToken, "frozenArgsHash") !== proposedDiff.hash
    ) {
      addError(
        errors,
        pointer(tokenPath, "frozenArgsHash"),
        "must match this write's proposedDiff hash",
      );
    }
  }
}

function validateRequired(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  errors: ReceiptValidationError[],
  validator: (value: unknown, path: string, errors: ReceiptValidationError[]) => void,
): void {
  const fieldPath = pointer(basePath, key);
  if (!hasOwn(record, key) || record[key] === undefined) {
    addError(errors, fieldPath, "is required");
    return;
  }
  validator(record[key], fieldPath, errors);
}

function validateOptional(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  errors: ReceiptValidationError[],
  validator: (value: unknown, path: string, errors: ReceiptValidationError[]) => void,
): void {
  if (!hasOwn(record, key) || record[key] === undefined) return;
  validator(record[key], pointer(basePath, key), errors);
}

function validateArray(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
  itemValidator: (value: unknown, path: string, errors: ReceiptValidationError[]) => void,
): void {
  if (!Array.isArray(value)) {
    addError(errors, path, "must be an array");
    return;
  }
  for (let i = 0; i < value.length; i += 1) {
    itemValidator(value[i], pointer(path, String(i)), errors);
  }
}

function validateNullable(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
  validator: (value: unknown, path: string, errors: ReceiptValidationError[]) => void,
): void {
  if (value === null) return;
  validator(value, path, errors);
}

function validateString(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (typeof value !== "string") addError(errors, path, "must be a string");
}

function validateBoolean(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (typeof value !== "boolean") addError(errors, path, "must be a boolean");
}

function validateDate(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (!(value instanceof Date) || Number.isNaN(value.getTime())) {
    addError(errors, path, "must be a valid Date");
  }
}

function validateNonNegativeInteger(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (typeof value !== "number" || !Number.isInteger(value) || value < 0) {
    addError(errors, path, "must be a non-negative integer");
  }
}

function validateNonNegativeFiniteNumber(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0) {
    addError(errors, path, "must be a non-negative finite number");
  }
}

function validateLiteral<T extends readonly string[]>(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
  choices: T,
  message: string,
): void {
  if (typeof value !== "string" || !choices.includes(value)) {
    addError(errors, path, message);
  }
}

function validateReceiptIdValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isReceiptId(value)) addError(errors, path, "must be an uppercase ULID ReceiptId");
}

function validateAgentSlugValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isAgentSlug(value)) addError(errors, path, "must match /^[a-z0-9][a-z0-9_-]*$/");
}

function validateTaskIdValue(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (!isTaskId(value)) addError(errors, path, "must be an uppercase ULID TaskId");
}

function validateProviderKindValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isProviderKind(value)) addError(errors, path, "must be a supported ProviderKind");
}

function validateToolCallIdValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isToolCallId(value)) addError(errors, path, "must be a valid ToolCallId");
}

function validateApprovalIdValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isApprovalId(value)) addError(errors, path, "must be a valid ApprovalId");
}

function validateSha256HexValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isSha256Hex(value)) addError(errors, path, "must be a sha256 hex digest");
}

function validateFrozenArgs(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  // `instanceof` alone is not a freeze-boundary check: an attacker can produce
  // `Object.create(FrozenArgs.prototype)` with mismatched canonicalJson/hash
  // and pass the type-system check. Re-derive both fields and assert byte
  // equality to make forgery observable.
  if (!(value instanceof FrozenArgs)) {
    addError(errors, path, "must be FrozenArgs");
    return;
  }
  if (typeof value.canonicalJson !== "string") {
    addError(errors, pointer(path, "canonicalJson"), "must be a string");
    return;
  }
  if (!isSha256Hex(value.hash)) {
    addError(errors, pointer(path, "hash"), "must be a sha256 hex digest");
    return;
  }
  try {
    const reFrozen = FrozenArgs.freeze(JSON.parse(value.canonicalJson));
    if (reFrozen.canonicalJson !== value.canonicalJson) {
      addError(errors, pointer(path, "canonicalJson"), "must be RFC 8785 canonical JSON");
    }
    if (reFrozen.hash !== value.hash) {
      addError(errors, pointer(path, "hash"), "does not match canonicalJson");
    }
  } catch (err) {
    addError(
      errors,
      pointer(path, "canonicalJson"),
      err instanceof Error ? err.message : "must be valid JSON",
    );
  }
}

function validateSanitizedString(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  // Same reasoning as validateFrozenArgs: re-run the sanitizer and require
  // byte equality so forged `Object.create(SanitizedString.prototype)` with
  // bidi/control text is rejected.
  if (!(value instanceof SanitizedString)) {
    addError(errors, path, "must be SanitizedString");
    return;
  }
  if (typeof value.value !== "string") {
    addError(errors, pointer(path, "value"), "must be a string");
    return;
  }
  try {
    const reSanitized = SanitizedString.fromUnknown(value.value);
    if (reSanitized.value !== value.value) {
      addError(errors, path, "must already be sanitized");
    }
  } catch (err) {
    addError(errors, path, err instanceof Error ? err.message : "must be sanitized");
  }
}
