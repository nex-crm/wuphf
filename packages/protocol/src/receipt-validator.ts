// Receipt validator. Splits out from receipt.ts (which would otherwise be
// >1500 LOC and trip the file-size hook). Public surface is `validateReceipt`
// and `isReceiptSnapshot`; both are re-exported by receipt.ts.
//
// The validator is hand-rolled — no third-party schema lib at runtime — so
// the moat boundary (RFC §6) sits behind code we own end-to-end.

import { validateReceiptBudget } from "./budgets.ts";
import { FrozenArgs } from "./frozen-args.ts";
import {
  APPROVAL_DECISION_VALUES,
  APPROVAL_ROLE_VALUES,
  BROKER_TOKEN_VERDICT_STATUS_VALUES,
  FILE_CHANGE_MODE_VALUES,
  MEMORY_STORE_VALUES,
  RECEIPT_STATUS_VALUES,
  TOOL_CALL_STATUS_VALUES,
  TRIGGER_KIND_VALUES,
  WRITE_RESULT_VALUES,
} from "./receipt-literals.ts";
import {
  type ApprovalEvent,
  type BrokerTokenVerdict,
  type CommitRef,
  type ExternalWrite,
  type FileChange,
  isAgentSlug,
  isApprovalId,
  isIdempotencyKey,
  isProviderKind,
  isReceiptId,
  isTaskId,
  isThreadId,
  isToolCallId,
  isWriteId,
  type MemoryWriteRef,
  type ReceiptCore,
  type ReceiptSnapshot,
  type ReceiptSnapshotV2,
  type ReceiptStatus,
  type ReceiptValidationError,
  type ReceiptValidationResult,
  type SourceRead,
  type ToolCall,
  type WriteFailureMetadata,
} from "./receipt-types.ts";
import { addError, hasOwn, isRecord, pointer, recordValue } from "./receipt-utils.ts";
import { SanitizedString } from "./sanitized-string.ts";
import { isSha256Hex } from "./sha256.ts";
import {
  isReceiptCoSignClaim,
  isReceiptCoSignScope,
  signedApprovalTokenFromJson,
} from "./signed-approval-token.ts";

export { SIGNED_APPROVAL_TOKEN_KEYS } from "./signed-approval-token.ts";

type MemoryStore = (typeof MEMORY_STORE_VALUES)[number];
type ReceiptSnapshotKey = keyof ReceiptCore | keyof ReceiptSnapshotV2;

// Allowlists are tied to interface declarations via `satisfies readonly
// (keyof T)[]`. Adding a typo'd entry fails typecheck. The reverse direction
// — interface gains a new field, allowlist forgot to mirror — is covered by
// the round-trip + property tests in tests/receipt.spec.ts (a missing key in
// the codec produces a serialization that fails to decode), but reviewers
// should still spot-check the matching tuple when adding fields.

const RECEIPT_KEYS_TUPLE = [
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
  "threadId",
  "schemaVersion",
] as const satisfies readonly ReceiptSnapshotKey[];
export const RECEIPT_KEYS: ReadonlySet<string> = new Set<string>(RECEIPT_KEYS_TUPLE);

const SOURCE_READ_KEYS_TUPLE = [
  "provider",
  "entityType",
  "entityId",
  "fetchedAt",
  "hash",
  "citation",
  "rawRef",
] as const satisfies readonly (keyof SourceRead)[];
export const SOURCE_READ_KEYS: ReadonlySet<string> = new Set<string>(SOURCE_READ_KEYS_TUPLE);

const TOOL_CALL_KEYS_TUPLE = [
  "toolId",
  "toolName",
  "inputs",
  "output",
  "startedAt",
  "finishedAt",
  "status",
  "error",
] as const satisfies readonly (keyof ToolCall)[];
export const TOOL_CALL_KEYS: ReadonlySet<string> = new Set<string>(TOOL_CALL_KEYS_TUPLE);

const APPROVAL_EVENT_KEYS_TUPLE = [
  "approvalId",
  "role",
  "decision",
  "signedToken",
  "tokenVerdict",
  "decidedAt",
] as const satisfies readonly (keyof ApprovalEvent)[];
export const APPROVAL_EVENT_KEYS: ReadonlySet<string> = new Set<string>(APPROVAL_EVENT_KEYS_TUPLE);

const BROKER_TOKEN_VERDICT_KEYS_TUPLE = [
  "status",
  "verifiedAt",
] as const satisfies readonly (keyof BrokerTokenVerdict)[];
export const BROKER_TOKEN_VERDICT_KEYS: ReadonlySet<string> = new Set<string>(
  BROKER_TOKEN_VERDICT_KEYS_TUPLE,
);

const FILE_CHANGE_KEYS_TUPLE = [
  "path",
  "mode",
  "beforeHash",
  "afterHash",
  "linesAdded",
  "linesRemoved",
] as const satisfies readonly (keyof FileChange)[];
export const FILE_CHANGE_KEYS: ReadonlySet<string> = new Set<string>(FILE_CHANGE_KEYS_TUPLE);

const COMMIT_REF_KEYS_TUPLE = [
  "sha",
  "message",
  "author",
  "authorEmail",
  "parentSha",
  "signed",
] as const satisfies readonly (keyof CommitRef)[];
export const COMMIT_REF_KEYS: ReadonlySet<string> = new Set<string>(COMMIT_REF_KEYS_TUPLE);

const MEMORY_WRITE_KEYS_TUPLE = [
  "store",
  "slug",
  "hash",
  "citation",
] as const satisfies readonly (keyof MemoryWriteRef)[];
export const MEMORY_WRITE_KEYS: ReadonlySet<string> = new Set<string>(MEMORY_WRITE_KEYS_TUPLE);

// FrozenArgs is a class — its public-shape envelope on the wire is
// `{ canonicalJson, hash }`. Keep this set in sync with the wire codec in
// receipt.ts:frozenArgsToJsonValue. Without this allowlist, sibling fields
// like `{canonicalJson, hash, extra}` round-tripped silently and were not
// covered by the hash — the one place the strict-unknown contract leaked.
const FROZEN_ARGS_KEYS_TUPLE = [
  "canonicalJson",
  "hash",
] as const satisfies readonly (keyof FrozenArgs)[];
export const FROZEN_ARGS_KEYS: ReadonlySet<string> = new Set<string>(FROZEN_ARGS_KEYS_TUPLE);

const WRITE_FAILURE_METADATA_KEYS_TUPLE = [
  "code",
  "retryable",
  "retryAfterMs",
  "terminalReason",
] as const satisfies readonly (keyof WriteFailureMetadata)[];
export const WRITE_FAILURE_METADATA_KEYS: ReadonlySet<string> = new Set<string>(
  WRITE_FAILURE_METADATA_KEYS_TUPLE,
);

const EXTERNAL_WRITE_KEYS_TUPLE = [
  "writeId",
  "action",
  "target",
  "idempotencyKey",
  "proposedDiff",
  "appliedDiff",
  "approvalToken",
  "approvedAt",
  "result",
  "postWriteVerify",
  "failureMetadata",
] as const satisfies readonly (keyof ExternalWrite)[];
export const EXTERNAL_WRITE_KEYS: ReadonlySet<string> = new Set<string>(EXTERNAL_WRITE_KEYS_TUPLE);

interface ReceiptValidationContext {
  readonly recomputedFrozenArgs: ReadonlySet<FrozenArgs>;
}

interface ReceiptWriteEvidence {
  readonly index: number;
  readonly result: unknown;
  readonly resultPath: string;
  readonly writeId: unknown;
}

interface ReceiptStatusEvidence {
  readonly statusPath: string;
  readonly hasRejectedApproval: boolean;
  readonly hasFailedToolCall: boolean;
  readonly hasNonEmptyError: boolean;
  readonly hasRejectionEvidence: boolean;
  readonly hasFailureEvidence: boolean;
  readonly rejectedApprovalWriteIds: ReadonlySet<string>;
  readonly writes: readonly ReceiptWriteEvidence[];
}

type ReceiptStatusEvidenceCheck = (
  evidence: ReceiptStatusEvidence,
  errors: ReceiptValidationError[],
) => void;

const RECEIPT_STATUS_EVIDENCE_RULES = {
  ok: [
    rejectRejectedApprovalsOutsideRejectedOrError,
    rejectFailedToolCallsForOk,
    rejectNonAppliedWritesForOk,
  ],
  approval_pending: [
    rejectRejectedApprovalsOutsideRejectedOrError,
    rejectAppliedWritesForApprovalPending,
  ],
  rejected: [requireRejectionEvidenceForRejected, rejectAppliedWritesForRejectedApprovals],
  error: [requireFailureEvidenceForError],
  stalled: [rejectRejectedApprovalsOutsideRejectedOrError, rejectAppliedWritesForStalled],
} as const satisfies Record<ReceiptStatus, readonly ReceiptStatusEvidenceCheck[]>;

const EMPTY_VALIDATION_CONTEXT: ReceiptValidationContext = {
  recomputedFrozenArgs: new Set<FrozenArgs>(),
};

export function validateReceipt(input: unknown): ReceiptValidationResult {
  return validateReceiptWithContext(input, EMPTY_VALIDATION_CONTEXT);
}

export function validateReceiptWithRecomputedFrozenArgs(
  input: unknown,
  recomputedFrozenArgs: ReadonlySet<FrozenArgs>,
): ReceiptValidationResult {
  return validateReceiptWithContext(input, { recomputedFrozenArgs });
}

function validateReceiptWithContext(
  input: unknown,
  context: ReceiptValidationContext,
): ReceiptValidationResult {
  try {
    const errors: ReceiptValidationError[] = [];
    if (!isRecord(input)) {
      addError(errors, "", "must be an object");
      return { ok: false, errors };
    }
    const budget = validateReceiptBoundaryBudget(input);
    if (budget !== undefined && !budget.ok) {
      return { ok: false, errors: [{ path: "", message: budget.reason }] };
    }
    validateReceiptSnapshot(input, "", errors, context);
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

function validateReceiptBoundaryBudget(
  input: Readonly<Record<string, unknown>>,
): ReturnType<typeof validateReceiptBudget> | undefined {
  try {
    return validateReceiptBudget(input as unknown as ReceiptSnapshot);
  } catch {
    // Malformed in-budget receipts still need field-scoped shape errors below.
    return undefined;
  }
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
  context: ReceiptValidationContext,
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
    validateArray(v, p, e, (item, itemPath, itemErrors) =>
      validateToolCall(item, itemPath, itemErrors, context),
    ),
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
      validateExternalWrite(item, itemPath, itemErrors, receiptId, context),
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
    validateArray(v, p, e, (item, itemPath, itemErrors) =>
      validateMemoryWriteRef(item, itemPath, itemErrors, "notebook"),
    ),
  );
  validateRequired(value, "wikiWrites", path, errors, (v, p, e) =>
    validateArray(v, p, e, (item, itemPath, itemErrors) =>
      validateMemoryWriteRef(item, itemPath, itemErrors, "wiki"),
    ),
  );
  validateOptional(value, "worktreePath", path, errors, validateString);
  validateOptional(value, "gitHeadStart", path, errors, validateString);
  validateOptional(value, "gitHeadEnd", path, errors, validateString);
  validateRequired(value, "schemaVersion", path, errors, validateReceiptSchemaVersionValue);
  validateOptional(value, "threadId", path, errors, validateThreadIdValue);
  const schemaVersion = recordValue(value, "schemaVersion");
  if (schemaVersion === 1 && hasOwn(value, "threadId")) {
    addError(errors, pointer(path, "threadId"), "must be absent for schemaVersion 1");
  }
  validateTemporalOrdering(
    recordValue(value, "startedAt"),
    "startedAt",
    recordValue(value, "finishedAt"),
    "finishedAt",
    pointer(path, "finishedAt"),
    errors,
    true,
  );
  validateReceiptStatusEvidence(value, path, errors);
}

function validateReceiptStatusEvidence(
  value: Readonly<Record<string, unknown>>,
  path: string,
  errors: ReceiptValidationError[],
): void {
  const status = recordValue(value, "status");
  if (!isReceiptStatusValue(status)) {
    return;
  }

  const evidence = collectReceiptStatusEvidence(value, path);
  for (const check of RECEIPT_STATUS_EVIDENCE_RULES[status]) {
    check(evidence, errors);
  }
}

function isReceiptStatusValue(value: unknown): value is ReceiptStatus {
  return (
    typeof value === "string" &&
    RECEIPT_STATUS_VALUES.includes(value as (typeof RECEIPT_STATUS_VALUES)[number])
  );
}

function collectReceiptStatusEvidence(
  value: Readonly<Record<string, unknown>>,
  path: string,
): ReceiptStatusEvidence {
  const statusPath = pointer(path, "status");
  const rejectedApprovalWriteIds = new Set<string>();
  const approvals = recordValue(value, "approvals");
  let hasRejectedApproval = false;
  if (Array.isArray(approvals)) {
    for (let i = 0; i < approvals.length; i += 1) {
      const approval = arrayDataValue(approvals, i);
      if (!isRecord(approval) || recordValue(approval, "decision") !== "reject") continue;
      hasRejectedApproval = true;
      const signedToken = recordValue(approval, "signedToken");
      const claim = isRecord(signedToken) ? recordValue(signedToken, "claim") : undefined;
      const writeId = isRecord(claim) ? recordValue(claim, "writeId") : undefined;
      if (typeof writeId === "string") {
        rejectedApprovalWriteIds.add(writeId);
      }
    }
  }

  const toolCalls = recordValue(value, "toolCalls");
  let hasFailedToolCall = false;
  if (Array.isArray(toolCalls)) {
    hasFailedToolCall = toolCalls.some((_, index) => {
      const toolCall = arrayDataValue(toolCalls, index);
      return isRecord(toolCall) && recordValue(toolCall, "status") === "error";
    });
  }

  const writeEvidence: ReceiptWriteEvidence[] = [];
  const writes = recordValue(value, "writes");
  if (Array.isArray(writes)) {
    for (let i = 0; i < writes.length; i += 1) {
      const write = arrayDataValue(writes, i);
      if (!isRecord(write)) continue;
      const writePath = pointer(pointer(path, "writes"), String(i));
      writeEvidence.push({
        index: i,
        result: recordValue(write, "result"),
        resultPath: pointer(writePath, "result"),
        writeId: recordValue(write, "writeId"),
      });
    }
  }

  const errorValue = recordValue(value, "error");
  const errorText =
    errorValue instanceof SanitizedString
      ? recordValue(errorValue as unknown as Readonly<Record<string, unknown>>, "value")
      : undefined;
  const hasNonEmptyError = typeof errorText === "string" && errorText.length > 0;
  const hasRejectedWrite = writeEvidence.some((write) => write.result === "rejected");
  const hasFailedWrite = writeEvidence.some((write) => write.result !== "applied");

  return {
    statusPath,
    hasRejectedApproval,
    hasFailedToolCall,
    hasNonEmptyError,
    hasRejectionEvidence: hasRejectedApproval || hasRejectedWrite,
    hasFailureEvidence:
      hasRejectedApproval || hasFailedToolCall || hasFailedWrite || hasNonEmptyError,
    rejectedApprovalWriteIds,
    writes: writeEvidence,
  };
}

function rejectRejectedApprovalsOutsideRejectedOrError(
  evidence: ReceiptStatusEvidence,
  errors: ReceiptValidationError[],
): void {
  if (evidence.hasRejectedApproval) {
    addError(
      errors,
      evidence.statusPath,
      "must be rejected or error when approvals include a rejection",
    );
  }
}

function rejectFailedToolCallsForOk(
  evidence: ReceiptStatusEvidence,
  errors: ReceiptValidationError[],
): void {
  if (evidence.hasFailedToolCall) {
    addError(errors, evidence.statusPath, "must not be ok when any tool call failed");
  }
}

function rejectNonAppliedWritesForOk(
  evidence: ReceiptStatusEvidence,
  errors: ReceiptValidationError[],
): void {
  if (evidence.writes.some((write) => write.result !== "applied")) {
    addError(errors, evidence.statusPath, "must not be ok when any write did not apply");
  }
}

function rejectAppliedWritesForApprovalPending(
  evidence: ReceiptStatusEvidence,
  errors: ReceiptValidationError[],
): void {
  if (evidence.writes.some((write) => write.result === "applied")) {
    addError(errors, evidence.statusPath, "must not be approval_pending when any write applied");
  }
}

function requireRejectionEvidenceForRejected(
  evidence: ReceiptStatusEvidence,
  errors: ReceiptValidationError[],
): void {
  if (!evidence.hasRejectionEvidence) {
    addError(
      errors,
      evidence.statusPath,
      "must include rejected approval or rejected write evidence",
    );
  }
}

function rejectAppliedWritesForRejectedApprovals(
  evidence: ReceiptStatusEvidence,
  errors: ReceiptValidationError[],
): void {
  for (const write of evidence.writes) {
    if (
      write.result === "applied" &&
      typeof write.writeId === "string" &&
      evidence.rejectedApprovalWriteIds.has(write.writeId)
    ) {
      addError(
        errors,
        write.resultPath,
        "must not be applied when the matching approval was rejected",
      );
    }
  }
}

function requireFailureEvidenceForError(
  evidence: ReceiptStatusEvidence,
  errors: ReceiptValidationError[],
): void {
  if (!evidence.hasFailureEvidence) {
    addError(errors, evidence.statusPath, "must include failure evidence when status is error");
  }
}

function rejectAppliedWritesForStalled(
  evidence: ReceiptStatusEvidence,
  errors: ReceiptValidationError[],
): void {
  if (evidence.writes.some((write) => write.result === "applied")) {
    addError(errors, evidence.statusPath, "must not be stalled when any write applied");
  }
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

function validateToolCall(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
  context: ReceiptValidationContext,
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, TOOL_CALL_KEYS, errors);
  validateRequired(value, "toolId", path, errors, validateToolCallIdValue);
  validateRequired(value, "toolName", path, errors, validateString);
  validateRequired(value, "inputs", path, errors, (v, p, e) =>
    validateFrozenArgs(v, p, e, context),
  );
  validateRequired(value, "output", path, errors, validateSanitizedString);
  validateRequired(value, "startedAt", path, errors, validateDate);
  validateRequired(value, "finishedAt", path, errors, validateDate);
  validateRequired(value, "status", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, TOOL_CALL_STATUS_VALUES, "must be a valid tool call status"),
  );
  validateOptional(value, "error", path, errors, validateSanitizedString);
  validateTemporalOrdering(
    recordValue(value, "startedAt"),
    "startedAt",
    recordValue(value, "finishedAt"),
    "finishedAt",
    pointer(path, "finishedAt"),
    errors,
    true,
  );
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
  validateRequired(value, "tokenVerdict", path, errors, validateBrokerTokenVerdict);
  validateRequired(value, "decidedAt", path, errors, validateDate);

  // Cross-field invariant: the signed token must reference this receipt.
  const signedToken = recordValue(value, "signedToken");
  const token = parseSignedApprovalTokenForValidation(
    signedToken,
    pointer(path, "signedToken"),
    errors,
  );
  if (token !== undefined) {
    const tokenPath = pointer(path, "signedToken");
    if (!isReceiptCoSignClaim(token.claim)) {
      addError(errors, pointer(pointer(tokenPath, "claim"), "kind"), "must be receipt_co_sign");
      return;
    }
    if (typeof receiptId === "string" && token.claim.receiptId !== receiptId) {
      addError(
        errors,
        pointer(pointer(tokenPath, "claim"), "receiptId"),
        "must match enclosing receipt id",
      );
    }
    const role = recordValue(value, "role");
    if (typeof role === "string" && token.scope.role !== role) {
      addError(errors, pointer(pointer(tokenPath, "scope"), "role"), "must match approval role");
    }
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
  expectedStore: MemoryStore,
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, MEMORY_WRITE_KEYS, errors);
  validateRequired(value, "store", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, MEMORY_STORE_VALUES, "must be notebook or wiki"),
  );
  const store = recordValue(value, "store");
  if ((store === "notebook" || store === "wiki") && store !== expectedStore) {
    addError(errors, pointer(path, "store"), `must be ${expectedStore}`);
  }
  validateRequired(value, "slug", path, errors, validateString);
  validateRequired(value, "hash", path, errors, validateSha256HexValue);
  validateRequired(value, "citation", path, errors, validateString);
}

function validateBrokerTokenVerdict(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, BROKER_TOKEN_VERDICT_KEYS, errors);
  validateRequired(value, "status", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, BROKER_TOKEN_VERDICT_STATUS_VALUES, "must be a valid token verdict"),
  );
  validateRequired(value, "verifiedAt", path, errors, validateDate);
}

function validateSignedApprovalToken(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  parseSignedApprovalTokenForValidation(value, path, errors);
}

function validateExternalWrite(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
  receiptId: unknown,
  context: ReceiptValidationContext,
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, EXTERNAL_WRITE_KEYS, errors);
  validateRequired(value, "writeId", path, errors, validateWriteIdValue);
  validateRequired(value, "action", path, errors, validateString);
  validateRequired(value, "target", path, errors, validateString);
  validateRequired(value, "idempotencyKey", path, errors, validateIdempotencyKeyValue);
  validateRequired(value, "proposedDiff", path, errors, (v, p, e) =>
    validateFrozenArgs(v, p, e, context),
  );
  validateRequired(value, "approvalToken", path, errors, (v, p, e) =>
    validateNullable(v, p, e, validateSignedApprovalToken),
  );
  validateOptional(value, "approvedAt", path, errors, validateDate);
  validateRequired(value, "result", path, errors, (v, p, e) =>
    validateLiteral(v, p, e, WRITE_RESULT_VALUES, "must be a valid write result"),
  );
  validateOptional(value, "failureMetadata", path, errors, validateWriteFailureMetadata);

  // Per-state field requirements mirror the discriminated-union shape in
  // receipt-types.ts. The codec enforces the same invariants in
  // `externalWriteFromJson` — the two sides must agree, otherwise we get
  // round-trip skew (codec accepts what validator rejects, or vice versa).
  const result = recordValue(value, "result");
  const appliedDiffPath = pointer(path, "appliedDiff");
  const postWriteVerifyPath = pointer(path, "postWriteVerify");
  const failureMetadataPath = pointer(path, "failureMetadata");
  const appliedDiffValue = recordValue(value, "appliedDiff");
  const postWriteVerifyValue = recordValue(value, "postWriteVerify");
  const hasFailureMetadata =
    hasOwn(value, "failureMetadata") && recordValue(value, "failureMetadata") !== undefined;
  const requireFrozen = (val: unknown, p: string, state: string): void => {
    if (val === null) {
      addError(errors, p, `must be a FrozenArgs envelope (null is invalid for state "${state}")`);
    } else {
      validateFrozenArgs(val, p, errors, context);
    }
  };
  const requireNull = (val: unknown, p: string, state: string): void => {
    if (val !== null) {
      addError(errors, p, `must be null for state "${state}"`);
    }
  };
  if (result === "applied") {
    requireFrozen(appliedDiffValue, appliedDiffPath, "applied");
    requireFrozen(postWriteVerifyValue, postWriteVerifyPath, "applied");
    if (hasFailureMetadata) {
      addError(errors, failureMetadataPath, 'must be absent for state "applied"');
    }
  } else if (result === "rejected") {
    requireNull(appliedDiffValue, appliedDiffPath, "rejected");
    requireNull(postWriteVerifyValue, postWriteVerifyPath, "rejected");
  } else if (result === "partial") {
    requireFrozen(appliedDiffValue, appliedDiffPath, "partial");
    validateNullable(postWriteVerifyValue, postWriteVerifyPath, errors, (v, p, e) =>
      validateFrozenArgs(v, p, e, context),
    );
  } else if (result === "rollback") {
    requireFrozen(appliedDiffValue, appliedDiffPath, "rollback");
    requireNull(postWriteVerifyValue, postWriteVerifyPath, "rollback");
  }

  // Cross-field invariants: when present, the approval token must reference
  // this receipt, bind to the proposedDiff hash, and bind to writeId when the
  // token is write-scoped. RFC §6 invariant chain.
  const approvalToken = recordValue(value, "approvalToken");
  const proposedDiff = recordValue(value, "proposedDiff");
  if (isRecord(approvalToken)) {
    const token = parseSignedApprovalTokenForValidation(
      approvalToken,
      pointer(path, "approvalToken"),
      errors,
    );
    if (token === undefined) return;
    const tokenPath = pointer(path, "approvalToken");
    if (!isReceiptCoSignClaim(token.claim) || !isReceiptCoSignScope(token.scope)) {
      addError(errors, pointer(pointer(tokenPath, "claim"), "kind"), "must be receipt_co_sign");
    } else {
      const claimPath = pointer(tokenPath, "claim");
      if (typeof receiptId === "string" && token.claim.receiptId !== receiptId) {
        addError(errors, pointer(claimPath, "receiptId"), "must match enclosing receipt id");
      }
      // Re-derive the diff hash rather than trusting `proposedDiff.hash` from
      // an `instanceof`-passing object. A forged `proposedDiff`
      // (`Object.create(FrozenArgs.prototype)` with attacker-chosen `.hash`)
      // would otherwise pass this check by setting both sides to the same
      // value. `validateFrozenArgs` runs earlier on the same proposedDiff and
      // also re-derives, so today the order chains the invariant — but doing
      // the re-derivation locally here means the cross-field rule's
      // correctness no longer depends on validator ordering.
      if (proposedDiff instanceof FrozenArgs) {
        try {
          const proposedCanonicalJson = recordValue(
            proposedDiff as unknown as Readonly<Record<string, unknown>>,
            "canonicalJson",
          );
          if (typeof proposedCanonicalJson === "string") {
            const reFrozen = FrozenArgs.fromCanonical(proposedCanonicalJson);
            if (token.claim.frozenArgsHash !== reFrozen.hash) {
              addError(
                errors,
                pointer(claimPath, "frozenArgsHash"),
                "must match this write's proposedDiff hash",
              );
            }
          }
        } catch {
          // validateFrozenArgs already reported the field-level reason for the
          // forged/invalid FrozenArgs; the cross-field hash check has nothing
          // to add and would otherwise collapse all errors into path: "".
        }
      }
      const tokenWriteId = token.claim.writeId;
      if (tokenWriteId !== undefined && tokenWriteId !== recordValue(value, "writeId")) {
        addError(errors, pointer(claimPath, "writeId"), "must match this write's writeId");
      }
      const approvedAt = recordValue(value, "approvedAt");
      if (
        approvedAt instanceof Date &&
        !Number.isNaN(approvedAt.getTime()) &&
        approvedAt.getTime() < token.notBefore
      ) {
        addError(
          errors,
          pointer(path, "approvedAt"),
          "must be at or after approvalToken.notBefore",
        );
      }
    }
  }
}

function validateWriteFailureMetadata(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, WRITE_FAILURE_METADATA_KEYS, errors);
  validateRequired(value, "code", path, errors, validateString);
  validateRequired(value, "retryable", path, errors, validateBoolean);
  validateOptional(value, "retryAfterMs", path, errors, validateNonNegativeInteger);
  validateOptional(value, "terminalReason", path, errors, validateSanitizedString);
}

function validateRequired(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  errors: ReceiptValidationError[],
  validator: (value: unknown, path: string, errors: ReceiptValidationError[]) => void,
): void {
  const fieldPath = pointer(basePath, key);
  const value = recordValue(record, key);
  if (!hasOwn(record, key) || value === undefined) {
    addError(errors, fieldPath, "is required");
    return;
  }
  validator(value, fieldPath, errors);
}

function validateOptional(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  errors: ReceiptValidationError[],
  validator: (value: unknown, path: string, errors: ReceiptValidationError[]) => void,
): void {
  const value = recordValue(record, key);
  if (!hasOwn(record, key) || value === undefined) return;
  validator(value, pointer(basePath, key), errors);
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
    itemValidator(arrayDataValue(value, i), pointer(path, String(i)), errors);
  }
}

function arrayDataValue(value: readonly unknown[], index: number): unknown {
  const descriptor = Object.getOwnPropertyDescriptor(value, String(index));
  return descriptor !== undefined && "value" in descriptor ? descriptor.value : undefined;
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

function parseSignedApprovalTokenForValidation(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): ReturnType<typeof signedApprovalTokenFromJson> | undefined {
  try {
    return signedApprovalTokenFromJson(value, path);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    const separator = message.indexOf(": ");
    if (separator > 0) {
      addError(errors, message.slice(0, separator), message.slice(separator + 2));
    } else {
      addError(errors, path, message);
    }
    return undefined;
  }
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

function validateTemporalOrdering(
  earlier: unknown,
  earlierName: string,
  later: unknown,
  laterName: string,
  errorPath: string,
  errors: ReceiptValidationError[],
  allowEqual: boolean,
): void {
  if (!isValidDate(earlier) || !isValidDate(later)) {
    return;
  }
  const earlierTime = earlier.getTime();
  const laterTime = later.getTime();
  const ordered = allowEqual ? laterTime >= earlierTime : laterTime > earlierTime;
  if (!ordered) {
    const relation = allowEqual ? "after or equal to" : "after";
    addError(
      errors,
      errorPath,
      `must be ${relation} ${earlierName} (got ${laterName}=${later.toISOString()} ${earlierName}=${earlier.toISOString()})`,
    );
  }
}

function isValidDate(value: unknown): value is Date {
  return value instanceof Date && !Number.isNaN(value.getTime());
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

function validateThreadIdValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isThreadId(value)) addError(errors, path, "must be an uppercase ULID ThreadId");
}

function validateReceiptSchemaVersionValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (value !== 1 && value !== 2) addError(errors, path, "must be 1 or 2");
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

function validateWriteIdValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isWriteId(value)) addError(errors, path, "must be a valid WriteId");
}

function validateIdempotencyKeyValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isIdempotencyKey(value)) {
    addError(errors, path, "must match /^[A-Za-z0-9_-]{1,128}$/");
  }
}

function validateSha256HexValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isSha256Hex(value)) addError(errors, path, "must be a sha256 hex digest");
}

function validateFrozenArgs(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
  context: ReceiptValidationContext,
): void {
  // `instanceof` alone is not a freeze-boundary check: an attacker can produce
  // `Object.create(FrozenArgs.prototype)` with mismatched canonicalJson/hash
  // and pass the type-system check. Re-derive both fields and assert byte
  // equality to make forgery observable.
  if (!(value instanceof FrozenArgs)) {
    addError(errors, path, "must be FrozenArgs");
    return;
  }
  const frozenRecord = value as unknown as Readonly<Record<string, unknown>>;
  const canonicalJson = recordValue(frozenRecord, "canonicalJson");
  const hash = recordValue(frozenRecord, "hash");
  if (typeof canonicalJson !== "string") {
    addError(errors, pointer(path, "canonicalJson"), "must be a string");
    return;
  }
  if (!isSha256Hex(hash)) {
    addError(errors, pointer(path, "hash"), "must be a sha256 hex digest");
    return;
  }
  // The receipt decoder has already parsed canonicalJson and recomputed the
  // hash for instances it records in this set. Hand-built receipts call the
  // public validator without the set, so they still take the full re-derive
  // path instead of trusting `instanceof`.
  if (context.recomputedFrozenArgs.has(value)) {
    return;
  }
  try {
    const reFrozen = FrozenArgs.fromCanonical(canonicalJson);
    if (reFrozen.hash !== hash) {
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
  const sanitizedValue = recordValue(
    value as unknown as Readonly<Record<string, unknown>>,
    "value",
  );
  if (typeof sanitizedValue !== "string") {
    addError(errors, pointer(path, "value"), "must be a string");
    return;
  }
  try {
    const reSanitized = SanitizedString.fromUnknown(sanitizedValue);
    if (reSanitized.value !== sanitizedValue) {
      addError(errors, path, "must already be sanitized");
    }
  } catch (err) {
    addError(errors, path, err instanceof Error ? err.message : "must be sanitized");
  }
}
