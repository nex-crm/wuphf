import type { Brand } from "./brand.ts";
import { canonicalJSON } from "./canonical-json.ts";
import { FrozenArgs } from "./frozen-args.ts";
import { SanitizedString } from "./sanitized-string.ts";
import { asSha256Hex, isSha256Hex, type Sha256Hex } from "./sha256.ts";

export type ReceiptId = Brand<string, "ReceiptId">;
export type AgentSlug = Brand<string, "AgentSlug">;
export type TaskId = Brand<string, "TaskId">;
export type ProviderKind = Brand<string, "ProviderKind">;
export type ToolCallId = Brand<string, "ToolCallId">;
export type ApprovalId = Brand<string, "ApprovalId">;

export type ReceiptStatus = "ok" | "error" | "stalled" | "approval_pending" | "rejected";

export type RiskClass = "low" | "medium" | "high" | "critical";

export type WriteResult = "applied" | "rejected" | "partial" | "rollback";

export type TriggerKind =
  | "human_message"
  | "scheduler"
  | "mention"
  | "webhook"
  | "agent_message"
  | "system";

export interface SourceRead {
  readonly provider: string;
  readonly entityType: string;
  readonly entityId: string;
  readonly fetchedAt: Date;
  readonly hash: Sha256Hex;
  readonly citation: string;
  readonly rawRef?: string | undefined;
}

export interface ToolCall {
  readonly toolId: ToolCallId;
  readonly toolName: string;
  readonly inputs: FrozenArgs;
  readonly output: SanitizedString;
  readonly startedAt: Date;
  readonly finishedAt: Date;
  readonly status: "ok" | "error";
  readonly error?: SanitizedString | undefined;
}

export interface ApprovalEvent {
  readonly approvalId: ApprovalId;
  readonly role: "viewer" | "approver" | "host";
  readonly decision: "approve" | "reject" | "abstain";
  readonly signedToken: SignedApprovalToken;
  readonly decidedAt: Date;
}

export interface FileChange {
  readonly path: string;
  readonly mode: "created" | "modified" | "deleted";
  readonly beforeHash?: Sha256Hex | undefined;
  readonly afterHash: Sha256Hex;
  readonly linesAdded: number;
  readonly linesRemoved: number;
}

export interface CommitRef {
  readonly sha: string;
  readonly message: SanitizedString;
  readonly author: string;
  readonly authorEmail: string;
  readonly parentSha?: string | undefined;
  readonly signed: boolean;
}

export interface MemoryWriteRef {
  readonly store: "notebook" | "wiki";
  readonly slug: string;
  readonly hash: Sha256Hex;
  readonly citation: string;
}

export interface SignedApprovalToken {
  readonly signerIdentity: string;
  readonly role: "viewer" | "approver" | "host";
  readonly receiptId: ReceiptId;
  readonly frozenArgsHash: Sha256Hex;
  readonly riskClass: RiskClass;
  readonly expiresAt: Date;
  readonly webauthnAssertion?: string | undefined;
  readonly brokerVerificationStatus: "valid" | "expired" | "tampered";
}

export interface ExternalWrite {
  readonly action: string;
  readonly target: string;
  readonly idempotencyKey: string;
  readonly proposedDiff: FrozenArgs;
  readonly appliedDiff: FrozenArgs | null;
  readonly approvalToken: SignedApprovalToken | null;
  readonly approvedAt?: Date | undefined;
  readonly result: WriteResult;
  readonly postWriteVerify: FrozenArgs | null;
}

export interface ReceiptSnapshot {
  readonly id: ReceiptId;
  readonly agentSlug: AgentSlug;
  readonly taskId: TaskId;
  readonly triggerKind: TriggerKind;
  readonly triggerRef: string;
  readonly startedAt: Date;
  readonly finishedAt?: Date | undefined;
  readonly status: ReceiptStatus;

  readonly providerKind: ProviderKind;
  readonly model: string;
  readonly promptHash: Sha256Hex;
  readonly toolManifest: Sha256Hex;

  readonly toolCalls: readonly ToolCall[];
  readonly approvals: readonly ApprovalEvent[];
  readonly filesChanged: readonly FileChange[];
  readonly commits: readonly CommitRef[];

  readonly sourceReads: readonly SourceRead[];
  readonly writes: readonly ExternalWrite[];

  readonly inputTokens: number;
  readonly outputTokens: number;
  readonly cacheReadTokens: number;
  readonly cacheCreationTokens: number;
  readonly costUsd: number;

  readonly finalMessage?: SanitizedString | undefined;
  readonly error?: SanitizedString | undefined;

  readonly notebookWrites: readonly MemoryWriteRef[];
  readonly wikiWrites: readonly MemoryWriteRef[];

  readonly worktreePath?: string | undefined;
  readonly gitHeadStart?: string | undefined;
  readonly gitHeadEnd?: string | undefined;

  readonly schemaVersion: 1;
}

export type ReceiptValidationError = { path: string; message: string };
export type ReceiptValidationResult =
  | { ok: true }
  | { ok: false; errors: ReceiptValidationError[] };

const ULID_RE = /^[0-9A-HJKMNP-TV-Z]{26}$/;
const AGENT_SLUG_RE = /^[a-z0-9][a-z0-9_-]*$/;
const LOCAL_ID_RE = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;

const PROVIDER_KIND_VALUES = ["anthropic", "openai", "openai-compat", "openclaw"] as const;
const PROVIDER_KIND_SET: ReadonlySet<string> = new Set(PROVIDER_KIND_VALUES);

const RECEIPT_STATUS_VALUES = [
  "ok",
  "error",
  "stalled",
  "approval_pending",
  "rejected",
] as const satisfies readonly ReceiptStatus[];
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

export function asReceiptId(s: string): ReceiptId {
  if (!ULID_RE.test(s)) {
    throw new Error("not a ReceiptId ULID");
  }
  return s as ReceiptId;
}

export function isReceiptId(s: unknown): s is ReceiptId {
  return typeof s === "string" && ULID_RE.test(s);
}

export function asAgentSlug(s: string): AgentSlug {
  if (!AGENT_SLUG_RE.test(s)) {
    throw new Error("not an AgentSlug");
  }
  return s as AgentSlug;
}

export function isAgentSlug(s: unknown): s is AgentSlug {
  return typeof s === "string" && AGENT_SLUG_RE.test(s);
}

export function asTaskId(s: string): TaskId {
  if (!ULID_RE.test(s)) {
    throw new Error("not a TaskId ULID");
  }
  return s as TaskId;
}

export function isTaskId(s: unknown): s is TaskId {
  return typeof s === "string" && ULID_RE.test(s);
}

export function asProviderKind(s: string): ProviderKind {
  if (!PROVIDER_KIND_SET.has(s)) {
    throw new Error("not a supported ProviderKind");
  }
  return s as ProviderKind;
}

export function isProviderKind(s: unknown): s is ProviderKind {
  return typeof s === "string" && PROVIDER_KIND_SET.has(s);
}

export function asToolCallId(s: string): ToolCallId {
  if (!LOCAL_ID_RE.test(s)) {
    throw new Error("not a ToolCallId");
  }
  return s as ToolCallId;
}

export function isToolCallId(s: unknown): s is ToolCallId {
  return typeof s === "string" && LOCAL_ID_RE.test(s);
}

export function asApprovalId(s: string): ApprovalId {
  if (!LOCAL_ID_RE.test(s)) {
    throw new Error("not an ApprovalId");
  }
  return s as ApprovalId;
}

export function isApprovalId(s: unknown): s is ApprovalId {
  return typeof s === "string" && LOCAL_ID_RE.test(s);
}

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

const RECEIPT_KEYS: ReadonlySet<string> = new Set<string>([
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
const SOURCE_READ_KEYS: ReadonlySet<string> = new Set<string>([
  "provider",
  "entityType",
  "entityId",
  "fetchedAt",
  "hash",
  "citation",
  "rawRef",
]);
const TOOL_CALL_KEYS: ReadonlySet<string> = new Set<string>([
  "toolId",
  "toolName",
  "inputs",
  "output",
  "startedAt",
  "finishedAt",
  "status",
  "error",
]);
const APPROVAL_EVENT_KEYS: ReadonlySet<string> = new Set<string>([
  "approvalId",
  "role",
  "decision",
  "signedToken",
  "decidedAt",
]);
const FILE_CHANGE_KEYS: ReadonlySet<string> = new Set<string>([
  "path",
  "mode",
  "beforeHash",
  "afterHash",
  "linesAdded",
  "linesRemoved",
]);
const COMMIT_REF_KEYS: ReadonlySet<string> = new Set<string>([
  "sha",
  "message",
  "author",
  "authorEmail",
  "parentSha",
  "signed",
]);
const MEMORY_WRITE_KEYS: ReadonlySet<string> = new Set<string>([
  "store",
  "slug",
  "hash",
  "citation",
]);
const SIGNED_APPROVAL_TOKEN_KEYS: ReadonlySet<string> = new Set<string>([
  "signerIdentity",
  "role",
  "receiptId",
  "frozenArgsHash",
  "riskClass",
  "expiresAt",
  "webauthnAssertion",
  "brokerVerificationStatus",
]);
const EXTERNAL_WRITE_KEYS: ReadonlySet<string> = new Set<string>([
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

function validateKnownKeys(
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
    if (v !== 1) {
      addError(e, p, "must be 1");
    }
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
  if (!hasOwn(record, key) || record[key] === undefined) {
    return;
  }
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
  if (value === null) {
    return;
  }
  validator(value, path, errors);
}

function validateString(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (typeof value !== "string") {
    addError(errors, path, "must be a string");
  }
}

function validateBoolean(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (typeof value !== "boolean") {
    addError(errors, path, "must be a boolean");
  }
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
  if (!isReceiptId(value)) {
    addError(errors, path, "must be an uppercase ULID ReceiptId");
  }
}

function validateAgentSlugValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isAgentSlug(value)) {
    addError(errors, path, "must match /^[a-z0-9][a-z0-9_-]*$/");
  }
}

function validateTaskIdValue(value: unknown, path: string, errors: ReceiptValidationError[]): void {
  if (!isTaskId(value)) {
    addError(errors, path, "must be an uppercase ULID TaskId");
  }
}

function validateProviderKindValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isProviderKind(value)) {
    addError(errors, path, "must be a supported ProviderKind");
  }
}

function validateToolCallIdValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isToolCallId(value)) {
    addError(errors, path, "must be a valid ToolCallId");
  }
}

function validateApprovalIdValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isApprovalId(value)) {
    addError(errors, path, "must be a valid ApprovalId");
  }
}

function validateSha256HexValue(
  value: unknown,
  path: string,
  errors: ReceiptValidationError[],
): void {
  if (!isSha256Hex(value)) {
    addError(errors, path, "must be a sha256 hex digest");
  }
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

function signedApprovalTokenToJsonValue(t: SignedApprovalToken): Record<string, unknown> {
  return omitUndefined({
    signerIdentity: t.signerIdentity,
    role: t.role,
    receiptId: t.receiptId,
    frozenArgsHash: t.frozenArgsHash,
    riskClass: t.riskClass,
    expiresAt: dateToJson(t.expiresAt),
    webauthnAssertion: t.webauthnAssertion,
    brokerVerificationStatus: t.brokerVerificationStatus,
  });
}

function externalWriteToJsonValue(w: ExternalWrite): Record<string, unknown> {
  return omitUndefined({
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

function signedApprovalTokenFromJson(value: unknown, path: string): SignedApprovalToken {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, SIGNED_APPROVAL_TOKEN_KEYS);
  const webauthnAssertion = optionalStringFromJson(record, "webauthnAssertion", path);
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
    frozenArgsHash: asSha256Hex(requiredStringFromJson(record, "frozenArgsHash", path)),
    riskClass: requiredLiteralFromJson(record, "riskClass", path, RISK_CLASS_VALUES),
    expiresAt: requiredDateFromJson(record, "expiresAt", path),
    ...(webauthnAssertion === undefined ? {} : { webauthnAssertion }),
    brokerVerificationStatus: requiredLiteralFromJson(
      record,
      "brokerVerificationStatus",
      path,
      BROKER_VERIFICATION_STATUS_VALUES,
    ),
  };
}

function externalWriteFromJson(value: unknown, path: string): ExternalWrite {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, EXTERNAL_WRITE_KEYS);
  const approvedAt = optionalDateFromJson(record, "approvedAt", path);
  return {
    action: requiredStringFromJson(record, "action", path),
    target: requiredStringFromJson(record, "target", path),
    idempotencyKey: requiredStringFromJson(record, "idempotencyKey", path),
    proposedDiff: frozenArgsFromJson(
      requiredFieldFromJson(record, "proposedDiff", path),
      pointer(path, "proposedDiff"),
    ),
    appliedDiff: nullableFrozenArgsFromJson(
      requiredFieldFromJson(record, "appliedDiff", path),
      pointer(path, "appliedDiff"),
    ),
    approvalToken: nullableSignedApprovalTokenFromJson(
      requiredFieldFromJson(record, "approvalToken", path),
      pointer(path, "approvalToken"),
    ),
    ...(approvedAt === undefined ? {} : { approvedAt }),
    result: requiredLiteralFromJson(record, "result", path, WRITE_RESULT_VALUES),
    postWriteVerify: nullableFrozenArgsFromJson(
      requiredFieldFromJson(record, "postWriteVerify", path),
      pointer(path, "postWriteVerify"),
    ),
  };
}

function frozenArgsToJsonValue(value: FrozenArgs): Record<string, unknown> {
  return {
    canonicalJson: value.canonicalJson,
    hash: value.hash,
  };
}

function optionalFrozenArgsToJsonValue(value: FrozenArgs | null): Record<string, unknown> | null {
  return value === null ? null : frozenArgsToJsonValue(value);
}

function frozenArgsFromJson(value: unknown, path: string): FrozenArgs {
  const record = requireRecord(value, path);
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
  if (!hasOwn(record, key)) {
    return undefined;
  }
  const value = record[key];
  if (value === undefined) {
    return undefined;
  }
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
  if (!hasOwn(record, key)) {
    return undefined;
  }
  const value = record[key];
  if (value === undefined) {
    return undefined;
  }
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
  if (!hasOwn(record, key)) {
    return undefined;
  }
  const value = record[key];
  if (value === undefined) {
    return undefined;
  }
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

function requireRecord(value: unknown, path: string): Readonly<Record<string, unknown>> {
  if (!isRecord(value)) {
    throw new Error(`${path}: must be an object`);
  }
  return value;
}

function assertKnownKeys(
  record: Readonly<Record<string, unknown>>,
  basePath: string,
  allowed: ReadonlySet<string>,
): void {
  for (const key of Object.keys(record)) {
    if (!allowed.has(key)) {
      throw new Error(`${pointer(basePath, key)}: is not allowed`);
    }
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasOwn(record: Readonly<Record<string, unknown>>, key: string): boolean {
  return Object.hasOwn(record, key);
}

function recordValue(record: Readonly<Record<string, unknown>>, key: string): unknown {
  return record[key];
}

function omitUndefined<T extends Record<string, unknown>>(input: T): T {
  const out: Partial<T> = {};
  for (const [key, value] of Object.entries(input) as [keyof T, T[keyof T]][]) {
    if (value !== undefined) {
      out[key] = value;
    }
  }
  return out as T;
}

function addError(errors: ReceiptValidationError[], path: string, message: string): void {
  errors.push({ path, message });
}

function pointer(base: string, segment: string): string {
  const escaped = segment.replace(/~/g, "~0").replace(/\//g, "~1");
  return `${base}/${escaped}`;
}

function formatValidationErrors(errors: readonly ReceiptValidationError[]): string {
  return errors.map((error) => `${error.path}: ${error.message}`).join("; ");
}
