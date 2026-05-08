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
// is locked here, exposed via golden vectors in tests/audit-event.spec.ts.

import type { Brand } from "./brand.ts";
import { canonicalJSON } from "./canonical-json.ts";
import type { ReceiptId } from "./receipt.ts";
import { type Sha256Hex, sha256Hex } from "./sha256.ts";

export type AuditSeqNo = Brand<number, "AuditSeqNo">;
export type MerkleRootHex = Brand<string, "MerkleRootHex">;

export type AuditEventKind =
  | "receipt_created"
  | "receipt_updated"
  | "receipt_finalized"
  | "approval_requested"
  | "approval_decision"
  | "cost_event"
  | "tool_call_started"
  | "tool_call_completed"
  | "external_write_proposed"
  | "external_write_applied"
  | "external_write_failed"
  | "boot_marker"
  | "merkle_root";

export interface AuditEventPayload {
  readonly kind: AuditEventKind;
  readonly receiptId?: ReceiptId | undefined;
  // Opaque body bytes; hashing covers them via base64 in the canonical
  // serialization. Storage is CBOR-line at the broker.
  readonly body: Uint8Array;
}

export interface AuditEventRecord {
  readonly seqNo: AuditSeqNo;
  readonly timestamp: Date;
  readonly prevHash: Sha256Hex;
  readonly eventHash: Sha256Hex;
  readonly payload: AuditEventPayload;
}

export interface MerkleRootRecord {
  readonly seqNo: AuditSeqNo;
  readonly rootHash: MerkleRootHex;
  readonly signedAt: Date;
  readonly ephemeralKeyId: string;
  readonly signature: string; // base64
  readonly certChainPem: string;
}

export const GENESIS_PREV_HASH = sha256Hex("wuphf:audit:genesis:v1");

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
    seqNo: record.seqNo as number,
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
  const buf = new Uint8Array(prevHash.length + recordBytes.length);
  buf.set(new TextEncoder().encode(prevHash), 0);
  buf.set(recordBytes, prevHash.length);
  return sha256Hex(buf);
}

/**
 * Convenience: compute eventHash directly from a record using the canonical
 * serializer.
 */
export function computeAuditEventHash(record: AuditEventRecord): Sha256Hex {
  return computeEventHash(record.prevHash, serializeAuditEventRecordForHash(record));
}

export type ChainVerificationResult =
  | { ok: true; empty: true }
  | { ok: true; empty: false; lastEventHash: Sha256Hex; lastSeqNo: AuditSeqNo }
  | { ok: false; brokenAtSeqNo: AuditSeqNo; reason: string };

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
  if (records.length === 0) {
    return { ok: true, empty: true };
  }

  let expectedPrev: Sha256Hex = GENESIS_PREV_HASH;
  let expectedSeq = 0;

  for (let i = 0; i < records.length; i++) {
    const r = records[i];
    if (r === undefined) {
      return {
        ok: false,
        brokenAtSeqNo: expectedSeq as AuditSeqNo,
        reason: "missing record",
      };
    }
    if ((r.seqNo as number) !== expectedSeq) {
      return {
        ok: false,
        brokenAtSeqNo: r.seqNo,
        reason: `seq_no gap: expected ${expectedSeq}, got ${r.seqNo as number}`,
      };
    }
    if (r.prevHash !== expectedPrev) {
      return {
        ok: false,
        brokenAtSeqNo: r.seqNo,
        reason: `prev_hash mismatch at seq ${r.seqNo as number}`,
      };
    }
    const recomputed = computeEventHash(r.prevHash, serialize(r));
    if (recomputed !== r.eventHash) {
      return {
        ok: false,
        brokenAtSeqNo: r.seqNo,
        reason: `event_hash mismatch at seq ${r.seqNo as number}`,
      };
    }
    expectedPrev = r.eventHash;
    expectedSeq += 1;
  }

  return {
    ok: true,
    empty: false,
    lastEventHash: expectedPrev,
    lastSeqNo: (expectedSeq - 1) as AuditSeqNo,
  };
}

function bytesToBase64(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64");
}
