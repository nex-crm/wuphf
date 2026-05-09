import { Buffer } from "node:buffer";
import { readFileSync } from "node:fs";
import fc from "fast-check";
import { describe, expect, it } from "vitest";
import {
  AUDIT_EVENT_KIND_VALUES,
  type AuditEventKind,
  type AuditEventRecord,
  asMerkleRootHex,
  type ChainFailureCode,
  type ChainVerifierState,
  computeAuditEventHash,
  computeEventHash,
  GENESIS_PREV_HASH,
  INITIAL_VERIFIER_STATE,
  isMerkleRootHex,
  type MerkleRootRecord,
  merkleRootRecordFromJson,
  merkleRootRecordToJsonValue,
  PAYLOAD_KIND_METADATA,
  serializeAuditEventRecordForHash,
  validateMerkleRootRecord,
  verifyChain,
  verifyChainIncremental,
} from "../src/audit-event.ts";
import {
  MAX_AUDIT_CHAIN_BATCH_SIZE,
  MAX_AUDIT_EVENT_BODY_BYTES,
  MAX_MERKLE_ROOT_CERT_CHAIN_BYTES,
  MAX_MERKLE_ROOT_SIGNATURE_BYTES,
} from "../src/budgets.ts";
import { canonicalJSON } from "../src/canonical-json.ts";
import { type EventLsn, lsnFromV1Number, parseLsn } from "../src/event-lsn.ts";
import { asReceiptId, type ReceiptId } from "../src/receipt.ts";
import { asSha256Hex, type Sha256Hex, sha256Hex } from "../src/sha256.ts";

interface AuditEventVectorPayloadInput {
  readonly kind: AuditEventKind;
  readonly receiptId: string | null;
  readonly bodyB64: string;
}

interface AuditEventVectorInput {
  readonly seqNo: string;
  readonly timestamp: string;
  readonly prevHash: string;
  readonly payload: AuditEventVectorPayloadInput;
}

interface AuditEventVectorExpected {
  readonly canonicalSerialization: string;
  readonly eventHash: string;
}

interface AuditEventVector {
  readonly name: string;
  readonly input: AuditEventVectorInput;
  readonly expected: AuditEventVectorExpected;
}

interface MerkleRootVectorInput {
  readonly seqNo: string;
  readonly rootHash: string;
  readonly signedAt: string;
  readonly ephemeralKeyId: string;
  readonly signature: string;
  readonly certChainPem: string;
}

interface MerkleRootVectorExpected {
  readonly canonicalJson: string;
}

interface MerkleRootVector {
  readonly name: string;
  readonly input: MerkleRootVectorInput;
  readonly expected: MerkleRootVectorExpected;
}

interface AuditEventVectorsFixture {
  readonly schemaVersion: 1;
  readonly comment: string;
  readonly vectors: readonly AuditEventVector[];
  readonly merkleRootVectors: readonly MerkleRootVector[];
}

interface AuditRecordInput {
  readonly seqNo?: EventLsn;
  readonly timestamp?: Date;
  readonly prevHash?: Sha256Hex;
  readonly eventHash?: Sha256Hex;
  readonly payload?: AuditEventRecord["payload"];
}

const auditEventKindSet: ReadonlySet<string> = new Set(AUDIT_EVENT_KIND_VALUES);
const auditEventVectors = loadAuditEventVectors();

function chainOfLength(n: number): AuditEventRecord[] {
  const out: AuditEventRecord[] = [];
  let prev: Sha256Hex = GENESIS_PREV_HASH;
  for (let i = 0; i < n; i++) {
    const partial: AuditEventRecord = {
      seqNo: lsnFromV1Number(i),
      timestamp: new Date(2026, 4, 8, 0, 0, i),
      prevHash: prev,
      eventHash: GENESIS_PREV_HASH, // placeholder
      payload: {
        kind: "receipt_created",
        body: new TextEncoder().encode(`event-${i}`),
      },
    };
    const eventHash = computeAuditEventHash(partial);
    const finalRecord: AuditEventRecord = { ...partial, eventHash };
    out.push(finalRecord);
    prev = eventHash;
  }
  return out;
}

function localLsn(lsn: EventLsn): number {
  return parseLsn(lsn).localLsn;
}

describe("audit-event chain verification", () => {
  it("verifies an empty chain with no last seq and genesis state implied", () => {
    const result = verifyChain([]);
    expect(result).toEqual({ ok: true, empty: true });
  });

  it("verifies a single-record chain", () => {
    const chain = chainOfLength(1);
    const result = verifyChain(chain);
    expect(result.ok).toBe(true);
    if (result.ok && !result.empty) {
      expect(localLsn(result.lastSeqNo)).toBe(0);
    }
  });

  it("verifies a 100-record chain", () => {
    const chain = chainOfLength(100);
    const result = verifyChain(chain);
    expect(result.ok).toBe(true);
    if (result.ok && !result.empty) {
      expect(localLsn(result.lastSeqNo)).toBe(99);
    }
  });

  it("verifies a 25,000-record chain in bounded incremental batches", () => {
    const chain = chainOfLength(25_000);
    const allAtOnce = verifyChain(chain);

    let state = INITIAL_VERIFIER_STATE;
    for (const batch of [
      chain.slice(0, MAX_AUDIT_CHAIN_BATCH_SIZE),
      chain.slice(MAX_AUDIT_CHAIN_BATCH_SIZE, MAX_AUDIT_CHAIN_BATCH_SIZE * 2),
      chain.slice(MAX_AUDIT_CHAIN_BATCH_SIZE * 2),
    ]) {
      const result = verifyChainIncremental(state, batch);
      expect(result.ok).toBe(true);
      if (!result.ok) throw new Error(result.reason);
      state = result.state;
    }

    expect(allAtOnce.ok).toBe(true);
    if (allAtOnce.ok && !allAtOnce.empty) {
      expect(state.recordsVerified).toBe(25_000);
      expect(state.expectedPrev).toBe(allAtOnce.lastEventHash);
      expect(state.lastSeen).toBe(allAtOnce.lastSeqNo);
    }
  });

  it("resumes across three batches from only the previous successful verifier state", () => {
    const chain = chainOfLength(9);
    const firstState = expectIncrementalState(
      verifyChainIncremental({ ...INITIAL_VERIFIER_STATE }, chain.slice(0, 3)),
    );
    expect(firstState).toEqual({
      expectedPrev: recordAt(chain, 2).eventHash,
      expectedSeq: lsnFromV1Number(3),
      lastSeen: lsnFromV1Number(2),
      recordsVerified: 3,
    });

    const persistedFirstState: ChainVerifierState = { ...firstState };
    const secondState = expectIncrementalState(
      verifyChainIncremental(persistedFirstState, chain.slice(3, 6)),
    );
    expect(secondState).toEqual({
      expectedPrev: recordAt(chain, 5).eventHash,
      expectedSeq: lsnFromV1Number(6),
      lastSeen: lsnFromV1Number(5),
      recordsVerified: 6,
    });

    const thirdState = expectIncrementalState(
      verifyChainIncremental({ ...secondState }, chain.slice(6, 9)),
    );
    expect(thirdState).toEqual({
      expectedPrev: recordAt(chain, 8).eventHash,
      expectedSeq: lsnFromV1Number(9),
      lastSeen: lsnFromV1Number(8),
      recordsVerified: 9,
    });
  });

  it("reaches the same final state when one resumed batch is split in two", () => {
    const chain = chainOfLength(9);
    const firstState = expectIncrementalState(
      verifyChainIncremental(INITIAL_VERIFIER_STATE, chain.slice(0, 3)),
    );
    const unsplitFinalState = expectIncrementalState(
      verifyChainIncremental(firstState, chain.slice(3, 9)),
    );

    const splitMiddleState = expectIncrementalState(
      verifyChainIncremental(firstState, chain.slice(3, 5)),
    );
    const splitFinalState = expectIncrementalState(
      verifyChainIncremental(splitMiddleState, chain.slice(5, 9)),
    );

    expect(splitFinalState).toEqual(unsplitFinalState);
  });

  it("keeps an already advanced verifier state for an empty resumed batch", () => {
    const chain = chainOfLength(2);
    const advancedState = expectIncrementalState(
      verifyChainIncremental(INITIAL_VERIFIER_STATE, chain),
    );

    const result = verifyChainIncremental({ ...advancedState }, []);

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.state).toEqual(advancedState);
    }
  });

  it("rejects corrupt resumed state at the first record it can no longer chain", () => {
    const chain = chainOfLength(3);
    const resumedState = expectIncrementalState(
      verifyChainIncremental(INITIAL_VERIFIER_STATE, chain.slice(0, 2)),
    );

    expectFailureCode(
      verifyChainIncremental(
        { ...resumedState, expectedPrev: GENESIS_PREV_HASH },
        chain.slice(2, 3),
      ),
      "prev_hash_mismatch",
      2,
    );
    expectFailureCode(
      verifyChainIncremental(
        { ...resumedState, expectedSeq: lsnFromV1Number(1) },
        chain.slice(2, 3),
      ),
      "seq_gap",
      2,
    );
  });

  it("incremental verification reports tampering in the middle of the second batch", () => {
    const chain = chainOfLength(25_000);
    chain[17_500] = { ...recordAt(chain, 17_500), eventHash: GENESIS_PREV_HASH };

    const firstBatch = verifyChainIncremental(
      INITIAL_VERIFIER_STATE,
      chain.slice(0, MAX_AUDIT_CHAIN_BATCH_SIZE),
    );
    expect(firstBatch.ok).toBe(true);
    if (!firstBatch.ok) throw new Error(firstBatch.reason);

    const secondBatch = verifyChainIncremental(
      firstBatch.state,
      chain.slice(MAX_AUDIT_CHAIN_BATCH_SIZE, MAX_AUDIT_CHAIN_BATCH_SIZE * 2),
    );

    expect(secondBatch.ok).toBe(false);
    if (!secondBatch.ok) {
      expect(secondBatch.code).toBe("event_hash_mismatch");
      expect(secondBatch.brokenAtSeqNo as string).toBe("v1:17500");
    }
  });

  it("rejects an oversized incremental batch before per-record serialization", () => {
    const chain = chainOfLength(MAX_AUDIT_CHAIN_BATCH_SIZE + 1);
    let serializerCalls = 0;

    const result = verifyChainIncremental(INITIAL_VERIFIER_STATE, chain, (record) => {
      serializerCalls += 1;
      return serializeAuditEventRecordForHash(record);
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.code).toBe("batch_too_large");
      expect(result.reason).toMatch(/batch too large/);
    }
    expect(serializerCalls).toBe(0);
  });

  it("accepts an exact-at-cap incremental batch", () => {
    const chain = chainOfLength(MAX_AUDIT_CHAIN_BATCH_SIZE);
    const result = verifyChainIncremental(INITIAL_VERIFIER_STATE, chain);

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.state.recordsVerified).toBe(MAX_AUDIT_CHAIN_BATCH_SIZE);
      expect(result.state.expectedPrev).toBe(
        recordAt(chain, MAX_AUDIT_CHAIN_BATCH_SIZE - 1).eventHash,
      );
    }
  });

  it("keeps the genesis verifier state for an empty first batch", () => {
    const result = verifyChainIncremental(INITIAL_VERIFIER_STATE, []);

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.state).toBe(INITIAL_VERIFIER_STATE);
      expect(result.state).toEqual({
        expectedPrev: GENESIS_PREV_HASH,
        expectedSeq: lsnFromV1Number(0),
        lastSeen: lsnFromV1Number(0),
        recordsVerified: 0,
      });
    }
  });

  it("rejects a chain with a tampered eventHash", () => {
    const chain = chainOfLength(5);
    chain[2] = { ...recordAt(chain, 2), eventHash: GENESIS_PREV_HASH };
    const result = verifyChain(chain);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(localLsn(result.brokenAtSeqNo)).toBe(2);
      expect(result.code).toBe("event_hash_mismatch");
    }
  });

  it("rejects a chain with a tampered prevHash", () => {
    const chain = chainOfLength(5);
    chain[3] = { ...recordAt(chain, 3), prevHash: GENESIS_PREV_HASH };
    const result = verifyChain(chain);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(localLsn(result.brokenAtSeqNo)).toBe(3);
      expect(result.code).toBe("prev_hash_mismatch");
    }
  });

  it("rejects a chain with a missing record (gap)", () => {
    const chain = chainOfLength(5);
    chain.splice(2, 1); // remove seq 2
    const result = verifyChain(chain);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(localLsn(result.brokenAtSeqNo)).toBe(3);
      expect(result.code).toBe("seq_gap");
    }
  });

  it("returns typed missing_record for a sparse chain hole", () => {
    const chain = chainOfLength(3);
    Reflect.deleteProperty(chain, "1");

    const result = verifyChain(chain);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(localLsn(result.brokenAtSeqNo)).toBe(1);
      expect(result.code).toBe("missing_record");
    }
  });

  it("returns typed seq_gap when a record seqNo skips ahead", () => {
    const chain = chainOfLength(3);
    chain[1] = { ...recordAt(chain, 1), seqNo: lsnFromV1Number(2) };

    const result = verifyChain(chain);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.code).toBe("seq_gap");
      expect(localLsn(result.brokenAtSeqNo)).toBe(2);
    }
  });

  it("returns typed serialization_threw for malformed record timestamps", () => {
    const chain = chainOfLength(1);
    chain[0] = { ...recordAt(chain, 0), timestamp: new Date("invalid") };

    const result = verifyChain(chain);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.code).toBe("serialization_threw");
      expect(localLsn(result.brokenAtSeqNo)).toBe(0);
      expect(result.reason).toMatch(/Invalid time value|serialization threw/);
    }
  });

  it("returns typed serialization_threw for hash-consistent records with unknown payload kinds", () => {
    const record = recordAt(chainOfLength(1), 0);
    const tampered: AuditEventRecord = {
      ...record,
      eventHash: GENESIS_PREV_HASH,
      payload: {
        ...record.payload,
        kind: "made_up_kind" as AuditEventKind,
      },
    };
    const hashConsistentTampered: AuditEventRecord = {
      ...tampered,
      eventHash: computeEventHash(tampered.prevHash, legacySerializeAuditEventRecord(tampered)),
    };

    const result = verifyChain([hashConsistentTampered]);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.code).toBe("serialization_threw");
      expect(result.reason).toMatch(/invalid payload\.kind.*made_up_kind/);
    }
  });

  it('rejects payload.kind = "made_up" in the writer-side serializer', () => {
    const record = auditRecord({
      payload: {
        kind: "made_up" as AuditEventKind,
        body: new TextEncoder().encode("hostile-kind"),
      },
    });

    expect(() => serializeAuditEventRecordForHash(record)).toThrow(
      /invalid payload\.kind "made_up"/,
    );
  });

  for (const testCase of INVALID_PAYLOAD_KIND_DESCRIPTION_CASES) {
    it(`describes invalid payload.kind ${testCase.name} in serializer errors`, () => {
      const record = auditRecord({
        payload: {
          kind: testCase.kind as unknown as AuditEventKind,
          body: new TextEncoder().encode("hostile-kind"),
        },
      });

      expect(() => serializeAuditEventRecordForHash(record)).toThrow(testCase.message);
    });
  }

  it('rejects payload.kind = "made_up" in verifier-side serialization', () => {
    const record = auditRecord({
      payload: {
        kind: "made_up" as AuditEventKind,
        body: new TextEncoder().encode("hostile-kind"),
      },
    });

    expectFailureCode(verifyChain([record]), "serialization_threw", 0);
  });

  it("rejects shape-invalid records before invoking a custom verifier serializer", () => {
    const valid = auditRecord();
    const validBytes = serializeAuditEventRecordForHash(valid);
    const record: AuditEventRecord = {
      ...valid,
      eventHash: computeEventHash(valid.prevHash, validBytes),
      payload: {
        ...valid.payload,
        kind: "made_up_kind" as AuditEventKind,
      },
    };
    let serializerCalls = 0;

    const result = verifyChain([record], () => {
      serializerCalls += 1;
      return validBytes;
    });

    expectFailureCode(result, "serialization_threw", 0);
    expect(serializerCalls).toBe(0);
    if (!result.ok) {
      expect(result.reason).toMatch(/invalid payload\.kind.*made_up_kind/);
    }
  });

  for (const [index, kind] of AUDIT_EVENT_KIND_VALUES.entries()) {
    it(`accepts payload.kind ${kind} through serializer and verifier`, () => {
      const partial = auditRecord({
        payload: {
          kind,
          receiptId: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
          body: new Uint8Array([index, 0, 255]),
        },
      });
      const record: AuditEventRecord = {
        ...partial,
        eventHash: computeAuditEventHash(partial),
      };

      const projection = JSON.parse(
        new TextDecoder().decode(serializeAuditEventRecordForHash(record)),
      ) as { payload: { kind: string; receiptId: string; bodyB64: string } };

      expect(projection.payload.kind).toBe(kind);
      expect(projection.payload.receiptId).toBe("01ARZ3NDEKTSV4RRFFQ69G5FAV");
      expect(projection.payload.bodyB64).toBe(Buffer.from([index, 0, 255]).toString("base64"));
      expect(verifyChain([record])).toEqual({
        ok: true,
        empty: false,
        lastEventHash: record.eventHash,
        lastSeqNo: lsnFromV1Number(0),
      });
    });
  }

  it("serializes non-null receiptId and non-UTF8 opaque body bytes in the hash projection", () => {
    const partial = auditRecord({
      payload: {
        kind: "receipt_updated",
        receiptId: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
        body: new Uint8Array([0, 255, 128, 64]),
      },
    });
    const record = { ...partial, eventHash: computeAuditEventHash(partial) };

    expect(new TextDecoder().decode(serializeAuditEventRecordForHash(record))).toBe(
      '{"payload":{"bodyB64":"AP+AQA==","kind":"receipt_updated","receiptId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"},"prevHash":"69292e708cc2e023492933025af465b063bdf24002e72a825b5170541b733f71","seqNo":"v1:0","timestamp":"2026-05-08T00:00:00.000Z"}',
    );
  });

  it("rejects malformed payload.receiptId in both serializer and verifier", () => {
    const malformed = auditRecord({
      payload: {
        kind: "receipt_created",
        receiptId: "lower-case-ulid" as ReceiptId,
        body: new TextEncoder().encode("bad-receipt-id"),
      },
    });
    const record: AuditEventRecord = {
      ...malformed,
      eventHash: computeEventHash(malformed.prevHash, legacySerializeAuditEventRecord(malformed)),
    };

    expect(() => serializeAuditEventRecordForHash(record)).toThrow(
      /payload\.receiptId must be an uppercase ULID ReceiptId/,
    );
    const result = verifyChain([record]);
    expectFailureCode(result, "serialization_threw", 0);
    if (!result.ok) {
      expect(result.reason).toMatch(/payload\.receiptId must be an uppercase ULID ReceiptId/);
    }
  });

  it("rejects non-Uint8Array payload bodies in the writer-side serializer", () => {
    const record = auditRecord({
      payload: {
        kind: "receipt_created",
        body: [] as unknown as Uint8Array,
      },
    });

    expect(() => serializeAuditEventRecordForHash(record)).toThrow(
      /payload\.body must be a Uint8Array/,
    );
  });

  it("accepts exact-at-cap audit event bodies in both serializer and verifier", () => {
    const partial = auditRecord({
      payload: {
        kind: "receipt_created",
        body: new Uint8Array(MAX_AUDIT_EVENT_BODY_BYTES),
      },
    });
    const record: AuditEventRecord = {
      ...partial,
      eventHash: computeAuditEventHash(partial),
    };

    expect(() => serializeAuditEventRecordForHash(record)).not.toThrow();
    expect(verifyChain([record])).toEqual({
      ok: true,
      empty: false,
      lastEventHash: record.eventHash,
      lastSeqNo: lsnFromV1Number(0),
    });
  });

  it("rejects one-over-cap audit event bodies in both serializer and verifier", () => {
    const oversized = auditRecord({
      payload: {
        kind: "receipt_created",
        body: new Uint8Array(MAX_AUDIT_EVENT_BODY_BYTES + 1),
      },
    });

    expect(() => serializeAuditEventRecordForHash(oversized)).toThrow(/MAX_AUDIT_EVENT_BODY_BYTES/);
    expectFailureCode(verifyChain([oversized]), "serialization_threw", 0);
  });

  it("returns typed serialization_threw for oversized audit event bodies", () => {
    const oversized: AuditEventRecord = {
      seqNo: lsnFromV1Number(0),
      timestamp: new Date("2026-05-08T00:00:00.000Z"),
      prevHash: GENESIS_PREV_HASH,
      eventHash: GENESIS_PREV_HASH,
      payload: {
        kind: "receipt_created",
        body: new Uint8Array(MAX_AUDIT_EVENT_BODY_BYTES + 1),
      },
    };

    const result = verifyChain([oversized]);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.code).toBe("serialization_threw");
      expect(result.reason).toMatch(/MAX_AUDIT_EVENT_BODY_BYTES/);
    }
  });

  it("returns typed lsn_threw when expected next LSN overflows", () => {
    const maxLsn = lsnFromV1Number(Number.MAX_SAFE_INTEGER);
    const nearOverflowState: ChainVerifierState = {
      expectedPrev: GENESIS_PREV_HASH,
      expectedSeq: maxLsn,
      lastSeen: lsnFromV1Number(Number.MAX_SAFE_INTEGER - 1),
      recordsVerified: Number.MAX_SAFE_INTEGER,
    };
    const partial = auditRecord({
      seqNo: maxLsn,
      prevHash: GENESIS_PREV_HASH,
      payload: {
        kind: "receipt_created",
        body: new TextEncoder().encode("max-lsn"),
      },
    });
    const record: AuditEventRecord = {
      ...partial,
      eventHash: computeAuditEventHash(partial),
    };

    expectFailureCode(
      verifyChainIncremental(nearOverflowState, [record]),
      "lsn_threw",
      Number.MAX_SAFE_INTEGER,
    );
  });

  it("returns typed serialization_threw when record access throws before serialization", () => {
    const hostileRecord = {
      get seqNo(): EventLsn {
        throw new Error("seqNo getter exploded");
      },
      timestamp: new Date("2026-05-08T00:00:00.000Z"),
      prevHash: GENESIS_PREV_HASH,
      eventHash: GENESIS_PREV_HASH,
      payload: {
        kind: "receipt_created",
        body: new TextEncoder().encode("hostile-accessor"),
      },
    } as AuditEventRecord;

    const result = verifyChainIncremental(INITIAL_VERIFIER_STATE, [hostileRecord]);

    expectFailureCode(result, "serialization_threw", 0);
    if (!result.ok) {
      expect(result.reason).toMatch(/seqNo getter exploded/);
    }
  });

  it("rejects a chain with a tampered payload (changes derived hash)", () => {
    const chain = chainOfLength(5);
    chain[2] = {
      ...recordAt(chain, 2),
      payload: {
        kind: "receipt_finalized",
        body: new TextEncoder().encode("malicious"),
      },
    };
    const result = verifyChain(chain);
    expect(result.ok).toBe(false);
  });

  it("computeEventHash is deterministic", () => {
    fc.assert(
      fc.property(fc.uint8Array({ minLength: 0, maxLength: 1024 }), (bytes) => {
        const h1 = computeEventHash(GENESIS_PREV_HASH, bytes);
        const h2 = computeEventHash(GENESIS_PREV_HASH, bytes);
        return h1 === h2;
      }),
      { numRuns: 500 },
    );
  });

  it("computeEventHash matches the previous concatenation implementation", () => {
    fc.assert(
      fc.property(fc.uint8Array({ minLength: 0, maxLength: 2048 }), (bytes) => {
        return computeEventHash(GENESIS_PREV_HASH, bytes) === previousComputeEventHash(bytes);
      }),
      { numRuns: 100 },
    );
  });

  it("computeEventHash differs for different prevHash", () => {
    const otherPrev = computeEventHash(GENESIS_PREV_HASH, new Uint8Array([1, 2, 3]));
    const sameBody = new Uint8Array([7, 7, 7]);
    expect(computeEventHash(GENESIS_PREV_HASH, sameBody)).not.toBe(
      computeEventHash(otherPrev, sameBody),
    );
  });

  describe("minimal ChainFailureCode records", () => {
    it("reports batch_too_large before reading or serializing records", () => {
      let serializerCalls = 0;
      const oversizedBatch = new Array<AuditEventRecord>(MAX_AUDIT_CHAIN_BATCH_SIZE + 1);

      const result = verifyChainIncremental(INITIAL_VERIFIER_STATE, oversizedBatch, () => {
        serializerCalls += 1;
        return new Uint8Array();
      });

      expectFailureCode(result, "batch_too_large", 0);
      expect(serializerCalls).toBe(0);
    });

    it("reports missing_record for a single sparse hole", () => {
      const sparseBatch = new Array<AuditEventRecord>(1);

      expectFailureCode(
        verifyChainIncremental(INITIAL_VERIFIER_STATE, sparseBatch),
        "missing_record",
        0,
      );
    });

    it("reports seq_gap for the first record with an unexpected LSN", () => {
      const record = auditRecord({ seqNo: lsnFromV1Number(1) });

      expectFailureCode(verifyChainIncremental(INITIAL_VERIFIER_STATE, [record]), "seq_gap", 1);
    });

    it("reports prev_hash_mismatch before serialization", () => {
      const record = auditRecord({ prevHash: sha256Hex("wrong previous hash") });

      expectFailureCode(
        verifyChainIncremental(INITIAL_VERIFIER_STATE, [record]),
        "prev_hash_mismatch",
        0,
      );
    });

    it("reports serialization_threw for the first malformed record", () => {
      const record = auditRecord({ timestamp: new Date("invalid") });

      expectFailureCode(
        verifyChainIncremental(INITIAL_VERIFIER_STATE, [record]),
        "serialization_threw",
        0,
      );
    });

    it("reports event_hash_mismatch for hash-consistent fields with a stale hash", () => {
      const record = auditRecord({ eventHash: sha256Hex("stale event hash") });

      expectFailureCode(
        verifyChainIncremental(INITIAL_VERIFIER_STATE, [record]),
        "event_hash_mismatch",
        0,
      );
    });
  });

  it("property: any tamper at position k breaks verification at k", () => {
    fc.assert(
      fc.property(
        fc.integer({ min: 1, max: 10 }),
        fc.integer({ min: 0, max: 9 }),
        (length, tamperAt) => {
          const k = Math.min(tamperAt, length - 1);
          const chain = chainOfLength(length);
          chain[k] = {
            ...recordAt(chain, k),
            eventHash: computeEventHash(GENESIS_PREV_HASH, new TextEncoder().encode("tamper")),
          };
          const result = verifyChain(chain);
          if (result.ok) return false;
          return localLsn(result.brokenAtSeqNo) <= k;
        },
      ),
      { numRuns: 200 },
    );
  });

  it("reports event_hash_mismatch at every single-bit tamper position in a 25,000-record chain", () => {
    const chain = chainOfLength(25_000);
    let state = INITIAL_VERIFIER_STATE;

    for (let index = 0; index < chain.length; index++) {
      const record = recordAt(chain, index);
      const tampered = flipFirstPayloadBodyBit(record);

      expectFailureCode(verifyChainIncremental(state, [tampered]), "event_hash_mismatch", index);
      state = expectIncrementalState(verifyChainIncremental(state, [record]));
    }

    expect(state.recordsVerified).toBe(25_000);
  });

  // Cross-language verifiers consume these golden vectors. If any of these
  // values change, the wire protocol changed — coordinate with downstream
  // verifier authors. The data lives in testdata so non-TS verifiers can load
  // the same fixture this test reads.
  describe("golden vectors (cross-language verifier contract)", () => {
    it('GENESIS_PREV_HASH is sha256("wuphf:audit:genesis:v1") in lower-hex', () => {
      expect(GENESIS_PREV_HASH).toBe(sha256Hex("wuphf:audit:genesis:v1"));
      expect(GENESIS_PREV_HASH).toBe(
        "69292e708cc2e023492933025af465b063bdf24002e72a825b5170541b733f71",
      );
    });

    it("loads the schema v1 JSON fixture", () => {
      expect(auditEventVectors.schemaVersion).toBe(1);
      expect(auditEventVectors.vectors.length).toBeGreaterThan(0);
      expect(auditEventVectors.merkleRootVectors.length).toBeGreaterThan(0);
    });

    for (const vector of auditEventVectors.vectors) {
      it(`${vector.name} produces stable canonical serialization and eventHash`, () => {
        const record = auditEventRecordFromVector(vector);
        const bytes = serializeAuditEventRecordForHash(record);

        // EventLsn is wire-encoded as a JSON string ("v1:0"), not a number.
        // This commits the multi-instance extension path (v2: "v2:<id>:<n>")
        // without a future hash-chain break.
        expect(new TextDecoder().decode(bytes)).toBe(vector.expected.canonicalSerialization);

        // Golden eventHash. Locks both pieces of the wire contract that the
        // serialization vector alone leaves open:
        //   1. prevHash is mixed in as 64-byte ASCII lower-hex, NOT 32 raw bytes.
        //   2. The mix order is `asciiLowerHex(prevHash) || jcsBytes(record)`
        //      (no separator).
        // A future implementation that switched to raw-byte mixing or changed
        // the order would preserve the serialization vector but break this
        // hash. Cross-language verifiers MUST reproduce this digit-for-digit.
        expect(computeAuditEventHash(record)).toBe(record.eventHash);
      });
    }

    it("hashes prevHash as 64 ASCII-hex bytes: sha256(asciiHex(prevHash) || jcsBytes(record))", () => {
      const record = recordAt(chainOfLength(1), 0);
      const recordBytes = serializeAuditEventRecordForHash(record);
      const handComputedInput = new Uint8Array(record.prevHash.length + recordBytes.length);
      handComputedInput.set(new TextEncoder().encode(record.prevHash), 0);
      handComputedInput.set(recordBytes, record.prevHash.length);

      expect(record.eventHash).toBe(sha256Hex(handComputedInput));
    });
  });

  describe("MerkleRootRecord public codec", () => {
    it("brands lowercase sha256 root hashes", () => {
      const rootHash = "0123456789abcdef".repeat(4);

      expect(asMerkleRootHex(rootHash) as string).toBe(rootHash);
      expect(isMerkleRootHex(rootHash)).toBe(true);
      expect(isMerkleRootHex(rootHash.toUpperCase())).toBe(false);
      expect(() => asMerkleRootHex("f".repeat(63))).toThrow(/sha256/);
    });

    for (const vector of auditEventVectors.merkleRootVectors) {
      it(`${vector.name} round-trips and produces stable canonical JSON`, () => {
        const record = merkleRootRecordFromVector(vector);

        expect(validateMerkleRootRecord(record)).toEqual({ ok: true });
        expect(merkleRootRecordFromJson(merkleRootRecordToJsonValue(record))).toEqual(record);
        expect(canonicalJSON(merkleRootRecordToJsonValue(record))).toBe(
          vector.expected.canonicalJson,
        );
      });
    }

    it("rejects unknown keys and invalid Merkle root wire values", () => {
      const vector = firstMerkleRootVector();
      const record = merkleRootRecordFromVector(vector);
      const validation = validateMerkleRootRecord({ ...record, shadow: "nope" });

      expect(validation.ok).toBe(false);
      if (!validation.ok) {
        expect(
          validation.errors.some(
            (error) => error.path === "/shadow" && /not allowed/.test(error.message),
          ),
        ).toBe(true);
      }
      expect(() => merkleRootRecordFromJson({ ...vector.input, rootHash: "A".repeat(64) })).toThrow(
        /\/rootHash: asMerkleRootHex/,
      );
      expect(() => merkleRootRecordFromJson({ ...vector.input, signature: "" })).toThrow(
        /\/signature: must be a non-empty base64 string/,
      );
      expect(() => merkleRootRecordFromJson({ ...vector.input, certChainPem: "" })).toThrow(
        /\/certChainPem: must be a non-empty string/,
      );
    });

    it("accepts exact-at-cap MerkleRoot signature and cert chain values", () => {
      const vector = firstMerkleRootVector();
      const record = merkleRootRecordFromVector(vector);
      const exactSignature = "A".repeat(MAX_MERKLE_ROOT_SIGNATURE_BYTES);
      const exactCertChain = "C".repeat(MAX_MERKLE_ROOT_CERT_CHAIN_BYTES);

      expect(
        validateMerkleRootRecord({
          ...record,
          signature: exactSignature,
          certChainPem: exactCertChain,
        }),
      ).toEqual({ ok: true });
      expect(() =>
        merkleRootRecordFromJson({
          ...vector.input,
          signature: exactSignature,
          certChainPem: exactCertChain,
        }),
      ).not.toThrow();
    });

    it("rejects one-over-cap MerkleRoot signature and cert chain values", () => {
      const vector = firstMerkleRootVector();
      const record = merkleRootRecordFromVector(vector);
      const oversizedSignature = "A".repeat(MAX_MERKLE_ROOT_SIGNATURE_BYTES + 1);
      const oversizedCertChain = "C".repeat(MAX_MERKLE_ROOT_CERT_CHAIN_BYTES + 1);

      const signatureValidation = validateMerkleRootRecord({
        ...record,
        signature: oversizedSignature,
      });
      expect(signatureValidation.ok).toBe(false);
      if (!signatureValidation.ok) {
        expect(
          signatureValidation.errors.some(
            (error) =>
              error.path === "/signature" &&
              /MerkleRootRecord\.signature bytes exceeds budget/.test(error.message),
          ),
        ).toBe(true);
      }
      expect(() =>
        merkleRootRecordFromJson({ ...vector.input, signature: oversizedSignature }),
      ).toThrow(/\/signature: MerkleRootRecord\.signature bytes exceeds budget/);

      const certValidation = validateMerkleRootRecord({
        ...record,
        certChainPem: oversizedCertChain,
      });
      expect(certValidation.ok).toBe(false);
      if (!certValidation.ok) {
        expect(
          certValidation.errors.some(
            (error) =>
              error.path === "/certChainPem" &&
              /MerkleRootRecord\.certChainPem bytes exceeds budget/.test(error.message),
          ),
        ).toBe(true);
      }
      expect(() =>
        merkleRootRecordFromJson({ ...vector.input, certChainPem: oversizedCertChain }),
      ).toThrow(/\/certChainPem: MerkleRootRecord\.certChainPem bytes exceeds budget/);
    });

    for (const key of MERKLE_ROOT_RECORD_FIELD_NAMES) {
      it(`rejects a MerkleRootRecord JSON object missing ${key}`, () => {
        const input: Record<string, unknown> = { ...firstMerkleRootVector().input };
        Reflect.deleteProperty(input, key);
        const validationInput = mutableMerkleRootRecord(
          merkleRootRecordFromVector(firstMerkleRootVector()),
        );
        Reflect.deleteProperty(validationInput, key);

        expect(() => merkleRootRecordFromJson(input)).toThrow(new RegExp(`/${key}: is required`));
        const validation = validateMerkleRootRecord(validationInput);
        expect(validation.ok).toBe(false);
        if (!validation.ok) {
          expect(
            validation.errors.some(
              (error) => error.path === `/${key}` && /is required/.test(error.message),
            ),
          ).toBe(true);
        }
      });
    }

    for (const testCase of MERKLE_ROOT_FIELD_VALIDATION_CASES) {
      it(`validates MerkleRootRecord ${testCase.name}`, () => {
        const record = mutableMerkleRootRecord(merkleRootRecordFromVector(firstMerkleRootVector()));
        const validation = validateMerkleRootRecord(testCase.apply(record));

        expect(validation.ok).toBe(false);
        if (!validation.ok) {
          expect(
            validation.errors.some(
              (error) => error.path === testCase.path && testCase.message.test(error.message),
            ),
          ).toBe(true);
        }
      });
    }

    it("rejects non-object MerkleRootRecord validator inputs", () => {
      for (const input of [null, [], "not-an-object"]) {
        const validation = validateMerkleRootRecord(input);

        expect(validation.ok).toBe(false);
        if (!validation.ok) {
          expect(
            validation.errors.some(
              (error) => error.path === "" && /must be an object/.test(error.message),
            ),
          ).toBe(true);
        }
      }
    });

    it("returns a validation error when MerkleRootRecord property access throws", () => {
      const hostile: Record<string, unknown> = {};
      Object.defineProperty(hostile, "seqNo", {
        enumerable: true,
        get() {
          throw new Error("seqNo getter exploded");
        },
      });

      const validation = validateMerkleRootRecord(hostile);

      expect(validation.ok).toBe(false);
      if (!validation.ok) {
        expect(validation.errors).toEqual([{ path: "", message: "seqNo getter exploded" }]);
      }
    });

    it("rejects invalid MerkleRootRecord JSON decoder fields", () => {
      const vector = firstMerkleRootVector();

      expect(() => merkleRootRecordFromJson({ ...vector.input, seqNo: "v1:01" })).toThrow(
        /\/seqNo: parseLsn: malformed v1 LSN/,
      );
      expect(() => merkleRootRecordFromJson({ ...vector.input, rootHash: 42 })).toThrow(
        /\/rootHash: must be a string/,
      );
      expect(() =>
        merkleRootRecordFromJson({ ...vector.input, signedAt: "2026-05-08T12:34:56Z" }),
      ).toThrow(/\/signedAt: must be an ISO 8601 string/);
      expect(() =>
        merkleRootRecordFromJson({ ...vector.input, signedAt: "2026-02-31T00:00:00.000Z" }),
      ).toThrow(/\/signedAt: must be a valid ISO 8601 instant/);
    });

    it("rejects invalid MerkleRootRecord values when encoding to JSON", () => {
      const record = merkleRootRecordFromVector(firstMerkleRootVector());

      expect(() => merkleRootRecordToJsonValue({ ...record, signature: "" })).toThrow(
        /\/signature: must be a non-empty base64 string/,
      );
    });
  });

  describe("payload kind metadata", () => {
    it("covers every AuditEventKind at runtime", () => {
      expect(Object.keys(PAYLOAD_KIND_METADATA).sort()).toEqual(
        [...AUDIT_EVENT_KIND_VALUES].sort(),
      );
      for (const kind of AUDIT_EVENT_KIND_VALUES) {
        expect(PAYLOAD_KIND_METADATA[kind].description.length).toBeGreaterThan(0);
        expect(PAYLOAD_KIND_METADATA[kind].bodySchemaRef.length).toBeGreaterThan(0);
      }
    });
  });
});

const INVALID_PAYLOAD_KIND_DESCRIPTION_CASES = [
  { name: "symbol", kind: Symbol("kind"), message: /Symbol\(kind\)/ },
  { name: "null", kind: null, message: /null/ },
  { name: "object", kind: { madeUp: true }, message: /\[object\]/ },
  { name: "number", kind: 42, message: /42/ },
] as const;

const MERKLE_ROOT_RECORD_FIELD_NAMES = [
  "seqNo",
  "rootHash",
  "signedAt",
  "ephemeralKeyId",
  "signature",
  "certChainPem",
] as const satisfies readonly (keyof MerkleRootRecord)[];

const MERKLE_ROOT_FIELD_VALIDATION_CASES = [
  {
    name: "seqNo type",
    path: "/seqNo",
    message: /EventLsn string/,
    apply: (record: MutableMerkleRootRecord): MutableMerkleRootRecord => ({
      ...record,
      seqNo: 42,
    }),
  },
  {
    name: "seqNo format",
    path: "/seqNo",
    message: /malformed v1 LSN/,
    apply: (record: MutableMerkleRootRecord): MutableMerkleRootRecord => ({
      ...record,
      seqNo: "v1:01",
    }),
  },
  {
    name: "rootHash lowercase sha256 format",
    path: "/rootHash",
    message: /sha256 hex digest/,
    apply: (record: MutableMerkleRootRecord): MutableMerkleRootRecord => ({
      ...record,
      rootHash: "A".repeat(64),
    }),
  },
  {
    name: "signedAt Date validity",
    path: "/signedAt",
    message: /valid Date/,
    apply: (record: MutableMerkleRootRecord): MutableMerkleRootRecord => ({
      ...record,
      signedAt: new Date("invalid"),
    }),
  },
  {
    name: "ephemeralKeyId presence",
    path: "/ephemeralKeyId",
    message: /non-empty string/,
    apply: (record: MutableMerkleRootRecord): MutableMerkleRootRecord => ({
      ...record,
      ephemeralKeyId: "",
    }),
  },
  {
    name: "signature presence",
    path: "/signature",
    message: /non-empty base64 string/,
    apply: (record: MutableMerkleRootRecord): MutableMerkleRootRecord => ({
      ...record,
      signature: "",
    }),
  },
  {
    name: "signature base64 alphabet",
    path: "/signature",
    message: /non-empty base64 string/,
    apply: (record: MutableMerkleRootRecord): MutableMerkleRootRecord => ({
      ...record,
      signature: "not base64!",
    }),
  },
  {
    name: "certChainPem presence",
    path: "/certChainPem",
    message: /non-empty string/,
    apply: (record: MutableMerkleRootRecord): MutableMerkleRootRecord => ({
      ...record,
      certChainPem: "",
    }),
  },
] as const;

type MutableMerkleRootRecord = Record<keyof MerkleRootRecord, unknown>;

function recordAt(chain: readonly AuditEventRecord[], index: number): AuditEventRecord {
  const record = chain[index];
  if (record === undefined) {
    throw new Error(`missing audit record at ${index}`);
  }
  return record;
}

function auditRecord(input: AuditRecordInput = {}): AuditEventRecord {
  return {
    seqNo: input.seqNo ?? lsnFromV1Number(0),
    timestamp: input.timestamp ?? new Date("2026-05-08T00:00:00.000Z"),
    prevHash: input.prevHash ?? GENESIS_PREV_HASH,
    eventHash: input.eventHash ?? GENESIS_PREV_HASH,
    payload: input.payload ?? {
      kind: "receipt_created",
      body: new TextEncoder().encode("event-0"),
    },
  };
}

function expectIncrementalState(
  result: ReturnType<typeof verifyChainIncremental>,
): ChainVerifierState {
  expect(result.ok).toBe(true);
  if (!result.ok) {
    throw new Error(result.reason);
  }
  return result.state;
}

function expectFailureCode(
  result: ReturnType<typeof verifyChain> | ReturnType<typeof verifyChainIncremental>,
  code: ChainFailureCode,
  expectedLocalSeqNo: number,
): void {
  expect(result.ok).toBe(false);
  if (result.ok) {
    throw new Error(`expected ${code}, got ok`);
  }
  expect(result.code).toBe(code);
  expect(localLsn(result.brokenAtSeqNo)).toBe(expectedLocalSeqNo);
}

function flipFirstPayloadBodyBit(record: AuditEventRecord): AuditEventRecord {
  const body = new Uint8Array(record.payload.body);
  const firstByte = body[0];
  if (firstByte === undefined) {
    throw new Error("cannot flip the first bit of an empty payload body");
  }
  body[0] = firstByte ^ 1;
  return {
    ...record,
    payload: {
      ...record.payload,
      body,
    },
  };
}

function previousComputeEventHash(recordBytes: Uint8Array): Sha256Hex {
  const buf = new Uint8Array(GENESIS_PREV_HASH.length + recordBytes.length);
  buf.set(new TextEncoder().encode(GENESIS_PREV_HASH), 0);
  buf.set(recordBytes, GENESIS_PREV_HASH.length);
  return sha256Hex(buf);
}

function legacySerializeAuditEventRecord(record: AuditEventRecord): Uint8Array {
  return new TextEncoder().encode(
    canonicalJSON({
      seqNo: record.seqNo as string,
      timestamp: record.timestamp.toISOString(),
      prevHash: record.prevHash,
      payload: {
        kind: record.payload.kind,
        receiptId: record.payload.receiptId ?? null,
        bodyB64: Buffer.from(record.payload.body).toString("base64"),
      },
    }),
  );
}

function auditEventRecordFromVector(vector: AuditEventVector): AuditEventRecord {
  const receiptId = vector.input.payload.receiptId;
  const seqNo = vector.input.seqNo as EventLsn;
  parseLsn(seqNo);
  return {
    seqNo,
    timestamp: dateFromFixture(vector.input.timestamp, `${vector.name}.input.timestamp`),
    prevHash: asSha256Hex(vector.input.prevHash),
    eventHash: asSha256Hex(vector.expected.eventHash),
    payload: {
      kind: vector.input.payload.kind,
      ...(receiptId === null ? {} : { receiptId: asReceiptId(receiptId) }),
      body: Buffer.from(vector.input.payload.bodyB64, "base64"),
    },
  };
}

function loadAuditEventVectors(): AuditEventVectorsFixture {
  const parsed: unknown = JSON.parse(
    readFileSync(new URL("../testdata/audit-event-vectors.json", import.meta.url), "utf8"),
  );
  const record = requireRecord(parsed, "fixture");
  const vectors = requiredArray(record, "vectors", "fixture").map((vector, index) =>
    parseAuditEventVector(vector, `fixture.vectors.${index}`),
  );
  return {
    schemaVersion: requiredSchemaVersion(record, "schemaVersion", "fixture"),
    comment: requiredString(record, "comment", "fixture"),
    vectors,
    merkleRootVectors: requiredArray(record, "merkleRootVectors", "fixture").map((vector, index) =>
      parseMerkleRootVector(vector, `fixture.merkleRootVectors.${index}`),
    ),
  };
}

function parseAuditEventVector(value: unknown, path: string): AuditEventVector {
  const record = requireRecord(value, path);
  const input = requireRecord(requiredField(record, "input", path), `${path}.input`);
  const payload = requireRecord(
    requiredField(input, "payload", `${path}.input`),
    `${path}.input.payload`,
  );
  const expected = requireRecord(requiredField(record, "expected", path), `${path}.expected`);
  return {
    name: requiredString(record, "name", path),
    input: {
      seqNo: requiredString(input, "seqNo", `${path}.input`),
      timestamp: requiredString(input, "timestamp", `${path}.input`),
      prevHash: requiredString(input, "prevHash", `${path}.input`),
      payload: {
        kind: requiredAuditEventKind(payload, "kind", `${path}.input.payload`),
        receiptId: requiredNullableString(payload, "receiptId", `${path}.input.payload`),
        bodyB64: requiredString(payload, "bodyB64", `${path}.input.payload`),
      },
    },
    expected: {
      canonicalSerialization: requiredString(
        expected,
        "canonicalSerialization",
        `${path}.expected`,
      ),
      eventHash: requiredString(expected, "eventHash", `${path}.expected`),
    },
  };
}

function parseMerkleRootVector(value: unknown, path: string): MerkleRootVector {
  const record = requireRecord(value, path);
  const input = requireRecord(requiredField(record, "input", path), `${path}.input`);
  const expected = requireRecord(requiredField(record, "expected", path), `${path}.expected`);
  return {
    name: requiredString(record, "name", path),
    input: {
      seqNo: requiredString(input, "seqNo", `${path}.input`),
      rootHash: requiredString(input, "rootHash", `${path}.input`),
      signedAt: requiredString(input, "signedAt", `${path}.input`),
      ephemeralKeyId: requiredString(input, "ephemeralKeyId", `${path}.input`),
      signature: requiredString(input, "signature", `${path}.input`),
      certChainPem: requiredString(input, "certChainPem", `${path}.input`),
    },
    expected: {
      canonicalJson: requiredString(expected, "canonicalJson", `${path}.expected`),
    },
  };
}

function merkleRootRecordFromVector(vector: MerkleRootVector): MerkleRootRecord {
  return merkleRootRecordFromJson(vector.input);
}

function firstMerkleRootVector(): MerkleRootVector {
  const vector = auditEventVectors.merkleRootVectors[0];
  if (vector === undefined) {
    throw new Error("fixture must contain a Merkle root vector");
  }
  return vector;
}

function mutableMerkleRootRecord(record: MerkleRootRecord): MutableMerkleRootRecord {
  return {
    seqNo: record.seqNo,
    rootHash: record.rootHash,
    signedAt: record.signedAt,
    ephemeralKeyId: record.ephemeralKeyId,
    signature: record.signature,
    certChainPem: record.certChainPem,
  };
}

function requireRecord(value: unknown, path: string): Readonly<Record<string, unknown>> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${path}: must be an object`);
  }
  return value as Readonly<Record<string, unknown>>;
}

function requiredField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): unknown {
  if (!Object.hasOwn(record, key) || record[key] === undefined) {
    throw new Error(`${path}.${key}: is required`);
  }
  return record[key];
}

function requiredString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string {
  const value = requiredField(record, key, path);
  if (typeof value !== "string") {
    throw new Error(`${path}.${key}: must be a string`);
  }
  return value;
}

function requiredNullableString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string | null {
  const value = requiredField(record, key, path);
  if (value === null || typeof value === "string") return value;
  throw new Error(`${path}.${key}: must be a string or null`);
}

function requiredArray(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): readonly unknown[] {
  const value = requiredField(record, key, path);
  if (!Array.isArray(value)) {
    throw new Error(`${path}.${key}: must be an array`);
  }
  return value;
}

function requiredSchemaVersion(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): 1 {
  const value = requiredField(record, key, path);
  if (value !== 1) {
    throw new Error(`${path}.${key}: must be 1`);
  }
  return 1;
}

function requiredAuditEventKind(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): AuditEventKind {
  const value = requiredString(record, key, path);
  if (!auditEventKindSet.has(value)) {
    throw new Error(`${path}.${key}: must be an AuditEventKind`);
  }
  return value as AuditEventKind;
}

function dateFromFixture(value: string, path: string): Date {
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.toISOString() !== value) {
    throw new Error(`${path}: must be an ISO 8601 instant`);
  }
  return date;
}
