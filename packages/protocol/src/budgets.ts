import type { FrozenArgs } from "./frozen-args.ts";
import type { ApprovalClaims, ExternalWrite, ReceiptSnapshot } from "./receipt-types.ts";
import type { SanitizedString } from "./sanitized-string.ts";

export type BudgetValidationResult = { ok: true } | { ok: false; reason: string };

/**
 * A single receipt larger than 10 MiB can force parsers, canonicalizers, and
 * validators to allocate attacker-sized buffers. Ten MiB is enough for thousands
 * of receipt entries and large human-readable summaries; 100x would permit
 * gigabyte-class receipts that can OOM verifier processes.
 */
export const MAX_RECEIPT_BYTES = 10 * 1024 * 1024;

/**
 * One receipt represents one task. 1,024 tool calls is already well beyond a
 * normal interactive task while keeping per-receipt validation bounded; 100x
 * would let one runaway loop pin the verifier on 100k nested blobs.
 */
export const MAX_TOOL_CALLS_PER_RECEIPT = 1024;

/**
 * FrozenArgs are canonical JSON blobs that get hashed and compared. A 1 MiB
 * per-blob cap allows substantial structured diffs; 100x would make a single
 * tool argument large enough to crash or stall canonicalization.
 */
export const MAX_FROZEN_ARGS_BYTES = 1 * 1024 * 1024;

/**
 * SanitizedString values are rendered for humans after normalization. A 1 MiB
 * UTF-8 cap covers long logs or summaries; 100x would let one string dominate
 * memory and UI rendering work.
 */
export const MAX_SANITIZED_STRING_BYTES = 1 * 1024 * 1024;

/**
 * Audit verification should proceed in bounded chunks. 10,000 records keeps a
 * batch large enough for efficient sequential I/O while preventing callers from
 * accidentally materializing 1M-event chains in one verifier step.
 */
export const MAX_AUDIT_CHAIN_BATCH_SIZE = 10_000;

/**
 * Audit event bodies are opaque broker-owned bytes that are base64-encoded
 * before hashing. A 1 MiB per-record cap keeps that expansion and JCS work
 * bounded while leaving room for large receipt snapshots or tool metadata.
 */
export const MAX_AUDIT_EVENT_BODY_BYTES = 1 * 1024 * 1024;

/**
 * Signed Merkle checkpoints carry a detached signature and PEM certificate
 * chain. 64 KiB is generous for current signatures and compact cert chains
 * while bounding regex scans and JSON decoding before deeper validation.
 */
export const MAX_MERKLE_ROOT_SIGNATURE_BYTES = 64 * 1024;
export const MAX_MERKLE_ROOT_CERT_CHAIN_BYTES = 64 * 1024;

/**
 * Approval tokens are cleanup-bound capabilities. Thirty minutes covers a
 * human review window; 100x would leave bearer material valid for days after
 * the task context has gone stale.
 */
export const MAX_APPROVAL_TOKEN_LIFETIME_MS = 30 * 60 * 1000;

/**
 * File-change lists can grow with generated or vendored trees. 10,000 entries
 * covers large refactors; 100x would let path lists alone consume verifier
 * memory before any receipt semantics are checked.
 */
export const MAX_RECEIPT_FILES_CHANGED = 10_000;

/**
 * A receipt may reference a stack of commits, but 1,024 commits is already far
 * past normal task scope. 100x would turn commit metadata into an unbounded log
 * stream inside a single receipt.
 */
export const MAX_RECEIPT_COMMITS = 1024;

/**
 * External writes are high-risk side effects. 256 writes leaves room for bulk
 * operations while keeping approval-token and diff validation bounded; 100x
 * would make one receipt a runaway write ledger.
 */
export const MAX_RECEIPT_WRITES = 256;

/**
 * Approval events are human decisions, not a message bus. 64 approvals covers
 * multi-reviewer workflows; 100x would let stale or duplicated decisions bloat
 * receipt verification.
 */
export const MAX_RECEIPT_APPROVALS = 64;

/**
 * Source-read citations can be numerous in research-heavy tasks. 10,000 entries
 * matches the file-list cap; 100x would let citation metadata crowd out the
 * receipt's actual execution record.
 */
export const MAX_RECEIPT_SOURCE_READS = 10_000;

/**
 * Notebook write references are durable memory updates. 10,000 entries is
 * generous for generated references; 100x would let a single receipt become an
 * unbounded memory-write queue.
 */
export const MAX_RECEIPT_NOTEBOOK_WRITES = 10_000;

/**
 * Wiki write references are durable knowledge-base updates. 10,000 entries is
 * enough for large import tasks; 100x would make receipt validation scale with
 * an accidental site-wide write fanout.
 */
export const MAX_RECEIPT_WIKI_WRITES = 10_000;

export function assertWithinBudget(value: number, budget: number, label: string): void {
  if (!Number.isFinite(value) || value < 0) {
    throw new Error(`${label} must be a non-negative finite number`);
  }
  if (!Number.isFinite(budget) || budget < 0) {
    throw new Error(`${label} budget must be a non-negative finite number`);
  }
  if (value > budget) {
    throw new Error(`${label} exceeds budget: ${value} > ${budget}`);
  }
}

/**
 * Budget validation assumes a plain-data receipt or JSON.parse output. Hostile
 * wire bytes must enter through receiptFromJson, which parses before this runs;
 * direct callers should not pass objects with accessors or custom toJSON hooks.
 */
export function validateReceiptBudget(receipt: ReceiptSnapshot): BudgetValidationResult {
  const countBudget = validateReceiptCollectionBudgets(receipt);
  if (!countBudget.ok) return countBudget;

  const nestedBudget = validateReceiptNestedBudgets(receipt);
  if (!nestedBudget.ok) return nestedBudget;

  const stringFloor = validateSerializableStringFloor(receipt, MAX_RECEIPT_BYTES);
  if (!stringFloor.ok) return stringFloor;

  try {
    const serialized = JSON.stringify(receipt);
    if (serialized === undefined) {
      return { ok: false, reason: "receipt serialized bytes: receipt is not JSON-serializable" };
    }
    assertWithinBudget(
      utf8ByteLengthUpTo(serialized, MAX_RECEIPT_BYTES),
      MAX_RECEIPT_BYTES,
      "receipt serialized bytes",
    );
  } catch (err) {
    if (err instanceof Error && /exceeds budget/.test(err.message)) {
      return { ok: false, reason: err.message };
    }
    return {
      ok: false,
      reason: `receipt serialized bytes: ${err instanceof Error ? err.message : String(err)}`,
    };
  }

  return { ok: true };
}

export function validateFrozenArgsBudget(frozen: FrozenArgs): BudgetValidationResult {
  return validateUtf8StringBudget(
    frozen.canonicalJson,
    MAX_FROZEN_ARGS_BYTES,
    "FrozenArgs canonicalJson bytes",
  );
}

export function validateSanitizedStringBudget(s: SanitizedString): BudgetValidationResult {
  return validateUtf8StringBudget(
    s.value,
    MAX_SANITIZED_STRING_BYTES,
    "SanitizedString value bytes",
  );
}

export function validateAuditEventBodyBudget(body: Uint8Array): BudgetValidationResult {
  try {
    assertWithinBudget(body.byteLength, MAX_AUDIT_EVENT_BODY_BYTES, "MAX_AUDIT_EVENT_BODY_BYTES");
    return { ok: true };
  } catch (err) {
    return { ok: false, reason: err instanceof Error ? err.message : String(err) };
  }
}

export function validateMerkleRootSignatureBudget(signature: string): BudgetValidationResult {
  return validateUtf8StringBudget(
    signature,
    MAX_MERKLE_ROOT_SIGNATURE_BYTES,
    "MerkleRootRecord.signature bytes",
  );
}

export function validateMerkleRootCertChainBudget(certChainPem: string): BudgetValidationResult {
  return validateUtf8StringBudget(
    certChainPem,
    MAX_MERKLE_ROOT_CERT_CHAIN_BYTES,
    "MerkleRootRecord.certChainPem bytes",
  );
}

export function validateApprovalTokenLifetime(claims: ApprovalClaims): BudgetValidationResult {
  return validateApprovalTokenLifetimeValues(claims.issuedAt, claims.expiresAt);
}

function validateReceiptCollectionBudgets(receipt: ReceiptSnapshot): BudgetValidationResult {
  const checks: readonly [label: string, value: unknown, budget: number][] = [
    ["receipt toolCalls length", receipt.toolCalls, MAX_TOOL_CALLS_PER_RECEIPT],
    ["receipt filesChanged length", receipt.filesChanged, MAX_RECEIPT_FILES_CHANGED],
    ["receipt commits length", receipt.commits, MAX_RECEIPT_COMMITS],
    ["receipt writes length", receipt.writes, MAX_RECEIPT_WRITES],
    ["receipt approvals length", receipt.approvals, MAX_RECEIPT_APPROVALS],
    ["receipt sourceReads length", receipt.sourceReads, MAX_RECEIPT_SOURCE_READS],
    ["receipt notebookWrites length", receipt.notebookWrites, MAX_RECEIPT_NOTEBOOK_WRITES],
    ["receipt wikiWrites length", receipt.wikiWrites, MAX_RECEIPT_WIKI_WRITES],
  ];

  for (const [label, value, budget] of checks) {
    if (!Array.isArray(value)) continue;
    try {
      assertWithinBudget(value.length, budget, label);
    } catch (err) {
      return { ok: false, reason: err instanceof Error ? err.message : String(err) };
    }
  }

  return { ok: true };
}

function validateReceiptNestedBudgets(receipt: ReceiptSnapshot): BudgetValidationResult {
  const topLevelStrings: readonly [label: string, value: SanitizedString | undefined][] = [
    ["receipt finalMessage", receipt.finalMessage],
    ["receipt error", receipt.error],
  ];
  for (const [label, value] of topLevelStrings) {
    const result = validateMaybeSanitizedStringBudget(value, label);
    if (!result.ok) return result;
  }

  const toolCalls = arrayOrEmpty(receipt.toolCalls);
  for (let i = 0; i < toolCalls.length; i++) {
    const toolCall = toolCalls[i];
    if (toolCall === undefined) continue;
    const inputs = validateMaybeFrozenArgsBudget(toolCall.inputs, `receipt toolCalls[${i}].inputs`);
    if (!inputs.ok) return inputs;
    const output = validateMaybeSanitizedStringBudget(
      toolCall.output,
      `receipt toolCalls[${i}].output`,
    );
    if (!output.ok) return output;
    if (toolCall.error !== undefined) {
      const error = validateMaybeSanitizedStringBudget(
        toolCall.error,
        `receipt toolCalls[${i}].error`,
      );
      if (!error.ok) return error;
    }
  }

  const approvals = arrayOrEmpty(receipt.approvals);
  for (let i = 0; i < approvals.length; i++) {
    const approval = approvals[i];
    if (approval === undefined) continue;
    const result = validateMaybeSignedApprovalTokenBudget(
      approval.signedToken,
      `receipt approvals[${i}].signedToken`,
    );
    if (!result.ok) return result;
  }

  const commits = arrayOrEmpty(receipt.commits);
  for (let i = 0; i < commits.length; i++) {
    const commit = commits[i];
    if (commit === undefined) continue;
    const message = validateMaybeSanitizedStringBudget(
      commit.message,
      `receipt commits[${i}].message`,
    );
    if (!message.ok) return message;
  }

  const writes = arrayOrEmpty(receipt.writes);
  for (let i = 0; i < writes.length; i++) {
    const write = writes[i];
    if (write === undefined) continue;
    const result = validateExternalWriteBudget(write, i);
    if (!result.ok) return result;
  }

  return { ok: true };
}

function validateExternalWriteBudget(write: ExternalWrite, index: number): BudgetValidationResult {
  const proposed = validateMaybeFrozenArgsBudget(
    write.proposedDiff,
    `receipt writes[${index}].proposedDiff`,
  );
  if (!proposed.ok) return proposed;
  if (write.appliedDiff !== null) {
    const applied = validateMaybeFrozenArgsBudget(
      write.appliedDiff,
      `receipt writes[${index}].appliedDiff`,
    );
    if (!applied.ok) return applied;
  }
  if (write.postWriteVerify !== null) {
    const verify = validateMaybeFrozenArgsBudget(
      write.postWriteVerify,
      `receipt writes[${index}].postWriteVerify`,
    );
    if (!verify.ok) return verify;
  }
  if (write.approvalToken !== null) {
    const token = validateMaybeSignedApprovalTokenBudget(
      write.approvalToken,
      `receipt writes[${index}].approvalToken`,
    );
    if (!token.ok) return token;
  }
  if (write.failureMetadata?.terminalReason !== undefined) {
    const terminal = validateMaybeSanitizedStringBudget(
      write.failureMetadata.terminalReason,
      `receipt writes[${index}].failureMetadata.terminalReason`,
    );
    if (!terminal.ok) return terminal;
  }
  return { ok: true };
}

function validateMaybeFrozenArgsBudget(value: unknown, label: string): BudgetValidationResult {
  const canonicalJson = stringProperty(value, "canonicalJson");
  if (canonicalJson === undefined) return { ok: true };
  const result = validateUtf8StringBudget(
    canonicalJson,
    MAX_FROZEN_ARGS_BYTES,
    "FrozenArgs canonicalJson bytes",
  );
  return result.ok ? result : prefixBudgetReason(label, result);
}

function validateMaybeSanitizedStringBudget(value: unknown, label: string): BudgetValidationResult {
  const sanitizedValue = stringProperty(value, "value");
  if (sanitizedValue === undefined) return { ok: true };
  const result = validateUtf8StringBudget(
    sanitizedValue,
    MAX_SANITIZED_STRING_BYTES,
    "SanitizedString value bytes",
  );
  return result.ok ? result : prefixBudgetReason(label, result);
}

function validateMaybeSignedApprovalTokenBudget(
  value: unknown,
  label: string,
): BudgetValidationResult {
  const claims = objectProperty(value, "claims");
  if (claims === undefined) return { ok: true };
  const issuedAt = objectProperty(claims, "issuedAt");
  const expiresAt = objectProperty(claims, "expiresAt");
  if (!(issuedAt instanceof Date) || !(expiresAt instanceof Date)) return { ok: true };
  const result = validateApprovalTokenLifetimeValues(issuedAt, expiresAt);
  return result.ok ? result : prefixBudgetReason(label, result);
}

function validateApprovalTokenLifetimeValues(
  issuedAt: Date,
  expiresAt: Date,
): BudgetValidationResult {
  const lifetimeMs = expiresAt.getTime() - issuedAt.getTime();
  if (!Number.isFinite(lifetimeMs)) {
    return { ok: false, reason: "approval token lifetime must be finite" };
  }
  // Lower-bound enforcement (strict `expiresAt > issuedAt`) lives in the
  // per-field validators: `validateApprovalClaims` in receipt-validator.ts
  // and `validateApprovalClaimsShape` in ipc.ts. Both surface path-anchored
  // errors at `/approvals/N/signedToken/claims/expiresAt` (or the IPC
  // equivalent). This budget validator owns only the upper bound — the
  // 30-minute cap. Duplicating the lower bound here would short-circuit the
  // per-field error path with a less useful top-level message at path "".
  if (lifetimeMs <= 0) {
    return { ok: true };
  }
  try {
    assertWithinBudget(lifetimeMs, MAX_APPROVAL_TOKEN_LIFETIME_MS, "approval token lifetime ms");
    return { ok: true };
  } catch (err) {
    return { ok: false, reason: err instanceof Error ? err.message : String(err) };
  }
}

function arrayOrEmpty<T>(value: readonly T[]): readonly T[] {
  return Array.isArray(value) ? value : [];
}

function objectProperty(value: unknown, key: string): unknown | undefined {
  if (typeof value !== "object" || value === null) return undefined;
  const descriptor = Object.getOwnPropertyDescriptor(value, key);
  return descriptor !== undefined && "value" in descriptor ? descriptor.value : undefined;
}

function stringProperty(value: unknown, key: string): string | undefined {
  const property = objectProperty(value, key);
  return typeof property === "string" ? property : undefined;
}

function validateUtf8StringBudget(
  value: string,
  budget: number,
  label: string,
): BudgetValidationResult {
  try {
    assertWithinBudget(utf8ByteLengthUpTo(value, budget), budget, label);
    return { ok: true };
  } catch (err) {
    return { ok: false, reason: err instanceof Error ? err.message : String(err) };
  }
}

function prefixBudgetReason(
  label: string,
  result: { ok: false; reason: string },
): BudgetValidationResult {
  return { ok: false, reason: `${label}: ${result.reason}` };
}

function validateSerializableStringFloor(value: unknown, budget: number): BudgetValidationResult {
  const result = sumSerializableStringBytes(value, budget, new Set<object>());
  if (!result.ok) return result;
  try {
    assertWithinBudget(result.bytes, budget, "receipt string payload bytes");
    return { ok: true };
  } catch (err) {
    return { ok: false, reason: err instanceof Error ? err.message : String(err) };
  }
}

type StringByteSumResult = { ok: true; bytes: number } | { ok: false; reason: string };

function sumSerializableStringBytes(
  value: unknown,
  budget: number,
  ancestors: Set<object>,
): StringByteSumResult {
  if (typeof value === "string") {
    return { ok: true, bytes: utf8ByteLengthUpTo(value, budget) };
  }
  if (value === null || typeof value !== "object") {
    return { ok: true, bytes: 0 };
  }
  if (ancestors.has(value)) {
    return { ok: false, reason: "receipt serialized bytes: receipt contains a cycle" };
  }

  ancestors.add(value);
  let bytes = 0;
  try {
    for (const key of Reflect.ownKeys(value)) {
      if (typeof key === "symbol") continue;
      const descriptor = Object.getOwnPropertyDescriptor(value, key);
      if (descriptor === undefined || !("value" in descriptor)) continue;
      const child = sumSerializableStringBytes(descriptor.value, budget - bytes, ancestors);
      if (!child.ok) return child;
      bytes += child.bytes;
      if (bytes > budget) {
        return {
          ok: false,
          reason: `receipt string payload bytes exceeds budget: ${bytes} > ${budget}`,
        };
      }
    }
  } finally {
    ancestors.delete(value);
  }

  return { ok: true, bytes };
}

function utf8ByteLengthUpTo(value: string, budget: number): number {
  if (value.length > budget) return budget + 1;

  let bytes = 0;
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code <= 0x7f) {
      bytes += 1;
    } else if (code <= 0x7ff) {
      bytes += 2;
    } else if (code >= 0xd800 && code <= 0xdbff && i + 1 < value.length) {
      const next = value.charCodeAt(i + 1);
      if (next >= 0xdc00 && next <= 0xdfff) {
        bytes += 4;
        i += 1;
      } else {
        bytes += 3;
      }
    } else {
      bytes += 3;
    }

    if (bytes > budget) return budget + 1;
  }

  return bytes;
}
