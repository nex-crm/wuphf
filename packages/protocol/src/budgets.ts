import type { FrozenArgs } from "./frozen-args.ts";
import type { ApprovalClaims, ReceiptSnapshot } from "./receipt-types.ts";
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
 * Public canonicalJSON callers can hand over very wide object or array graphs
 * that are small in depth but expensive to descriptor-walk and canonicalize.
 * 100,000 JSON value nodes is generous for protocol payloads while bounding
 * adversarial fanout before the JCS serializer sees it.
 */
export const MAX_CANONICAL_JSON_NODES = 100_000;

/**
 * SanitizedString values are rendered for humans after normalization. A 1 MiB
 * UTF-8 cap covers long logs or summaries; 100x would let one string dominate
 * memory and UI rendering work.
 */
export const MAX_SANITIZED_STRING_BYTES = 1 * 1024 * 1024;

/**
 * SanitizedString JSON projections are descriptor-walked and copied before
 * rendering. 50,000 nodes leaves room for broad structured logs while bounding
 * flat object/array fanout before descriptor checks and normalization work.
 */
export const MAX_SANITIZED_JSON_NODES = 50_000;

/**
 * Thread titles are compact human labels. 512 bytes handles descriptive task
 * names while preventing one thread row from becoming a rendered log blob.
 */
export const MAX_THREAD_TITLE_BYTES = 512;

/**
 * Thread specs are structured JSON state, not attachment storage. 64 KiB keeps
 * canonicalization and audit replay bounded while leaving room for substantial
 * review instructions.
 */
export const MAX_THREAD_SPEC_CONTENT_BYTES = 64 * 1024;

/**
 * Thread external refs point to upstream issue/docs/work items. 32 refs covers
 * normal fan-in without making thread validation scale with arbitrary link
 * dumps.
 */
export const MAX_THREAD_EXTERNAL_REFS = 32;

/**
 * Individual external refs can carry URLs or provider IDs. 2 KiB covers long
 * URLs with query state while bounding per-ref normalization and JSON work.
 */
export const MAX_THREAD_EXTERNAL_REF_BYTES = 2 * 1024;

/**
 * A thread's receipt-derived task index should stay bounded inside the
 * protocol shape. Broker projections can page richer receipt lists elsewhere.
 */
export const MAX_THREAD_TASK_IDS = 1024;

/**
 * Signer identities cross audit, receipt, and thread boundaries as text. Keep
 * the protocol brand bounded so forged large identities fail before storage,
 * signing, or log fanout.
 */
export const MAX_SIGNER_IDENTITY_BYTES = 256;

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
 * Ed25519 signatures are 64 raw bytes (roughly 88 base64 chars). 4 KiB leaves
 * room for envelope/version metadata while failing attacker-sized strings
 * before regex scans or downstream signature verification.
 */
export const MAX_APPROVAL_SIGNATURE_BYTES = 4 * 1024;

/**
 * WebAuthn assertions are usually a few KiB. 16 KiB gives authenticators room
 * for extension output while keeping high-risk approval submissions bounded.
 */
export const MAX_WEBAUTHN_ASSERTION_BYTES = 16 * 1024;

/**
 * Human-readable agent slugs are ASCII-only. 128 bytes covers descriptive
 * slugs while preventing unbounded branded identifiers from entering receipts.
 */
export const MAX_AGENT_SLUG_BYTES = 128;

/**
 * Tool-call identifiers are local receipt IDs, not payload storage. 128 bytes
 * gives generous room for generated IDs while keeping brand construction
 * bounded.
 */
export const MAX_TOOL_CALL_ID_BYTES = 128;

/**
 * Approval identifiers are local receipt IDs, not payload storage. 128 bytes
 * gives generous room for generated IDs while keeping brand construction
 * bounded.
 */
export const MAX_APPROVAL_ID_BYTES = 128;

/**
 * External-write identifiers are local receipt IDs, not payload storage. 128
 * bytes gives generated IDs enough room while keeping brand construction
 * bounded.
 */
export const MAX_WRITE_ID_BYTES = 128;

/**
 * Shared cap for other receipt-local IDs that use LOCAL_ID_RE. Specific brand
 * caps may be lowered independently, but none may exceed this generic bound.
 */
export const MAX_LOCAL_ID_BYTES = 128;

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

  const serializationSafety = validateJsonStringifySafety(receipt, "$", new Set<object>());
  if (!serializationSafety.ok) return serializationSafety;

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
  const canonicalJson = stringProperty(frozen, "canonicalJson");
  if (canonicalJson === undefined) return { ok: true };
  return validateUtf8StringBudget(
    canonicalJson,
    MAX_FROZEN_ARGS_BYTES,
    "FrozenArgs canonicalJson bytes",
  );
}

export function validateCanonicalJsonNodeBudget(
  nodeCount: number,
  path: string,
): BudgetValidationResult {
  try {
    assertWithinBudget(nodeCount, MAX_CANONICAL_JSON_NODES, `canonicalJSON node count at ${path}`);
    return { ok: true };
  } catch (err) {
    return { ok: false, reason: err instanceof Error ? err.message : String(err) };
  }
}

export function validateSanitizedStringBudget(s: SanitizedString): BudgetValidationResult {
  const value = stringProperty(s, "value");
  if (value === undefined) return { ok: true };
  return validateUtf8StringBudget(value, MAX_SANITIZED_STRING_BYTES, "SanitizedString value bytes");
}

export function validateSanitizedJsonNodeBudget(
  nodes: number,
  path: string,
): BudgetValidationResult {
  try {
    assertWithinBudget(
      nodes,
      MAX_SANITIZED_JSON_NODES,
      `SanitizedString JSON node count at ${path}`,
    );
    return { ok: true };
  } catch (err) {
    return { ok: false, reason: err instanceof Error ? err.message : String(err) };
  }
}

export function validateThreadTitleBudget(title: string): BudgetValidationResult {
  return validateUtf8StringBudget(title, MAX_THREAD_TITLE_BYTES, "Thread.title bytes");
}

export function validateThreadSpecContentBudget(canonicalContent: string): BudgetValidationResult {
  return validateUtf8StringBudget(
    canonicalContent,
    MAX_THREAD_SPEC_CONTENT_BYTES,
    "ThreadSpecRevision.content bytes",
  );
}

export function validateThreadExternalRefBudget(ref: string): BudgetValidationResult {
  return validateUtf8StringBudget(
    ref,
    MAX_THREAD_EXTERNAL_REF_BYTES,
    "ThreadExternalRefs item bytes",
  );
}

export function validateSignerIdentityBudget(identity: string): BudgetValidationResult {
  return validateUtf8StringBudget(identity, MAX_SIGNER_IDENTITY_BYTES, "SignerIdentity bytes");
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
  const issuedAt = objectProperty(claims, "issuedAt");
  const expiresAt = objectProperty(claims, "expiresAt");
  if (!(issuedAt instanceof Date) || !(expiresAt instanceof Date)) return { ok: true };
  return validateApprovalTokenLifetimeValues(issuedAt, expiresAt);
}

function validateReceiptCollectionBudgets(receipt: ReceiptSnapshot): BudgetValidationResult {
  const checks: readonly [label: string, value: unknown, budget: number][] = [
    ["receipt toolCalls length", objectProperty(receipt, "toolCalls"), MAX_TOOL_CALLS_PER_RECEIPT],
    [
      "receipt filesChanged length",
      objectProperty(receipt, "filesChanged"),
      MAX_RECEIPT_FILES_CHANGED,
    ],
    ["receipt commits length", objectProperty(receipt, "commits"), MAX_RECEIPT_COMMITS],
    ["receipt writes length", objectProperty(receipt, "writes"), MAX_RECEIPT_WRITES],
    ["receipt approvals length", objectProperty(receipt, "approvals"), MAX_RECEIPT_APPROVALS],
    [
      "receipt sourceReads length",
      objectProperty(receipt, "sourceReads"),
      MAX_RECEIPT_SOURCE_READS,
    ],
    [
      "receipt notebookWrites length",
      objectProperty(receipt, "notebookWrites"),
      MAX_RECEIPT_NOTEBOOK_WRITES,
    ],
    ["receipt wikiWrites length", objectProperty(receipt, "wikiWrites"), MAX_RECEIPT_WIKI_WRITES],
  ];

  for (const [label, value, budget] of checks) {
    const length = arrayLength(value);
    if (length === undefined) continue;
    try {
      assertWithinBudget(length, budget, label);
    } catch (err) {
      return { ok: false, reason: err instanceof Error ? err.message : String(err) };
    }
  }

  return { ok: true };
}

function validateReceiptNestedBudgets(receipt: ReceiptSnapshot): BudgetValidationResult {
  const topLevelStrings: readonly [label: string, value: unknown][] = [
    ["receipt finalMessage", objectProperty(receipt, "finalMessage")],
    ["receipt error", objectProperty(receipt, "error")],
  ];
  for (const [label, value] of topLevelStrings) {
    const result = validateMaybeSanitizedStringBudget(value, label);
    if (!result.ok) return result;
  }

  const toolCalls = arrayOrEmpty(objectProperty(receipt, "toolCalls"));
  for (let i = 0; i < toolCalls.length; i++) {
    const toolCall = arrayElement(toolCalls, i);
    if (toolCall === undefined) continue;
    const inputs = validateMaybeFrozenArgsBudget(
      objectProperty(toolCall, "inputs"),
      `receipt toolCalls[${i}].inputs`,
    );
    if (!inputs.ok) return inputs;
    const output = validateMaybeSanitizedStringBudget(
      objectProperty(toolCall, "output"),
      `receipt toolCalls[${i}].output`,
    );
    if (!output.ok) return output;
    const toolCallError = objectProperty(toolCall, "error");
    if (toolCallError !== undefined) {
      const error = validateMaybeSanitizedStringBudget(
        toolCallError,
        `receipt toolCalls[${i}].error`,
      );
      if (!error.ok) return error;
    }
  }

  const approvals = arrayOrEmpty(objectProperty(receipt, "approvals"));
  for (let i = 0; i < approvals.length; i++) {
    const approval = arrayElement(approvals, i);
    if (approval === undefined) continue;
    const result = validateMaybeSignedApprovalTokenBudget(
      objectProperty(approval, "signedToken"),
      `receipt approvals[${i}].signedToken`,
    );
    if (!result.ok) return result;
  }

  const commits = arrayOrEmpty(objectProperty(receipt, "commits"));
  for (let i = 0; i < commits.length; i++) {
    const commit = arrayElement(commits, i);
    if (commit === undefined) continue;
    const message = validateMaybeSanitizedStringBudget(
      objectProperty(commit, "message"),
      `receipt commits[${i}].message`,
    );
    if (!message.ok) return message;
  }

  const writes = arrayOrEmpty(objectProperty(receipt, "writes"));
  for (let i = 0; i < writes.length; i++) {
    const write = arrayElement(writes, i);
    if (write === undefined) continue;
    const result = validateExternalWriteBudget(write, i);
    if (!result.ok) return result;
  }

  return { ok: true };
}

function validateExternalWriteBudget(write: unknown, index: number): BudgetValidationResult {
  const proposed = validateMaybeFrozenArgsBudget(
    objectProperty(write, "proposedDiff"),
    `receipt writes[${index}].proposedDiff`,
  );
  if (!proposed.ok) return proposed;
  const appliedDiff = objectProperty(write, "appliedDiff");
  if (appliedDiff !== null) {
    const applied = validateMaybeFrozenArgsBudget(
      appliedDiff,
      `receipt writes[${index}].appliedDiff`,
    );
    if (!applied.ok) return applied;
  }
  const postWriteVerify = objectProperty(write, "postWriteVerify");
  if (postWriteVerify !== null) {
    const verify = validateMaybeFrozenArgsBudget(
      postWriteVerify,
      `receipt writes[${index}].postWriteVerify`,
    );
    if (!verify.ok) return verify;
  }
  const approvalToken = objectProperty(write, "approvalToken");
  if (approvalToken !== null) {
    const token = validateMaybeSignedApprovalTokenBudget(
      approvalToken,
      `receipt writes[${index}].approvalToken`,
    );
    if (!token.ok) return token;
  }
  const failureMetadata = objectProperty(write, "failureMetadata");
  const terminalReason = objectProperty(failureMetadata, "terminalReason");
  if (terminalReason !== undefined) {
    const terminal = validateMaybeSanitizedStringBudget(
      terminalReason,
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
  const signature = stringProperty(value, "signature");
  if (signature !== undefined) {
    const signatureBudget = validateUtf8StringBudget(
      signature,
      MAX_APPROVAL_SIGNATURE_BYTES,
      "approvalToken.signature bytes",
    );
    if (!signatureBudget.ok) return prefixBudgetReason(label, signatureBudget);
  }

  const claims = objectProperty(value, "claims");
  if (claims === undefined) return { ok: true };

  const webauthnAssertion = stringProperty(claims, "webauthnAssertion");
  if (webauthnAssertion !== undefined) {
    const assertionBudget = validateUtf8StringBudget(
      webauthnAssertion,
      MAX_WEBAUTHN_ASSERTION_BYTES,
      "approvalToken.claims.webauthnAssertion bytes",
    );
    if (!assertionBudget.ok) return prefixBudgetReason(label, assertionBudget);
  }

  const issuedAt = objectProperty(claims, "issuedAt");
  const expiresAt = objectProperty(claims, "expiresAt");
  if (!(issuedAt instanceof Date) || !(expiresAt instanceof Date)) return { ok: true };
  const result = validateApprovalTokenLifetimeValues(issuedAt, expiresAt);
  return result.ok ? result : prefixBudgetReason(label, result);
}

export function validateApprovalTokenLifetimeValues(
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

function arrayOrEmpty(value: unknown): readonly unknown[] {
  return Array.isArray(value) ? value : [];
}

function objectProperty(value: unknown, key: string): unknown | undefined {
  if (typeof value !== "object" || value === null) return undefined;
  const descriptor = Object.getOwnPropertyDescriptor(value, key);
  return descriptor !== undefined && "value" in descriptor ? descriptor.value : undefined;
}

function arrayLength(value: unknown): number | undefined {
  const length = objectProperty(value, "length");
  return typeof length === "number" ? length : undefined;
}

function arrayElement(value: readonly unknown[], index: number): unknown | undefined {
  return objectProperty(value, String(index));
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
      if (descriptor === undefined) continue;
      if (isAccessorDescriptor(descriptor)) {
        return { ok: false, reason: `receipt serialized bytes: accessor property at ${key}` };
      }
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

function validateJsonStringifySafety(
  value: unknown,
  path: string,
  ancestors: Set<object>,
): BudgetValidationResult {
  if (value === null || typeof value !== "object") return { ok: true };
  if (ancestors.has(value)) {
    return { ok: false, reason: "receipt serialized bytes: receipt contains a cycle" };
  }

  const toJsonDescriptor = findToJsonDescriptor(value);
  if (toJsonDescriptor !== undefined) {
    if (isAccessorDescriptor(toJsonDescriptor)) {
      return { ok: false, reason: `receipt serialized bytes: accessor toJSON at ${path}` };
    }
    if (typeof toJsonDescriptor.value === "function") {
      if (value instanceof Date && toJsonDescriptor.value === Date.prototype.toJSON) {
        return { ok: true };
      }
      return { ok: false, reason: `receipt serialized bytes: custom toJSON at ${path}` };
    }
  }

  ancestors.add(value);
  try {
    for (const key of Reflect.ownKeys(value)) {
      const descriptor = Object.getOwnPropertyDescriptor(value, key);
      if (descriptor === undefined) continue;
      const childPath = `${path}.${typeof key === "symbol" ? key.toString() : key}`;
      if (isAccessorDescriptor(descriptor)) {
        return { ok: false, reason: `receipt serialized bytes: accessor property at ${childPath}` };
      }
      const result = validateJsonStringifySafety(descriptor.value, childPath, ancestors);
      if (!result.ok) return result;
    }
  } finally {
    ancestors.delete(value);
  }

  return { ok: true };
}

function findToJsonDescriptor(value: object): PropertyDescriptor | undefined {
  let current: object | null = value;
  while (current !== null) {
    const descriptor = Object.getOwnPropertyDescriptor(current, "toJSON");
    if (descriptor !== undefined) return descriptor;
    current = Object.getPrototypeOf(current);
  }
  return undefined;
}

function isAccessorDescriptor(descriptor: PropertyDescriptor): boolean {
  return "get" in descriptor || "set" in descriptor;
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
