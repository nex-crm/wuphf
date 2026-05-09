// Receipt codec — `receiptToJson` + `receiptFromJson`. Types live in
// `receipt-types.ts`; validators in `receipt-validator.ts`. The split exists
// because the combined module exceeded the 1500-LOC budget; the public API is
// preserved here via re-exports so consumers do not need to change imports.

import { canonicalJSON } from "./canonical-json.ts";
import { FrozenArgs } from "./frozen-args.ts";
import {
  type ApprovalClaims,
  type ApprovalEvent,
  asAgentSlug,
  asApprovalId,
  asProviderKind,
  asReceiptId,
  asTaskId,
  asToolCallId,
  asWriteId,
  type BrokerTokenVerdict,
  type CommitRef,
  type ExternalWrite,
  type FileChange,
  type MemoryWriteRef,
  type ReceiptSnapshot,
  type SignedApprovalToken,
  type SourceRead,
  type ToolCall,
} from "./receipt-types.ts";
import {
  assertKnownKeys,
  formatValidationErrors,
  hasOwn,
  omitUndefined,
  pointer,
  requireRecord,
} from "./receipt-utils.ts";
import {
  APPROVAL_CLAIMS_KEYS,
  APPROVAL_EVENT_KEYS,
  BROKER_TOKEN_VERDICT_KEYS,
  COMMIT_REF_KEYS,
  EXTERNAL_WRITE_KEYS,
  FILE_CHANGE_KEYS,
  FROZEN_ARGS_KEYS,
  MEMORY_WRITE_KEYS,
  RECEIPT_KEYS,
  SIGNED_APPROVAL_TOKEN_KEYS,
  SOURCE_READ_KEYS,
  TOOL_CALL_KEYS,
  validateReceipt,
} from "./receipt-validator.ts";
import { SanitizedString } from "./sanitized-string.ts";
import { asSha256Hex, type Sha256Hex } from "./sha256.ts";

const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;

const RECEIPT_STATUS_VALUES = ["ok", "error", "stalled", "approval_pending", "rejected"] as const;
const RISK_CLASS_VALUES = ["low", "medium", "high", "critical"] as const;
const WRITE_RESULT_VALUES = ["applied", "rejected", "partial", "rollback"] as const;
const TRIGGER_KIND_VALUES = [
  "human_message",
  "scheduler",
  "mention",
  "webhook",
  "agent_message",
  "system",
] as const;
const APPROVAL_ROLE_VALUES = ["viewer", "approver", "host"] as const;
const APPROVAL_DECISION_VALUES = ["approve", "reject", "abstain"] as const;
const TOOL_CALL_STATUS_VALUES = ["ok", "error"] as const;
const FILE_CHANGE_MODE_VALUES = ["created", "modified", "deleted"] as const;
const MEMORY_STORE_VALUES = ["notebook", "wiki"] as const;
const APPROVAL_TOKEN_ALGORITHM_VALUES = ["ed25519"] as const;
const BROKER_TOKEN_VERDICT_STATUS_VALUES = [
  "valid",
  "expired",
  "tampered",
  "wrong_signer",
  "wrong_write",
] as const;
const BASE64_RE = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;

// Re-exports — public surface stays stable across the file split.
export type {
  AgentSlug,
  ApprovalClaims,
  ApprovalEvent,
  ApprovalId,
  BrokerTokenVerdict,
  CommitRef,
  ExternalWrite,
  ExternalWriteApplied,
  ExternalWriteCommon,
  ExternalWritePartial,
  ExternalWriteRejected,
  ExternalWriteRollback,
  FileChange,
  MemoryWriteRef,
  ProviderKind,
  ReceiptId,
  ReceiptSnapshot,
  ReceiptStatus,
  ReceiptValidationError,
  ReceiptValidationResult,
  RiskClass,
  SignedApprovalToken,
  SourceRead,
  TaskId,
  ToolCall,
  ToolCallId,
  TriggerKind,
  WriteId,
  WriteResult,
} from "./receipt-types.ts";
export {
  asAgentSlug,
  asApprovalId,
  asProviderKind,
  asReceiptId,
  asTaskId,
  asToolCallId,
  asWriteId,
  isAgentSlug,
  isApprovalId,
  isProviderKind,
  isReceiptId,
  isTaskId,
  isToolCallId,
  isWriteId,
  PROVIDER_KIND_VALUES,
} from "./receipt-types.ts";
export { isReceiptSnapshot, validateReceipt } from "./receipt-validator.ts";

export function receiptToJson(r: ReceiptSnapshot): string {
  const validation = validateReceipt(r);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return canonicalJSON(receiptToJsonValue(r));
}

export function receiptFromJson(json: string): ReceiptSnapshot {
  const parsed: unknown = JSON.parse(json);
  const receipt = receiptJsonToSnapshot(parsed);
  const validation = validateReceipt(receipt);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return receipt;
}

function receiptToJsonValue(r: ReceiptSnapshot): Record<string, unknown> {
  return omitUndefined({
    id: r.id,
    agentSlug: r.agentSlug,
    taskId: r.taskId,
    triggerKind: r.triggerKind,
    triggerRef: r.triggerRef,
    startedAt: dateToJson(r.startedAt),
    finishedAt: optionalDateToJson(r.finishedAt),
    status: r.status,
    providerKind: r.providerKind,
    model: r.model,
    promptHash: r.promptHash,
    toolManifest: r.toolManifest,
    toolCalls: r.toolCalls.map(toolCallToJsonValue),
    approvals: r.approvals.map(approvalEventToJsonValue),
    filesChanged: r.filesChanged.map(fileChangeToJsonValue),
    commits: r.commits.map(commitRefToJsonValue),
    sourceReads: r.sourceReads.map(sourceReadToJsonValue),
    writes: r.writes.map(externalWriteToJsonValue),
    inputTokens: r.inputTokens,
    outputTokens: r.outputTokens,
    cacheReadTokens: r.cacheReadTokens,
    cacheCreationTokens: r.cacheCreationTokens,
    costUsd: r.costUsd,
    finalMessage: optionalSanitizedStringToJson(r.finalMessage),
    error: optionalSanitizedStringToJson(r.error),
    notebookWrites: r.notebookWrites.map(memoryWriteRefToJsonValue),
    wikiWrites: r.wikiWrites.map(memoryWriteRefToJsonValue),
    worktreePath: r.worktreePath,
    gitHeadStart: r.gitHeadStart,
    gitHeadEnd: r.gitHeadEnd,
    schemaVersion: r.schemaVersion,
  });
}

function sourceReadToJsonValue(s: SourceRead): Record<string, unknown> {
  return omitUndefined({
    provider: s.provider,
    entityType: s.entityType,
    entityId: s.entityId,
    fetchedAt: dateToJson(s.fetchedAt),
    hash: s.hash,
    citation: s.citation,
    rawRef: s.rawRef,
  });
}

function toolCallToJsonValue(t: ToolCall): Record<string, unknown> {
  return omitUndefined({
    toolId: t.toolId,
    toolName: t.toolName,
    inputs: frozenArgsToJsonValue(t.inputs),
    output: sanitizedStringToJson(t.output),
    startedAt: dateToJson(t.startedAt),
    finishedAt: dateToJson(t.finishedAt),
    status: t.status,
    error: optionalSanitizedStringToJson(t.error),
  });
}

function approvalEventToJsonValue(a: ApprovalEvent): Record<string, unknown> {
  return {
    approvalId: a.approvalId,
    role: a.role,
    decision: a.decision,
    signedToken: signedApprovalTokenToJsonValue(a.signedToken),
    tokenVerdict: brokerTokenVerdictToJsonValue(a.tokenVerdict),
    decidedAt: dateToJson(a.decidedAt),
  };
}

function fileChangeToJsonValue(f: FileChange): Record<string, unknown> {
  return omitUndefined({
    path: f.path,
    mode: f.mode,
    beforeHash: f.beforeHash,
    afterHash: f.afterHash,
    linesAdded: f.linesAdded,
    linesRemoved: f.linesRemoved,
  });
}

function commitRefToJsonValue(c: CommitRef): Record<string, unknown> {
  return omitUndefined({
    sha: c.sha,
    message: sanitizedStringToJson(c.message),
    author: c.author,
    authorEmail: c.authorEmail,
    parentSha: c.parentSha,
    signed: c.signed,
  });
}

function memoryWriteRefToJsonValue(m: MemoryWriteRef): Record<string, unknown> {
  return {
    store: m.store,
    slug: m.slug,
    hash: m.hash,
    citation: m.citation,
  };
}

function approvalClaimsToJsonValue(c: ApprovalClaims): Record<string, unknown> {
  return omitUndefined({
    signerIdentity: c.signerIdentity,
    role: c.role,
    receiptId: c.receiptId,
    writeId: c.writeId,
    frozenArgsHash: c.frozenArgsHash,
    riskClass: c.riskClass,
    issuedAt: dateToJson(c.issuedAt),
    expiresAt: dateToJson(c.expiresAt),
    webauthnAssertion: c.webauthnAssertion,
  });
}

function signedApprovalTokenToJsonValue(t: SignedApprovalToken): Record<string, unknown> {
  return {
    claims: approvalClaimsToJsonValue(t.claims),
    algorithm: t.algorithm,
    signerKeyId: t.signerKeyId,
    signature: t.signature,
  };
}

function brokerTokenVerdictToJsonValue(v: BrokerTokenVerdict): Record<string, unknown> {
  return {
    status: v.status,
    verifiedAt: dateToJson(v.verifiedAt),
  };
}

function externalWriteToJsonValue(w: ExternalWrite): Record<string, unknown> {
  return omitUndefined({
    writeId: w.writeId,
    action: w.action,
    target: w.target,
    idempotencyKey: w.idempotencyKey,
    proposedDiff: frozenArgsToJsonValue(w.proposedDiff),
    appliedDiff: optionalFrozenArgsToJsonValue(w.appliedDiff),
    approvalToken:
      w.approvalToken === null ? null : signedApprovalTokenToJsonValue(w.approvalToken),
    approvedAt: optionalDateToJson(w.approvedAt),
    result: w.result,
    postWriteVerify: optionalFrozenArgsToJsonValue(w.postWriteVerify),
  });
}

function receiptJsonToSnapshot(value: unknown): ReceiptSnapshot {
  const record = requireRecord(value, "");
  assertKnownKeys(record, "", RECEIPT_KEYS);
  const finishedAt = optionalDateFromJson(record, "finishedAt", "");
  const finalMessage = optionalSanitizedStringFromJson(record, "finalMessage", "");
  const error = optionalSanitizedStringFromJson(record, "error", "");
  const worktreePath = optionalStringFromJson(record, "worktreePath", "");
  const gitHeadStart = optionalStringFromJson(record, "gitHeadStart", "");
  const gitHeadEnd = optionalStringFromJson(record, "gitHeadEnd", "");

  return {
    id: asReceiptId(requiredStringFromJson(record, "id", "")),
    agentSlug: asAgentSlug(requiredStringFromJson(record, "agentSlug", "")),
    taskId: asTaskId(requiredStringFromJson(record, "taskId", "")),
    triggerKind: requiredLiteralFromJson(record, "triggerKind", "", TRIGGER_KIND_VALUES),
    triggerRef: requiredStringFromJson(record, "triggerRef", ""),
    startedAt: requiredDateFromJson(record, "startedAt", ""),
    ...(finishedAt === undefined ? {} : { finishedAt }),
    status: requiredLiteralFromJson(record, "status", "", RECEIPT_STATUS_VALUES),
    providerKind: asProviderKind(requiredStringFromJson(record, "providerKind", "")),
    model: requiredStringFromJson(record, "model", ""),
    promptHash: asSha256Hex(requiredStringFromJson(record, "promptHash", "")),
    toolManifest: asSha256Hex(requiredStringFromJson(record, "toolManifest", "")),
    toolCalls: requiredArrayFromJson(record, "toolCalls", "", toolCallFromJson),
    approvals: requiredArrayFromJson(record, "approvals", "", approvalEventFromJson),
    filesChanged: requiredArrayFromJson(record, "filesChanged", "", fileChangeFromJson),
    commits: requiredArrayFromJson(record, "commits", "", commitRefFromJson),
    sourceReads: requiredArrayFromJson(record, "sourceReads", "", sourceReadFromJson),
    writes: requiredArrayFromJson(record, "writes", "", externalWriteFromJson),
    inputTokens: requiredNonNegativeIntegerFromJson(record, "inputTokens", ""),
    outputTokens: requiredNonNegativeIntegerFromJson(record, "outputTokens", ""),
    cacheReadTokens: requiredNonNegativeIntegerFromJson(record, "cacheReadTokens", ""),
    cacheCreationTokens: requiredNonNegativeIntegerFromJson(record, "cacheCreationTokens", ""),
    costUsd: requiredNonNegativeFiniteNumberFromJson(record, "costUsd", ""),
    ...(finalMessage === undefined ? {} : { finalMessage }),
    ...(error === undefined ? {} : { error }),
    notebookWrites: requiredArrayFromJson(record, "notebookWrites", "", memoryWriteRefFromJson),
    wikiWrites: requiredArrayFromJson(record, "wikiWrites", "", memoryWriteRefFromJson),
    ...(worktreePath === undefined ? {} : { worktreePath }),
    ...(gitHeadStart === undefined ? {} : { gitHeadStart }),
    ...(gitHeadEnd === undefined ? {} : { gitHeadEnd }),
    schemaVersion: requiredSchemaVersionFromJson(record, "schemaVersion", ""),
  };
}

function sourceReadFromJson(value: unknown, path: string): SourceRead {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, SOURCE_READ_KEYS);
  const rawRef = optionalStringFromJson(record, "rawRef", path);
  return {
    provider: requiredStringFromJson(record, "provider", path),
    entityType: requiredStringFromJson(record, "entityType", path),
    entityId: requiredStringFromJson(record, "entityId", path),
    fetchedAt: requiredDateFromJson(record, "fetchedAt", path),
    hash: asSha256Hex(requiredStringFromJson(record, "hash", path)),
    citation: requiredStringFromJson(record, "citation", path),
    ...(rawRef === undefined ? {} : { rawRef }),
  };
}

function toolCallFromJson(value: unknown, path: string): ToolCall {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, TOOL_CALL_KEYS);
  const error = optionalSanitizedStringFromJson(record, "error", path);
  return {
    toolId: asToolCallId(requiredStringFromJson(record, "toolId", path)),
    toolName: requiredStringFromJson(record, "toolName", path),
    inputs: frozenArgsFromJson(
      requiredFieldFromJson(record, "inputs", path),
      pointer(path, "inputs"),
    ),
    output: sanitizedStringFromJson(
      requiredFieldFromJson(record, "output", path),
      pointer(path, "output"),
    ),
    startedAt: requiredDateFromJson(record, "startedAt", path),
    finishedAt: requiredDateFromJson(record, "finishedAt", path),
    status: requiredLiteralFromJson(record, "status", path, TOOL_CALL_STATUS_VALUES),
    ...(error === undefined ? {} : { error }),
  };
}

function approvalEventFromJson(value: unknown, path: string): ApprovalEvent {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, APPROVAL_EVENT_KEYS);
  return {
    approvalId: asApprovalId(requiredStringFromJson(record, "approvalId", path)),
    role: requiredLiteralFromJson(record, "role", path, APPROVAL_ROLE_VALUES),
    decision: requiredLiteralFromJson(record, "decision", path, APPROVAL_DECISION_VALUES),
    signedToken: signedApprovalTokenFromJson(
      requiredFieldFromJson(record, "signedToken", path),
      pointer(path, "signedToken"),
    ),
    tokenVerdict: brokerTokenVerdictFromJson(
      requiredFieldFromJson(record, "tokenVerdict", path),
      pointer(path, "tokenVerdict"),
    ),
    decidedAt: requiredDateFromJson(record, "decidedAt", path),
  };
}

function fileChangeFromJson(value: unknown, path: string): FileChange {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, FILE_CHANGE_KEYS);
  const beforeHash = optionalSha256HexFromJson(record, "beforeHash", path);
  return {
    path: requiredStringFromJson(record, "path", path),
    mode: requiredLiteralFromJson(record, "mode", path, FILE_CHANGE_MODE_VALUES),
    ...(beforeHash === undefined ? {} : { beforeHash }),
    afterHash: asSha256Hex(requiredStringFromJson(record, "afterHash", path)),
    linesAdded: requiredNonNegativeIntegerFromJson(record, "linesAdded", path),
    linesRemoved: requiredNonNegativeIntegerFromJson(record, "linesRemoved", path),
  };
}

function commitRefFromJson(value: unknown, path: string): CommitRef {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, COMMIT_REF_KEYS);
  const parentSha = optionalStringFromJson(record, "parentSha", path);
  return {
    sha: requiredStringFromJson(record, "sha", path),
    message: sanitizedStringFromJson(
      requiredFieldFromJson(record, "message", path),
      pointer(path, "message"),
    ),
    author: requiredStringFromJson(record, "author", path),
    authorEmail: requiredStringFromJson(record, "authorEmail", path),
    ...(parentSha === undefined ? {} : { parentSha }),
    signed: requiredBooleanFromJson(record, "signed", path),
  };
}

function memoryWriteRefFromJson(value: unknown, path: string): MemoryWriteRef {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, MEMORY_WRITE_KEYS);
  return {
    store: requiredLiteralFromJson(record, "store", path, MEMORY_STORE_VALUES),
    slug: requiredStringFromJson(record, "slug", path),
    hash: asSha256Hex(requiredStringFromJson(record, "hash", path)),
    citation: requiredStringFromJson(record, "citation", path),
  };
}

function approvalClaimsFromJson(value: unknown, path: string): ApprovalClaims {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, APPROVAL_CLAIMS_KEYS);
  const webauthnAssertion = optionalStringFromJson(record, "webauthnAssertion", path);
  const writeId = optionalStringFromJson(record, "writeId", path);
  const riskClass = requiredLiteralFromJson(record, "riskClass", path, RISK_CLASS_VALUES);
  if (
    (riskClass === "high" || riskClass === "critical") &&
    (typeof webauthnAssertion !== "string" || webauthnAssertion.length === 0)
  ) {
    throw new Error(
      `${pointer(path, "webauthnAssertion")}: must be a non-empty string for high/critical risk`,
    );
  }
  return {
    signerIdentity: requiredStringFromJson(record, "signerIdentity", path),
    role: requiredLiteralFromJson(record, "role", path, APPROVAL_ROLE_VALUES),
    receiptId: asReceiptId(requiredStringFromJson(record, "receiptId", path)),
    ...(writeId === undefined ? {} : { writeId: asWriteId(writeId) }),
    frozenArgsHash: asSha256Hex(requiredStringFromJson(record, "frozenArgsHash", path)),
    riskClass,
    issuedAt: requiredDateFromJson(record, "issuedAt", path),
    expiresAt: requiredDateFromJson(record, "expiresAt", path),
    ...(webauthnAssertion === undefined ? {} : { webauthnAssertion }),
  };
}

function signedApprovalTokenFromJson(value: unknown, path: string): SignedApprovalToken {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, SIGNED_APPROVAL_TOKEN_KEYS);
  const signature = requiredStringFromJson(record, "signature", path);
  if (signature.length === 0 || !BASE64_RE.test(signature)) {
    throw new Error(`${pointer(path, "signature")}: must be a non-empty base64 string`);
  }
  return {
    claims: approvalClaimsFromJson(
      requiredFieldFromJson(record, "claims", path),
      pointer(path, "claims"),
    ),
    algorithm: requiredLiteralFromJson(record, "algorithm", path, APPROVAL_TOKEN_ALGORITHM_VALUES),
    signerKeyId: requiredStringFromJson(record, "signerKeyId", path),
    signature,
  };
}

function brokerTokenVerdictFromJson(value: unknown, path: string): BrokerTokenVerdict {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, BROKER_TOKEN_VERDICT_KEYS);
  return {
    status: requiredLiteralFromJson(record, "status", path, BROKER_TOKEN_VERDICT_STATUS_VALUES),
    verifiedAt: requiredDateFromJson(record, "verifiedAt", path),
  };
}

function externalWriteFromJson(value: unknown, path: string): ExternalWrite {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, EXTERNAL_WRITE_KEYS);
  const approvedAt = optionalDateFromJson(record, "approvedAt", path);
  const result = requiredLiteralFromJson(record, "result", path, WRITE_RESULT_VALUES);
  const common = {
    writeId: asWriteId(requiredStringFromJson(record, "writeId", path)),
    action: requiredStringFromJson(record, "action", path),
    target: requiredStringFromJson(record, "target", path),
    idempotencyKey: requiredStringFromJson(record, "idempotencyKey", path),
    proposedDiff: frozenArgsFromJson(
      requiredFieldFromJson(record, "proposedDiff", path),
      pointer(path, "proposedDiff"),
    ),
    approvalToken: nullableSignedApprovalTokenFromJson(
      requiredFieldFromJson(record, "approvalToken", path),
      pointer(path, "approvalToken"),
    ),
    ...(approvedAt === undefined ? {} : { approvedAt }),
  };
  const appliedDiffPath = pointer(path, "appliedDiff");
  const postWriteVerifyPath = pointer(path, "postWriteVerify");
  const appliedDiffValue = requiredFieldFromJson(record, "appliedDiff", path);
  const postWriteVerifyValue = requiredFieldFromJson(record, "postWriteVerify", path);

  // Per-state field requirements mirror the discriminated-union shape in
  // receipt-types.ts. The validator enforces the same invariants — keep both
  // sides in sync; a divergence would let the codec accept records the
  // validator rejects (or vice versa) and break round-trips.
  switch (result) {
    case "applied":
      return {
        ...common,
        result,
        appliedDiff: requireNonNullFrozenArgs(appliedDiffValue, appliedDiffPath, "applied"),
        postWriteVerify: requireNonNullFrozenArgs(
          postWriteVerifyValue,
          postWriteVerifyPath,
          "applied",
        ),
      };
    case "rejected":
      requireNullField(appliedDiffValue, appliedDiffPath, "rejected");
      requireNullField(postWriteVerifyValue, postWriteVerifyPath, "rejected");
      return {
        ...common,
        result,
        appliedDiff: null,
        postWriteVerify: null,
      };
    case "partial":
      return {
        ...common,
        result,
        appliedDiff: requireNonNullFrozenArgs(appliedDiffValue, appliedDiffPath, "partial"),
        postWriteVerify: nullableFrozenArgsFromJson(postWriteVerifyValue, postWriteVerifyPath),
      };
    case "rollback":
      requireNullField(postWriteVerifyValue, postWriteVerifyPath, "rollback");
      return {
        ...common,
        result,
        appliedDiff: requireNonNullFrozenArgs(appliedDiffValue, appliedDiffPath, "rollback"),
        postWriteVerify: null,
      };
  }
}

function requireNonNullFrozenArgs(value: unknown, path: string, state: string): FrozenArgs {
  if (value === null) {
    throw new Error(
      `${path}: must be a FrozenArgs envelope (null is invalid for state "${state}")`,
    );
  }
  return frozenArgsFromJson(value, path);
}

function requireNullField(value: unknown, path: string, state: string): void {
  if (value !== null) {
    throw new Error(`${path}: must be null for state "${state}"`);
  }
}

function frozenArgsToJsonValue(value: FrozenArgs): Record<string, unknown> {
  return { canonicalJson: value.canonicalJson, hash: value.hash };
}

function optionalFrozenArgsToJsonValue(value: FrozenArgs | null): Record<string, unknown> | null {
  return value === null ? null : frozenArgsToJsonValue(value);
}

function frozenArgsFromJson(value: unknown, path: string): FrozenArgs {
  const record = requireRecord(value, path);
  // Reject unknown sibling keys: every other object in the receipt rejects
  // them, so a `{canonicalJson, hash, extra}` envelope here would be the
  // single boundary where un-hashed shadow data could survive a round-trip.
  assertKnownKeys(record, path, FROZEN_ARGS_KEYS);
  const canonicalJson = requiredStringFromJson(record, "canonicalJson", path);
  const expectedHash = asSha256Hex(requiredStringFromJson(record, "hash", path));
  const decoded: unknown = JSON.parse(canonicalJson);
  const frozen = FrozenArgs.freeze(decoded);
  if (frozen.canonicalJson !== canonicalJson) {
    throw new Error(`${pointer(path, "canonicalJson")}: must be RFC 8785 canonical JSON`);
  }
  if (frozen.hash !== expectedHash) {
    throw new Error(`${pointer(path, "hash")}: does not match canonicalJson`);
  }
  return frozen;
}

function nullableFrozenArgsFromJson(value: unknown, path: string): FrozenArgs | null {
  return value === null ? null : frozenArgsFromJson(value, path);
}

function sanitizedStringToJson(value: SanitizedString): string {
  return value.value;
}

function optionalSanitizedStringToJson(value: SanitizedString | undefined): string | undefined {
  return value === undefined ? undefined : sanitizedStringToJson(value);
}

function sanitizedStringFromJson(value: unknown, path: string): SanitizedString {
  if (typeof value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  const sanitized = SanitizedString.fromUnknown(value);
  if (sanitized.value !== value) {
    throw new Error(`${path}: must already be sanitized`);
  }
  return sanitized;
}

function optionalSanitizedStringFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): SanitizedString | undefined {
  if (!hasOwn(record, key)) return undefined;
  const value = record[key];
  if (value === undefined) return undefined;
  return sanitizedStringFromJson(value, pointer(basePath, key));
}

function nullableSignedApprovalTokenFromJson(
  value: unknown,
  path: string,
): SignedApprovalToken | null {
  return value === null ? null : signedApprovalTokenFromJson(value, path);
}

function dateToJson(value: Date): string {
  return value.toISOString();
}

function optionalDateToJson(value: Date | undefined): string | undefined {
  return value === undefined ? undefined : dateToJson(value);
}

function requiredDateFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): Date {
  const value = requiredStringFromJson(record, key, basePath);
  return dateFromJson(value, pointer(basePath, key));
}

function optionalDateFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): Date | undefined {
  if (!hasOwn(record, key)) return undefined;
  const value = record[key];
  if (value === undefined) return undefined;
  if (typeof value !== "string") {
    throw new Error(`${pointer(basePath, key)}: must be an ISO 8601 string`);
  }
  return dateFromJson(value, pointer(basePath, key));
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

function requiredFieldFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): unknown {
  if (!hasOwn(record, key) || record[key] === undefined) {
    throw new Error(`${pointer(basePath, key)}: is required`);
  }
  return record[key];
}

function requiredStringFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): string {
  const value = requiredFieldFromJson(record, key, basePath);
  if (typeof value !== "string") {
    throw new Error(`${pointer(basePath, key)}: must be a string`);
  }
  return value;
}

function optionalStringFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): string | undefined {
  if (!hasOwn(record, key)) return undefined;
  const value = record[key];
  if (value === undefined) return undefined;
  if (typeof value !== "string") {
    throw new Error(`${pointer(basePath, key)}: must be a string`);
  }
  return value;
}

function requiredBooleanFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): boolean {
  const value = requiredFieldFromJson(record, key, basePath);
  if (typeof value !== "boolean") {
    throw new Error(`${pointer(basePath, key)}: must be a boolean`);
  }
  return value;
}

function requiredNonNegativeIntegerFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): number {
  const value = requiredFieldFromJson(record, key, basePath);
  if (typeof value !== "number" || !Number.isInteger(value) || value < 0) {
    throw new Error(`${pointer(basePath, key)}: must be a non-negative integer`);
  }
  return value;
}

function requiredNonNegativeFiniteNumberFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): number {
  const value = requiredFieldFromJson(record, key, basePath);
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0) {
    throw new Error(`${pointer(basePath, key)}: must be a non-negative finite number`);
  }
  return value;
}

function requiredSchemaVersionFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): 1 {
  const value = requiredFieldFromJson(record, key, basePath);
  if (value !== 1) {
    throw new Error(`${pointer(basePath, key)}: must be 1`);
  }
  return 1;
}

function optionalSha256HexFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): Sha256Hex | undefined {
  const value = optionalStringFromJson(record, key, basePath);
  return value === undefined ? undefined : asSha256Hex(value);
}

function requiredLiteralFromJson<T extends readonly string[]>(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  choices: T,
): T[number] {
  const value = requiredStringFromJson(record, key, basePath);
  if (!choices.includes(value)) {
    throw new Error(`${pointer(basePath, key)}: must be one of ${choices.join(", ")}`);
  }
  return value;
}

function requiredArrayFromJson<T>(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  itemDecoder: (value: unknown, path: string) => T,
): readonly T[] {
  const value = requiredFieldFromJson(record, key, basePath);
  if (!Array.isArray(value)) {
    throw new Error(`${pointer(basePath, key)}: must be an array`);
  }
  return value.map((item, index) =>
    itemDecoder(item, pointer(pointer(basePath, key), String(index))),
  );
}
