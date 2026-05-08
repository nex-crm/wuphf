// Hash-chained audit log event types.
//
// Storage: append-only CBOR-line file at `~/.wuphf/receipt_events/audit.cborl`.
// Periodic Merkle roots written to `audit.merkle`, signed by a per-boot ephemeral
// broker key whose certificate is signed by a per-install non-exportable audit
// key in the OS keychain. Without the per-install root, an attacker who tampers
// with the local log can mint a new ephemeral key and rewrite history.
//
// Verifier (`wuphf audit verify`) checks: hash continuity, signatures, receipt
// body/index agreement, terminal gzip integrity.

import type { Brand } from "./brand.ts";
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
  // CBOR-encoded sub-payload, opaque to the chain. Hashing covers it.
  readonly body: Uint8Array;
}

export interface AuditEventRecord {
  readonly seqNo: AuditSeqNo;
  readonly timestamp: Date;
  readonly prevHash: Sha256Hex; // sha256("genesis") for seqNo=0
  readonly eventHash: Sha256Hex; // sha256(prevHash || canonical(record-without-eventHash))
  readonly payload: AuditEventPayload;
}

export interface MerkleRootRecord {
  readonly seqNo: AuditSeqNo; // last seq covered by this root
  readonly rootHash: MerkleRootHex;
  readonly signedAt: Date;
  readonly ephemeralKeyId: string;
  readonly signature: string; // base64
  readonly certChainPem: string;
}

export const GENESIS_PREV_HASH = sha256Hex("wuphf:audit:genesis:v1");

/**
 * Compute event hash given the prev_hash and a canonical byte-encoding of the
 * event record minus its own eventHash field. Caller is responsible for the
 * canonical encoding (typically RFC 8785 over a JSON projection of the record).
 */
export function computeEventHash(prevHash: Sha256Hex, recordBytes: Uint8Array): Sha256Hex {
  const buf = new Uint8Array(prevHash.length + recordBytes.length);
  // prev_hash is hex; encode as UTF-8 bytes for chaining domain-separation.
  buf.set(new TextEncoder().encode(prevHash), 0);
  buf.set(recordBytes, prevHash.length);
  return sha256Hex(buf);
}

export type ChainVerificationResult =
  | { ok: true; lastEventHash: Sha256Hex; lastSeqNo: AuditSeqNo }
  | { ok: false; brokenAtSeqNo: AuditSeqNo; reason: string };

/**
 * Verify a sequence of records forms a valid hash chain rooted at GENESIS_PREV_HASH.
 * Caller supplies a `serialize` function that produces the canonical bytes for the
 * record-without-eventHash; the verifier re-derives each event hash.
 */
export function verifyChain(
  records: readonly AuditEventRecord[],
  serialize: (record: AuditEventRecord) => Uint8Array,
): ChainVerificationResult {
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

  if (records.length === 0) {
    return { ok: true, lastEventHash: GENESIS_PREV_HASH, lastSeqNo: -1 as unknown as AuditSeqNo };
  }
  return {
    ok: true,
    lastEventHash: expectedPrev,
    lastSeqNo: (expectedSeq - 1) as AuditSeqNo,
  };
}
