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
import { canonicalJSON } from "./canonical-json.ts";
import { type EventLsn, GENESIS_LSN, isEqualLsn, nextLsn } from "./event-lsn.ts";
import type { ReceiptId } from "./receipt.ts";
import { asSha256Hex, type Sha256Hex, sha256Hex } from "./sha256.ts";

export type MerkleRootHex = Brand<string, "MerkleRootHex">;

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
  let expectedSeq: EventLsn = GENESIS_LSN;
  let lastSeen: EventLsn = GENESIS_LSN;

  for (let i = 0; i < records.length; i++) {
    const r = records[i];
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
    empty: false,
    lastEventHash: expectedPrev,
    lastSeqNo: lastSeen,
  };
}

function bytesToBase64(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64");
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
