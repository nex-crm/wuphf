// Hash-chained audit log event types and the canonical record serializer.
//
// Storage shape (audit.cborl, audit.merkle, signing keys, fsync semantics) is
// defined in the RFC and lives in the broker package — this module is types
// plus the deterministic serializer/verifier so writer and verifier cannot
// drift.
//
// The chain semantics:
//   GENESIS_PREV_HASH = sha256("wuphf:audit:genesis:v1")  (domain-separated;
//                       the RFC sketch's literal "genesis" is updated to match)
//   eventHash         = sha256(asciiHex(prevHash) || serialize(record))
//   prevHash[n+1]     = eventHash[n]
//
// `prevHash` is mixed in as its 64-byte ASCII-hex form rather than 32 raw
// bytes. This keeps the chain trivially readable in JSON/CBOR debug dumps but
// is a cross-language footgun for any non-TS verifier — so the wire decision
// is locked here, exposed via golden vectors in testdata/audit-event-vectors.json.

import { createHash } from "node:crypto";
import type { Brand } from "./brand.ts";
import { MAX_AUDIT_CHAIN_BATCH_SIZE } from "./budgets.ts";
import { canonicalJSON } from "./canonical-json.ts";
import { type EventLsn, GENESIS_LSN, isEqualLsn, nextLsn, parseLsn } from "./event-lsn.ts";
import type { ReceiptId } from "./receipt.ts";
import { assertKnownKeys, hasOwn, pointer, requireRecord } from "./receipt-utils.ts";
import { asSha256Hex, type Sha256Hex, sha256Hex } from "./sha256.ts";

export type MerkleRootHex = Brand<string, "MerkleRootHex">;

const MERKLE_ROOT_HEX_RE = /^[0-9a-f]{64}$/;
const BASE64_RE = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;
const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;

export const AUDIT_EVENT_KIND_VALUES = [
  "receipt_created",
  "receipt_updated",
  "receipt_finalized",
  "approval_requested",
  "approval_decision",
  "cost_event",
  "tool_call_started",
  "tool_call_completed",
  "external_write_proposed",
  "external_write_applied",
  "external_write_failed",
  "boot_marker",
  "merkle_root",
] as const;

export type AuditEventKind = (typeof AUDIT_EVENT_KIND_VALUES)[number];

export interface AuditEventPayloadKindMetadata {
  readonly description: string;
  readonly bodySchemaRef: string;
}

export const PAYLOAD_KIND_METADATA = {
  receipt_created: {
    description: "Receipt snapshot was first persisted.",
    bodySchemaRef: "wuphf.audit.payload.receipt_created.v1",
  },
  receipt_updated: {
    description: "Receipt snapshot changed before finalization.",
    bodySchemaRef: "wuphf.audit.payload.receipt_updated.v1",
  },
  receipt_finalized: {
    description: "Receipt reached a terminal status.",
    bodySchemaRef: "wuphf.audit.payload.receipt_finalized.v1",
  },
  approval_requested: {
    description: "A write approval request was presented.",
    bodySchemaRef: "wuphf.audit.payload.approval_requested.v1",
  },
  approval_decision: {
    description: "A reviewer accepted, rejected, or abstained on an approval.",
    bodySchemaRef: "wuphf.audit.payload.approval_decision.v1",
  },
  cost_event: {
    description: "Token or cost accounting changed.",
    bodySchemaRef: "wuphf.audit.payload.cost_event.v1",
  },
  tool_call_started: {
    description: "A tool invocation began.",
    bodySchemaRef: "wuphf.audit.payload.tool_call_started.v1",
  },
  tool_call_completed: {
    description: "A tool invocation completed.",
    bodySchemaRef: "wuphf.audit.payload.tool_call_completed.v1",
  },
  external_write_proposed: {
    description: "An external write diff was proposed.",
    bodySchemaRef: "wuphf.audit.payload.external_write_proposed.v1",
  },
  external_write_applied: {
    description: "An external write was applied and verified.",
    bodySchemaRef: "wuphf.audit.payload.external_write_applied.v1",
  },
  external_write_failed: {
    description: "An external write failed, partially applied, or rolled back.",
    bodySchemaRef: "wuphf.audit.payload.external_write_failed.v1",
  },
  boot_marker: {
    description: "The broker appended a startup marker.",
    bodySchemaRef: "wuphf.audit.payload.boot_marker.v1",
  },
  merkle_root: {
    description: "A signed Merkle root checkpoint was emitted.",
    bodySchemaRef: "wuphf.audit.payload.merkle_root.v1",
  },
} as const satisfies Record<AuditEventKind, AuditEventPayloadKindMetadata>;

export interface AuditEventPayload {
  readonly kind: AuditEventKind;
  readonly receiptId?: ReceiptId | undefined;
  // Opaque kind-specific bytes. The broker owns the CBOR body schema named by
  // PAYLOAD_KIND_METADATA[kind].bodySchemaRef; protocol consumers hash and
  // verify these bytes without interpreting them here.
  readonly body: Uint8Array;
}

export interface AuditEventRecord {
  readonly seqNo: EventLsn;
  readonly timestamp: Date;
  readonly prevHash: Sha256Hex;
  readonly eventHash: Sha256Hex;
  readonly payload: AuditEventPayload;
}

export interface MerkleRootRecord {
  readonly seqNo: EventLsn;
  readonly rootHash: MerkleRootHex;
  readonly signedAt: Date;
  readonly ephemeralKeyId: string;
  readonly signature: string; // base64
  readonly certChainPem: string;
}

export type MerkleRootRecordValidationError = { path: string; message: string };
export type MerkleRootRecordValidationResult =
  | { ok: true }
  | { ok: false; errors: MerkleRootRecordValidationError[] };

const MERKLE_ROOT_RECORD_KEYS_TUPLE = [
  "seqNo",
  "rootHash",
  "signedAt",
  "ephemeralKeyId",
  "signature",
  "certChainPem",
] as const satisfies readonly (keyof MerkleRootRecord)[];
export const MERKLE_ROOT_RECORD_KEYS: ReadonlySet<string> = new Set<string>(
  MERKLE_ROOT_RECORD_KEYS_TUPLE,
);

export const GENESIS_PREV_HASH = sha256Hex("wuphf:audit:genesis:v1");

export function asMerkleRootHex(s: string): MerkleRootHex {
  if (!MERKLE_ROOT_HEX_RE.test(s)) {
    throw new Error("asMerkleRootHex: not a sha256 hex digest");
  }
  return s as MerkleRootHex;
}

export function isMerkleRootHex(value: unknown): value is MerkleRootHex {
  return typeof value === "string" && MERKLE_ROOT_HEX_RE.test(value);
}

export function validateMerkleRootRecord(input: unknown): MerkleRootRecordValidationResult {
  try {
    const errors: MerkleRootRecordValidationError[] = [];
    validateMerkleRootRecordValue(input, "", errors);
    return errors.length === 0 ? { ok: true } : { ok: false, errors };
  } catch (err) {
    return {
      ok: false,
      errors: [
        {
          path: "",
          message: err instanceof Error ? err.message : "merkle root record validation failed",
        },
      ],
    };
  }
}

export function merkleRootRecordToJsonValue(record: MerkleRootRecord): Record<string, unknown> {
  const validation = validateMerkleRootRecord(record);
  if (!validation.ok) {
    throw new Error(formatMerkleRootRecordValidationErrors(validation.errors));
  }
  return {
    seqNo: record.seqNo as string,
    rootHash: record.rootHash,
    signedAt: record.signedAt.toISOString(),
    ephemeralKeyId: record.ephemeralKeyId,
    signature: record.signature,
    certChainPem: record.certChainPem,
  };
}

export function merkleRootRecordFromJson(value: unknown): MerkleRootRecord {
  const record = requireRecord(value, "");
  assertKnownKeys(record, "", MERKLE_ROOT_RECORD_KEYS);
  const decoded: MerkleRootRecord = {
    seqNo: eventLsnFromJson(requiredStringFromJson(record, "seqNo", ""), pointer("", "seqNo")),
    rootHash: asMerkleRootHexAt(
      requiredStringFromJson(record, "rootHash", ""),
      pointer("", "rootHash"),
    ),
    signedAt: requiredDateFromJson(record, "signedAt", ""),
    ephemeralKeyId: requiredNonEmptyStringFromJson(record, "ephemeralKeyId", ""),
    signature: requiredBase64StringFromJson(record, "signature", ""),
    certChainPem: requiredNonEmptyStringFromJson(record, "certChainPem", ""),
  };
  const validation = validateMerkleRootRecord(decoded);
  if (!validation.ok) {
    throw new Error(formatMerkleRootRecordValidationErrors(validation.errors));
  }
  return decoded;
}

/**
 * Canonical byte serialization of an audit-event record (excluding eventHash)
 * for the purposes of computing eventHash. The broker MUST use this function
 * to write records and the verifier MUST use it to recompute hashes — that's
 * the whole point of the protocol package.
 *
 * Body bytes are base64-encoded inside a JCS object so the canonical JSON
 * stays printable and stable across encodings.
 */
export function serializeAuditEventRecordForHash(record: AuditEventRecord): Uint8Array {
  const payload: { kind: AuditEventKind; receiptId: string | null; bodyB64: string } = {
    kind: record.payload.kind,
    receiptId: record.payload.receiptId ?? null,
    bodyB64: bytesToBase64(record.payload.body),
  };
  const projection = {
    // EventLsn is an opaque branded string ("v1:<n>"). It serializes into the
    // canonical projection as a JSON string, locked in here as part of the
    // cross-language wire contract — see golden vector tests.
    seqNo: record.seqNo as string,
    timestamp: record.timestamp.toISOString(),
    prevHash: record.prevHash,
    payload,
  };
  return new TextEncoder().encode(canonicalJSON(projection));
}

/**
 * Compute eventHash given prevHash and a canonical serialization of the
 * record-without-eventHash. The serializer defaults to the canonical
 * `serializeAuditEventRecordForHash` so callers cannot accidentally use a
 * different projection than the verifier.
 */
export function computeEventHash(prevHash: Sha256Hex, recordBytes: Uint8Array): Sha256Hex {
  const hash = createHash("sha256");
  hash.update(prevHash, "ascii");
  hash.update(recordBytes);
  return asSha256Hex(hash.digest("hex"));
}

/**
 * Convenience: compute eventHash directly from a record using the canonical
 * serializer.
 */
export function computeAuditEventHash(record: AuditEventRecord): Sha256Hex {
  return computeEventHash(record.prevHash, serializeAuditEventRecordForHash(record));
}

export type ChainFailureCode =
  | "batch_too_large"
  | "missing_record"
  | "seq_gap"
  | "prev_hash_mismatch"
  | "event_hash_mismatch"
  | "serialization_threw"
  | "lsn_threw";

export type ChainVerificationResult =
  | { ok: true; empty: true }
  | { ok: true; empty: false; lastEventHash: Sha256Hex; lastSeqNo: EventLsn }
  | { ok: false; brokenAtSeqNo: EventLsn; code: ChainFailureCode; reason: string };

export interface ChainVerifierState {
  readonly expectedPrev: Sha256Hex;
  readonly expectedSeq: EventLsn;
  readonly lastSeen: EventLsn;
  readonly recordsVerified: number;
}

export const INITIAL_VERIFIER_STATE: ChainVerifierState = {
  expectedPrev: GENESIS_PREV_HASH,
  expectedSeq: GENESIS_LSN,
  lastSeen: GENESIS_LSN,
  recordsVerified: 0,
};

export type IncrementalVerifyResult =
  | { ok: true; state: ChainVerifierState }
  | { ok: false; brokenAtSeqNo: EventLsn; code: ChainFailureCode; reason: string };

/**
 * Verify a sequence of records forms a valid hash chain rooted at
 * GENESIS_PREV_HASH. The serializer defaults to the canonical
 * `serializeAuditEventRecordForHash` so writer and verifier cannot drift; a
 * caller that needs a custom projection (tests, migrations) can override it.
 */
export function verifyChain(
  records: readonly AuditEventRecord[],
  serialize: (record: AuditEventRecord) => Uint8Array = serializeAuditEventRecordForHash,
): ChainVerificationResult {
  let state = INITIAL_VERIFIER_STATE;
  for (let offset = 0; offset < records.length; offset += MAX_AUDIT_CHAIN_BATCH_SIZE) {
    const result = verifyChainIncremental(
      state,
      records.slice(offset, offset + MAX_AUDIT_CHAIN_BATCH_SIZE),
      serialize,
    );
    if (!result.ok) return result;
    state = result.state;
  }

  if (state.recordsVerified === 0) {
    return { ok: true, empty: true };
  }

  return {
    ok: true,
    empty: false,
    lastEventHash: state.expectedPrev,
    lastSeqNo: state.lastSeen,
  };
}

export function verifyChainIncremental(
  state: ChainVerifierState,
  batch: readonly AuditEventRecord[],
  serialize: (record: AuditEventRecord) => Uint8Array = serializeAuditEventRecordForHash,
): IncrementalVerifyResult {
  if (batch.length > MAX_AUDIT_CHAIN_BATCH_SIZE) {
    return {
      ok: false,
      brokenAtSeqNo: state.expectedSeq,
      code: "batch_too_large",
      reason: `batch too large: ${batch.length} > ${MAX_AUDIT_CHAIN_BATCH_SIZE}`,
    };
  }

  if (batch.length === 0) {
    return { ok: true, state };
  }

  let expectedPrev: Sha256Hex = state.expectedPrev;
  let expectedSeq: EventLsn = state.expectedSeq;
  let lastSeen: EventLsn = state.lastSeen;
  let recordsVerified = state.recordsVerified;

  for (let i = 0; i < batch.length; i++) {
    const r = batch[i];
    if (r === undefined) {
      return {
        ok: false,
        brokenAtSeqNo: expectedSeq,
        code: "missing_record",
        reason: "missing record",
      };
    }

    try {
      if (!isEqualLsn(r.seqNo, expectedSeq)) {
        return {
          ok: false,
          brokenAtSeqNo: r.seqNo,
          code: "seq_gap",
          reason: `seq_no gap: expected ${expectedSeq as string}, got ${r.seqNo as string}`,
        };
      }
      if (r.prevHash !== expectedPrev) {
        return {
          ok: false,
          brokenAtSeqNo: r.seqNo,
          code: "prev_hash_mismatch",
          reason: `prev_hash mismatch at seq ${r.seqNo as string}`,
        };
      }

      let recordBytes: Uint8Array;
      try {
        recordBytes = serialize(r);
      } catch (cause) {
        return {
          ok: false,
          brokenAtSeqNo: r.seqNo,
          code: "serialization_threw",
          reason: `serialization threw at seq ${r.seqNo as string}: ${errorMessage(cause)}`,
        };
      }

      const recomputed = computeEventHash(r.prevHash, recordBytes);
      if (recomputed !== r.eventHash) {
        return {
          ok: false,
          brokenAtSeqNo: r.seqNo,
          code: "event_hash_mismatch",
          reason: `event_hash mismatch at seq ${r.seqNo as string}`,
        };
      }
      expectedPrev = r.eventHash;
      try {
        expectedSeq = nextLsn(expectedSeq);
      } catch (cause) {
        return {
          ok: false,
          brokenAtSeqNo: r.seqNo,
          code: "lsn_threw",
          reason: `lsn advance threw after seq ${r.seqNo as string}: ${errorMessage(cause)}`,
        };
      }
      lastSeen = r.seqNo;
      recordsVerified += 1;
    } catch (cause) {
      const brokenAtSeqNo = safeRecordSeqNo(r, expectedSeq);
      return {
        ok: false,
        brokenAtSeqNo,
        code: "serialization_threw",
        reason: `record verification threw at seq ${brokenAtSeqNo as string}: ${errorMessage(cause)}`,
      };
    }
  }

  return {
    ok: true,
    state: {
      expectedPrev,
      expectedSeq,
      lastSeen,
      recordsVerified,
    },
  };
}

function bytesToBase64(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64");
}

function validateMerkleRootRecordValue(
  value: unknown,
  path: string,
  errors: MerkleRootRecordValidationError[],
): void {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    addMerkleRootRecordError(errors, path, "must be an object");
    return;
  }
  validateMerkleRootRecordKnownKeys(value as Readonly<Record<string, unknown>>, path, errors);
  validateRequiredMerkleRootRecordField(
    value as Readonly<Record<string, unknown>>,
    "seqNo",
    path,
    errors,
    validateEventLsnValue,
  );
  validateRequiredMerkleRootRecordField(
    value as Readonly<Record<string, unknown>>,
    "rootHash",
    path,
    errors,
    validateMerkleRootHexValue,
  );
  validateRequiredMerkleRootRecordField(
    value as Readonly<Record<string, unknown>>,
    "signedAt",
    path,
    errors,
    validateDateValue,
  );
  validateRequiredMerkleRootRecordField(
    value as Readonly<Record<string, unknown>>,
    "ephemeralKeyId",
    path,
    errors,
    validateNonEmptyStringValue,
  );
  validateRequiredMerkleRootRecordField(
    value as Readonly<Record<string, unknown>>,
    "signature",
    path,
    errors,
    validateBase64Value,
  );
  validateRequiredMerkleRootRecordField(
    value as Readonly<Record<string, unknown>>,
    "certChainPem",
    path,
    errors,
    validateNonEmptyStringValue,
  );
}

function validateMerkleRootRecordKnownKeys(
  record: Readonly<Record<string, unknown>>,
  basePath: string,
  errors: MerkleRootRecordValidationError[],
): void {
  for (const key of Object.keys(record)) {
    if (!MERKLE_ROOT_RECORD_KEYS.has(key)) {
      addMerkleRootRecordError(errors, pointer(basePath, key), "is not allowed");
    }
  }
}

function validateRequiredMerkleRootRecordField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  errors: MerkleRootRecordValidationError[],
  validator: (value: unknown, path: string, errors: MerkleRootRecordValidationError[]) => void,
): void {
  const fieldPath = pointer(basePath, key);
  if (!hasOwn(record, key) || record[key] === undefined) {
    addMerkleRootRecordError(errors, fieldPath, "is required");
    return;
  }
  validator(record[key], fieldPath, errors);
}

function validateEventLsnValue(
  value: unknown,
  path: string,
  errors: MerkleRootRecordValidationError[],
): void {
  if (typeof value !== "string") {
    addMerkleRootRecordError(errors, path, "must be an EventLsn string");
    return;
  }
  try {
    parseLsn(value as EventLsn);
  } catch (err) {
    addMerkleRootRecordError(errors, path, err instanceof Error ? err.message : "invalid LSN");
  }
}

function validateMerkleRootHexValue(
  value: unknown,
  path: string,
  errors: MerkleRootRecordValidationError[],
): void {
  if (!isMerkleRootHex(value)) {
    addMerkleRootRecordError(errors, path, "must be a sha256 hex digest");
  }
}

function validateDateValue(
  value: unknown,
  path: string,
  errors: MerkleRootRecordValidationError[],
): void {
  if (!(value instanceof Date) || Number.isNaN(value.getTime())) {
    addMerkleRootRecordError(errors, path, "must be a valid Date");
  }
}

function validateNonEmptyStringValue(
  value: unknown,
  path: string,
  errors: MerkleRootRecordValidationError[],
): void {
  if (typeof value !== "string" || value.length === 0) {
    addMerkleRootRecordError(errors, path, "must be a non-empty string");
  }
}

function validateBase64Value(
  value: unknown,
  path: string,
  errors: MerkleRootRecordValidationError[],
): void {
  if (typeof value !== "string" || value.length === 0 || !BASE64_RE.test(value)) {
    addMerkleRootRecordError(errors, path, "must be a non-empty base64 string");
  }
}

function eventLsnFromJson(value: string, path: string): EventLsn {
  try {
    parseLsn(value as EventLsn);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
  return value as EventLsn;
}

function asMerkleRootHexAt(value: string, path: string): MerkleRootHex {
  try {
    return asMerkleRootHex(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
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

function requiredNonEmptyStringFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): string {
  const value = requiredStringFromJson(record, key, basePath);
  if (value.length === 0) {
    throw new Error(`${pointer(basePath, key)}: must be a non-empty string`);
  }
  return value;
}

function requiredBase64StringFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): string {
  const value = requiredStringFromJson(record, key, basePath);
  if (value.length === 0 || !BASE64_RE.test(value)) {
    throw new Error(`${pointer(basePath, key)}: must be a non-empty base64 string`);
  }
  return value;
}

function requiredDateFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): Date {
  const value = requiredStringFromJson(record, key, basePath);
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

function addMerkleRootRecordError(
  errors: MerkleRootRecordValidationError[],
  path: string,
  message: string,
): void {
  errors.push({ path, message });
}

function formatMerkleRootRecordValidationErrors(
  errors: readonly MerkleRootRecordValidationError[],
): string {
  return errors.map((error) => `${error.path}: ${error.message}`).join("; ");
}

function errorMessage(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

function safeRecordSeqNo(record: AuditEventRecord, fallback: EventLsn): EventLsn {
  try {
    return record.seqNo;
  } catch {
    return fallback;
  }
}
