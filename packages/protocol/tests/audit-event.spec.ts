import { Buffer } from "node:buffer";
import { readFileSync } from "node:fs";
import fc from "fast-check";
import { describe, expect, it, vi } from "vitest";
import {
  AUDIT_EVENT_KIND_VALUES,
  type AuditEventKind,
  type AuditEventRecord,
  asMerkleRootHex,
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
import { MAX_AUDIT_CHAIN_BATCH_SIZE } from "../src/budgets.ts";
import { canonicalJSON } from "../src/canonical-json.ts";
import { type EventLsn, lsnFromV1Number, parseLsn } from "../src/event-lsn.ts";
import { asReceiptId } from "../src/receipt.ts";
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
  it("verifies an empty chain (no last seq, just empty: true)", () => {
    const result = verifyChain([]);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.empty).toBe(true);
    }
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

  it("keeps the initial verifier state for an empty first batch", () => {
    const result = verifyChainIncremental(INITIAL_VERIFIER_STATE, []);

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.state).toBe(INITIAL_VERIFIER_STATE);
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

  it("returns typed lsn_threw when expected next LSN overflows", async () => {
    vi.resetModules();
    vi.doMock("../src/event-lsn.ts", async (importOriginal) => {
      const actual = await importOriginal<typeof import("../src/event-lsn.ts")>();
      return {
        ...actual,
        GENESIS_LSN: actual.lsnFromV1Number(Number.MAX_SAFE_INTEGER),
      };
    });

    try {
      const auditEvent = await import("../src/audit-event.ts");
      const maxLsn = lsnFromV1Number(Number.MAX_SAFE_INTEGER);
      const partial: AuditEventRecord = {
        seqNo: maxLsn,
        timestamp: new Date("2026-05-08T00:00:00.000Z"),
        prevHash: auditEvent.GENESIS_PREV_HASH,
        eventHash: auditEvent.GENESIS_PREV_HASH,
        payload: {
          kind: "receipt_created",
          body: new TextEncoder().encode("max-lsn"),
        },
      };
      const lastRecord: AuditEventRecord = {
        ...partial,
        eventHash: auditEvent.computeAuditEventHash(partial),
      };
      const followUpRecord: AuditEventRecord = {
        ...lastRecord,
        payload: {
          kind: "receipt_updated",
          body: new TextEncoder().encode("unreachable-follow-up"),
        },
      };

      const result = auditEvent.verifyChain([lastRecord, followUpRecord]);

      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.code).toBe("lsn_threw");
        expect(localLsn(result.brokenAtSeqNo)).toBe(Number.MAX_SAFE_INTEGER);
      }
    } finally {
      vi.doUnmock("../src/event-lsn.ts");
      vi.resetModules();
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

  // Cross-language verifiers consume these golden vectors. If any of these
  // values change, the wire protocol changed — coordinate with downstream
  // verifier authors. The data lives in testdata so non-TS verifiers can load
  // the same fixture this test reads.
  describe("golden vectors (cross-language verifier contract)", () => {
    it('GENESIS_PREV_HASH is sha256("wuphf:audit:genesis:v1") in lower-hex', () => {
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

function recordAt(chain: readonly AuditEventRecord[], index: number): AuditEventRecord {
  const record = chain[index];
  if (record === undefined) {
    throw new Error(`missing audit record at ${index}`);
  }
  return record;
}

function previousComputeEventHash(recordBytes: Uint8Array): Sha256Hex {
  const buf = new Uint8Array(GENESIS_PREV_HASH.length + recordBytes.length);
  buf.set(new TextEncoder().encode(GENESIS_PREV_HASH), 0);
  buf.set(recordBytes, GENESIS_PREV_HASH.length);
  return sha256Hex(buf);
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
