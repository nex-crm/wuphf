import fc from "fast-check";
import { describe, expect, it } from "vitest";
import {
  type AuditEventRecord,
  type AuditSeqNo,
  computeEventHash,
  GENESIS_PREV_HASH,
  verifyChain,
} from "../src/audit-event.ts";
import { canonicalJSON } from "../src/canonical-json.ts";
import type { Sha256Hex } from "../src/sha256.ts";

function serialize(r: AuditEventRecord): Uint8Array {
  // Canonical projection minus eventHash (the field being computed).
  const projection = {
    seqNo: r.seqNo,
    timestamp: r.timestamp.toISOString(),
    prevHash: r.prevHash,
    payload: {
      kind: r.payload.kind,
      receiptId: r.payload.receiptId ?? null,
      bodyB64: Buffer.from(r.payload.body).toString("base64"),
    },
  };
  return new TextEncoder().encode(canonicalJSON(projection));
}

function chainOfLength(n: number): AuditEventRecord[] {
  const out: AuditEventRecord[] = [];
  let prev: Sha256Hex = GENESIS_PREV_HASH;
  for (let i = 0; i < n; i++) {
    const partial: AuditEventRecord = {
      seqNo: i as AuditSeqNo,
      timestamp: new Date(2026, 4, 8, 0, 0, i),
      prevHash: prev,
      eventHash: GENESIS_PREV_HASH, // placeholder; recomputed below
      payload: {
        kind: "receipt_created",
        body: new TextEncoder().encode(`event-${i}`),
      },
    };
    const eventHash = computeEventHash(prev, serialize(partial));
    const finalRecord: AuditEventRecord = { ...partial, eventHash };
    out.push(finalRecord);
    prev = eventHash;
  }
  return out;
}

describe("audit-event chain verification", () => {
  it("verifies an empty chain", () => {
    const result = verifyChain([], serialize);
    expect(result.ok).toBe(true);
  });

  it("verifies a single-record chain", () => {
    const chain = chainOfLength(1);
    const result = verifyChain(chain, serialize);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.lastSeqNo).toBe(0);
    }
  });

  it("verifies a 100-record chain", () => {
    const chain = chainOfLength(100);
    const result = verifyChain(chain, serialize);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.lastSeqNo).toBe(99);
    }
  });

  it("rejects a chain with a tampered eventHash", () => {
    const chain = chainOfLength(5);
    chain[2] = { ...recordAt(chain, 2), eventHash: GENESIS_PREV_HASH };
    const result = verifyChain(chain, serialize);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.brokenAtSeqNo).toBe(2);
      expect(result.reason).toMatch(/event_hash mismatch/);
    }
  });

  it("rejects a chain with a tampered prevHash", () => {
    const chain = chainOfLength(5);
    chain[3] = { ...recordAt(chain, 3), prevHash: GENESIS_PREV_HASH };
    const result = verifyChain(chain, serialize);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.brokenAtSeqNo).toBe(3);
      expect(result.reason).toMatch(/prev_hash mismatch/);
    }
  });

  it("rejects a chain with a missing record (gap)", () => {
    const chain = chainOfLength(5);
    chain.splice(2, 1); // remove seq 2
    const result = verifyChain(chain, serialize);
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.brokenAtSeqNo).toBe(3);
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
    const result = verifyChain(chain, serialize);
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
          // Tamper by mutating eventHash to a different valid sha256 value.
          chain[k] = {
            ...recordAt(chain, k),
            eventHash: computeEventHash(GENESIS_PREV_HASH, new TextEncoder().encode("tamper")),
          };
          const result = verifyChain(chain, serialize);
          if (result.ok) return false;
          // Verification must fail at or before position k (subsequent prevHash
          // mismatches will also surface).
          return (result.brokenAtSeqNo as number) <= k;
        },
      ),
      { numRuns: 200 },
    );
  });
});

function recordAt(chain: readonly AuditEventRecord[], index: number): AuditEventRecord {
  const record = chain[index];
  if (record === undefined) {
    throw new Error(`missing audit record at ${index}`);
  }
  return record;
}
