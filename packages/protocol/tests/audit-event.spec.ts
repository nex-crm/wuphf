import fc from "fast-check";
import { describe, expect, it } from "vitest";
import {
  type AuditEventRecord,
  computeAuditEventHash,
  computeEventHash,
  GENESIS_PREV_HASH,
  serializeAuditEventRecordForHash,
  verifyChain,
} from "../src/audit-event.ts";
import { type EventLsn, lsnFromV1Number, parseLsn } from "../src/event-lsn.ts";
import type { Sha256Hex } from "../src/sha256.ts";

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

  it("rejects a chain with a tampered eventHash", () => {
    const chain = chainOfLength(5);
    chain[2] = { ...recordAt(chain, 2), eventHash: GENESIS_PREV_HASH };
    const result = verifyChain(chain);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(localLsn(result.brokenAtSeqNo)).toBe(2);
      expect(result.reason).toMatch(/event_hash mismatch/);
    }
  });

  it("rejects a chain with a tampered prevHash", () => {
    const chain = chainOfLength(5);
    chain[3] = { ...recordAt(chain, 3), prevHash: GENESIS_PREV_HASH };
    const result = verifyChain(chain);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(localLsn(result.brokenAtSeqNo)).toBe(3);
      expect(result.reason).toMatch(/prev_hash mismatch/);
    }
  });

  it("rejects a chain with a missing record (gap)", () => {
    const chain = chainOfLength(5);
    chain.splice(2, 1); // remove seq 2
    const result = verifyChain(chain);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(localLsn(result.brokenAtSeqNo)).toBe(3);
      expect(result.reason).toMatch(/seq_no gap/);
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
  // verifier authors.
  describe("golden vectors (cross-language verifier contract)", () => {
    it('GENESIS_PREV_HASH is sha256("wuphf:audit:genesis:v1") in lower-hex', () => {
      expect(GENESIS_PREV_HASH).toBe(
        "69292e708cc2e023492933025af465b063bdf24002e72a825b5170541b733f71",
      );
    });

    it("seqNo=v1:0 record produces a stable canonical serialization AND eventHash", () => {
      const record: AuditEventRecord = {
        seqNo: lsnFromV1Number(0),
        timestamp: new Date("2026-05-08T00:00:00.000Z"),
        prevHash: GENESIS_PREV_HASH,
        eventHash: GENESIS_PREV_HASH,
        payload: {
          kind: "boot_marker",
          body: new TextEncoder().encode("boot"),
        },
      };
      const bytes = serializeAuditEventRecordForHash(record);
      // EventLsn is wire-encoded as a JSON string ("v1:0"), not a number.
      // This commits the multi-instance extension path (v2: "v2:<id>:<n>")
      // without a future hash-chain break.
      const expectedSerialization =
        '{"payload":{"bodyB64":"Ym9vdA==","kind":"boot_marker","receiptId":null},' +
        `"prevHash":"${GENESIS_PREV_HASH}",` +
        '"seqNo":"v1:0",' +
        '"timestamp":"2026-05-08T00:00:00.000Z"}';
      expect(new TextDecoder().decode(bytes)).toBe(expectedSerialization);

      // Golden eventHash. Locks both pieces of the wire contract that the
      // serialization vector alone leaves open:
      //   1. prevHash is mixed in as 64-byte ASCII lower-hex, NOT 32 raw bytes.
      //   2. The mix order is `asciiLowerHex(prevHash) || jcsBytes(record)`
      //      (no separator).
      // A future implementation that switched to raw-byte mixing or changed
      // the order would preserve the serialization vector but break this
      // hash. Cross-language verifiers MUST reproduce this digit-for-digit.
      expect(computeAuditEventHash(record)).toBe(
        "e27134d1b1641fb13747d9fac78aecc90d9d1385d04bfeea4a8a596fdb6101bb",
      );
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
