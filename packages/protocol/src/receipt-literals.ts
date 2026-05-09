import type {
  ApprovalClaims,
  ApprovalEvent,
  BrokerTokenVerdict,
  FileChange,
  MemoryWriteRef,
  ReceiptStatus,
  RiskClass,
  SignedApprovalToken,
  ToolCall,
  TriggerKind,
  WriteResult,
} from "./receipt-types.ts";

type ApprovalRole = ApprovalClaims["role"] | ApprovalEvent["role"];

// Typo-protection example:
//
//   ["ok", "deferred"] as const satisfies readonly ReceiptStatus[];
//
// TypeScript rejects "deferred" because it is not assignable to ReceiptStatus.
export const RECEIPT_STATUS_VALUES = [
  "ok",
  "error",
  "stalled",
  "approval_pending",
  "rejected",
] as const satisfies readonly ReceiptStatus[];

export const RISK_CLASS_VALUES = [
  "low",
  "medium",
  "high",
  "critical",
] as const satisfies readonly RiskClass[];

export const WRITE_RESULT_VALUES = [
  "applied",
  "rejected",
  "partial",
  "rollback",
] as const satisfies readonly WriteResult[];

export const TRIGGER_KIND_VALUES = [
  "human_message",
  "scheduler",
  "mention",
  "webhook",
  "agent_message",
  "system",
] as const satisfies readonly TriggerKind[];

export const APPROVAL_ROLE_VALUES = [
  "viewer",
  "approver",
  "host",
] as const satisfies readonly ApprovalRole[];

export const APPROVAL_DECISION_VALUES = [
  "approve",
  "reject",
  "abstain",
] as const satisfies readonly ApprovalEvent["decision"][];

export const TOOL_CALL_STATUS_VALUES = [
  "ok",
  "error",
] as const satisfies readonly ToolCall["status"][];

export const FILE_CHANGE_MODE_VALUES = [
  "created",
  "modified",
  "deleted",
] as const satisfies readonly FileChange["mode"][];

export const MEMORY_STORE_VALUES = [
  "notebook",
  "wiki",
] as const satisfies readonly MemoryWriteRef["store"][];

export const APPROVAL_TOKEN_ALGORITHM_VALUES = [
  "ed25519",
] as const satisfies readonly SignedApprovalToken["algorithm"][];

export const BROKER_TOKEN_VERDICT_STATUS_VALUES = [
  "valid",
  "expired",
  "tampered",
  "wrong_signer",
  "wrong_write",
] as const satisfies readonly BrokerTokenVerdict["status"][];

export const BASE64_RE = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;
