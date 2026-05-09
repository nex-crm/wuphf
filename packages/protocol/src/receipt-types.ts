// Receipt schema types + brand constructors. Pure data — no validators, no
// codec. The two heavy modules (receipt-validator, receipt-codec) import this
// module so that the file-size of any single file stays well below the cap.

import type { Brand } from "./brand.ts";
import type { FrozenArgs } from "./frozen-args.ts";
import type { SanitizedString } from "./sanitized-string.ts";
import type { Sha256Hex } from "./sha256.ts";

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

const PROVIDER_KIND_VALUES = ["anthropic", "openai", "openai-compat", "openclaw"] as const;
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
  if (!LOCAL_ID_RE.test(s)) throw new Error("not a ToolCallId");
  return s as ToolCallId;
}

export function isToolCallId(s: unknown): s is ToolCallId {
  return typeof s === "string" && LOCAL_ID_RE.test(s);
}

export function asApprovalId(s: string): ApprovalId {
  if (!LOCAL_ID_RE.test(s)) throw new Error("not an ApprovalId");
  return s as ApprovalId;
}

export function isApprovalId(s: unknown): s is ApprovalId {
  return typeof s === "string" && LOCAL_ID_RE.test(s);
}
