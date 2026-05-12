// Receipt schema types + brand constructors. Pure data — no validators, no
// codec. The two heavy modules (receipt-validator, receipt-codec) import this
// module so that the file-size of any single file stays well below the cap.

import type { Brand } from "./brand.ts";
import {
  MAX_AGENT_SLUG_BYTES,
  MAX_APPROVAL_ID_BYTES,
  MAX_LOCAL_ID_BYTES,
  MAX_TOOL_CALL_ID_BYTES,
  MAX_WRITE_ID_BYTES,
  validateSignerIdentityBudget,
} from "./budgets.ts";
import type { FrozenArgs } from "./frozen-args.ts";
import type { SanitizedString } from "./sanitized-string.ts";
import type { Sha256Hex } from "./sha256.ts";

export type ReceiptId = Brand<string, "ReceiptId">;
export type AgentSlug = Brand<string, "AgentSlug">;
export type TaskId = Brand<string, "TaskId">;
// ProviderKind is intentionally a closed enum at the type level. The
// PROVIDER_KIND_VALUES tuple below is the single source of truth — adding a
// value there both extends the type and forces every `switch (providerKind)`
// statement in the codebase to handle it (no fallthrough). Don't widen this to
// `Brand<string, ...>`; that loses exhaustiveness and re-introduces the
// silent-drift problem the closed-enum policy exists to prevent.
export type ProviderKind = Brand<(typeof PROVIDER_KIND_VALUES)[number], "ProviderKind">;
export type ToolCallId = Brand<string, "ToolCallId">;
export type ApprovalId = Brand<string, "ApprovalId">;
export type WriteId = Brand<string, "WriteId">;
export type IdempotencyKey = Brand<string, "IdempotencyKey">;
export type ThreadId = Brand<string, "ThreadId">;
export type ThreadSpecRevisionId = Brand<string, "ThreadSpecRevisionId">;
export type SignerIdentity = Brand<string, "SignerIdentity">;

export type ReceiptStatus = "ok" | "error" | "stalled" | "approval_pending" | "rejected";

export type RiskClass = "low" | "medium" | "high" | "critical";

export type WriteResult = "applied" | "rejected" | "partial" | "rollback";

// Retry fields are per-failure hints on the receipt. Total attempt counting
// and retry budgets are broker-side policy, not receipt-layer wire state.
export interface WriteFailureMetadata {
  readonly code: string;
  readonly retryable: boolean;
  readonly retryAfterMs?: number | undefined;
  readonly terminalReason?: SanitizedString | undefined;
}

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
  readonly tokenVerdict: BrokerTokenVerdict;
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

export interface ApprovalClaims {
  readonly signerIdentity: SignerIdentity;
  readonly role: "viewer" | "approver" | "host";
  readonly receiptId: ReceiptId;
  readonly writeId?: WriteId | undefined;
  readonly frozenArgsHash: Sha256Hex;
  readonly riskClass: RiskClass;
  readonly issuedAt: Date;
  readonly expiresAt: Date;
  readonly webauthnAssertion?: string | undefined;
}

export interface SignedApprovalToken {
  readonly claims: ApprovalClaims;
  readonly algorithm: "ed25519";
  readonly signerKeyId: string;
  readonly signature: string;
}

export interface BrokerTokenVerdict {
  readonly status: "valid" | "expired" | "tampered" | "wrong_signer" | "wrong_write";
  readonly verifiedAt: Date;
}

// `ExternalWrite` is a discriminated union over `result`. Per-state field
// shapes are enforced at the type level so consumers can `switch (write.result)`
// to access fields that are guaranteed non-null for that state — and so a
// representation like `result: "applied"` with `appliedDiff: null` becomes a
// type error instead of a silent runtime impossibility. The validator and
// codec mirror these invariants at the wire boundary.
export interface ExternalWriteCommon {
  readonly writeId: WriteId;
  readonly action: string;
  readonly target: string;
  readonly idempotencyKey: IdempotencyKey;
  readonly proposedDiff: FrozenArgs;
  readonly approvalToken: SignedApprovalToken | null;
  readonly approvedAt?: Date | undefined;
}

export interface ExternalWriteApplied extends ExternalWriteCommon {
  readonly result: "applied";
  // Both required: an applied write has a known applied diff and a verified
  // post-state. If post-write verification was skipped (e.g. low-risk write
  // policy), the result is "partial", not "applied".
  readonly appliedDiff: FrozenArgs;
  readonly postWriteVerify: FrozenArgs;
  readonly failureMetadata?: undefined;
}

export interface ExternalWriteRejected extends ExternalWriteCommon {
  readonly result: "rejected";
  // Nothing was written, so neither field carries data.
  readonly appliedDiff: null;
  readonly postWriteVerify: null;
  readonly failureMetadata?: WriteFailureMetadata | undefined;
}

export interface ExternalWritePartial extends ExternalWriteCommon {
  readonly result: "partial";
  // Some bytes landed but verification couldn't confirm the full diff. The
  // applied diff is what the writer believes was committed; postWriteVerify
  // may be null if verification was attempted and failed.
  readonly appliedDiff: FrozenArgs;
  readonly postWriteVerify: FrozenArgs | null;
  readonly failureMetadata?: WriteFailureMetadata | undefined;
}

export interface ExternalWriteRollback extends ExternalWriteCommon {
  readonly result: "rollback";
  // The diff that was applied and then reverted. Verification is skipped for
  // rolled-back writes — the post-state is, by definition, the pre-state.
  readonly appliedDiff: FrozenArgs;
  readonly postWriteVerify: null;
  readonly failureMetadata?: WriteFailureMetadata | undefined;
}

export type ExternalWrite =
  | ExternalWriteApplied
  | ExternalWriteRejected
  | ExternalWritePartial
  | ExternalWriteRollback;

export interface ReceiptCore {
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
}

export type ReceiptSnapshotV1 = ReceiptCore & {
  readonly schemaVersion: 1;
  readonly threadId?: never;
};

export type ReceiptSnapshotV2 = ReceiptCore & {
  readonly schemaVersion: 2;
  readonly threadId?: ThreadId | undefined;
};

export type ReceiptSnapshot = ReceiptSnapshotV1 | ReceiptSnapshotV2;

export type ReceiptValidationError = { path: string; message: string };
export type ReceiptValidationResult =
  | { ok: true }
  | { ok: false; errors: ReceiptValidationError[] };

const ULID_RE = /^[0-9A-HJKMNP-TV-Z]{26}$/;
const AGENT_SLUG_RE = new RegExp(`^[a-z0-9][a-z0-9_-]{0,${MAX_AGENT_SLUG_BYTES - 1}}$`);
const LOCAL_ID_RE = new RegExp(`^[A-Za-z0-9][A-Za-z0-9._-]{0,${MAX_LOCAL_ID_BYTES - 1}}$`);
export const IDEMPOTENCY_KEY_RE = /^[A-Za-z0-9_-]{1,128}$/;

// Exported because ProviderKind is `Brand<(typeof PROVIDER_KIND_VALUES)[number], …>`
// — consumers that need to enumerate the supported providers (forms, picker
// UI, exhaustive switches) read this tuple. It is the single source of truth;
// keep the order stable so any UI sort that relies on it doesn't churn.
export const PROVIDER_KIND_VALUES = [
  "anthropic",
  "openai",
  "openai-compat",
  "ollama",
  "openclaw",
  "hermes-agent",
  "openclaw-http",
] as const;
const PROVIDER_KIND_SET: ReadonlySet<string> = new Set(PROVIDER_KIND_VALUES);

export function asReceiptId(s: string): ReceiptId {
  if (!ULID_RE.test(s)) throw new Error("not a ReceiptId ULID");
  return s as ReceiptId;
}

export function isReceiptId(s: unknown): s is ReceiptId {
  return typeof s === "string" && ULID_RE.test(s);
}

export function asAgentSlug(s: string): AgentSlug {
  if (!AGENT_SLUG_RE.test(s)) throw new Error("not an AgentSlug");
  return s as AgentSlug;
}

export function isAgentSlug(s: unknown): s is AgentSlug {
  return typeof s === "string" && AGENT_SLUG_RE.test(s);
}

export function asTaskId(s: string): TaskId {
  if (!ULID_RE.test(s)) throw new Error("not a TaskId ULID");
  return s as TaskId;
}

export function isTaskId(s: unknown): s is TaskId {
  return typeof s === "string" && ULID_RE.test(s);
}

export function asProviderKind(s: string): ProviderKind {
  if (!PROVIDER_KIND_SET.has(s)) throw new Error("not a supported ProviderKind");
  return s as ProviderKind;
}

export function isProviderKind(s: unknown): s is ProviderKind {
  return typeof s === "string" && PROVIDER_KIND_SET.has(s);
}

export function asToolCallId(s: string): ToolCallId {
  if (!isBoundedLocalId(s, MAX_TOOL_CALL_ID_BYTES)) throw new Error("not a ToolCallId");
  return s as ToolCallId;
}

export function isToolCallId(s: unknown): s is ToolCallId {
  return typeof s === "string" && isBoundedLocalId(s, MAX_TOOL_CALL_ID_BYTES);
}

export function asApprovalId(s: string): ApprovalId {
  if (!isBoundedLocalId(s, MAX_APPROVAL_ID_BYTES)) throw new Error("not an ApprovalId");
  return s as ApprovalId;
}

export function isApprovalId(s: unknown): s is ApprovalId {
  return typeof s === "string" && isBoundedLocalId(s, MAX_APPROVAL_ID_BYTES);
}

export function asWriteId(s: string): WriteId {
  if (!isBoundedLocalId(s, MAX_WRITE_ID_BYTES)) throw new Error("not a WriteId");
  return s as WriteId;
}

export function isWriteId(s: unknown): s is WriteId {
  return typeof s === "string" && isBoundedLocalId(s, MAX_WRITE_ID_BYTES);
}

function isBoundedLocalId(value: string, maxBytes: number): boolean {
  return value.length <= maxBytes && LOCAL_ID_RE.test(value);
}

export function asIdempotencyKey(s: string): IdempotencyKey {
  if (!IDEMPOTENCY_KEY_RE.test(s)) {
    throw new Error("asIdempotencyKey: not a valid IdempotencyKey shape");
  }
  return s as IdempotencyKey;
}

export function isIdempotencyKey(value: unknown): value is IdempotencyKey {
  return typeof value === "string" && IDEMPOTENCY_KEY_RE.test(value);
}

export function asThreadId(s: string): ThreadId {
  if (!ULID_RE.test(s)) throw new Error("not a ThreadId ULID");
  return s as ThreadId;
}

export function isThreadId(value: unknown): value is ThreadId {
  return typeof value === "string" && ULID_RE.test(value);
}

export function asThreadSpecRevisionId(s: string): ThreadSpecRevisionId {
  if (!ULID_RE.test(s)) throw new Error("not a ThreadSpecRevisionId ULID");
  return s as ThreadSpecRevisionId;
}

export function isThreadSpecRevisionId(value: unknown): value is ThreadSpecRevisionId {
  return typeof value === "string" && ULID_RE.test(value);
}

export function asSignerIdentity(s: string): SignerIdentity {
  if (s.length === 0) throw new Error("not a SignerIdentity: must be non-empty");
  const budget = validateSignerIdentityBudget(s);
  if (!budget.ok) throw new Error(`not a SignerIdentity: ${budget.reason}`);
  return s as SignerIdentity;
}

export function isSignerIdentity(value: unknown): value is SignerIdentity {
  if (typeof value !== "string" || value.length === 0) return false;
  return validateSignerIdentityBudget(value).ok;
}
