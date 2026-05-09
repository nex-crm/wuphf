import fc from "fast-check";
import { describe, expect, it } from "vitest";
import {
  compareLsn,
  type EventLsn,
  GENESIS_LSN,
  isAfter,
  isBefore,
  isEqualLsn,
  lsnFromV1Number,
  nextLsn,
  parseLsn,
} from "../src/event-lsn.ts";
import { MAX_EVENT_LSN_BYTES } from "../src/index.ts";

describe("event-lsn", () => {
  describe("lsnFromV1Number", () => {
    it("encodes 0 as v1:0", () => {
      expect(lsnFromV1Number(0) as string).toBe("v1:0");
    });

    it("encodes positive integers", () => {
      expect(lsnFromV1Number(1) as string).toBe("v1:1");
      expect(lsnFromV1Number(42) as string).toBe("v1:42");
      expect(lsnFromV1Number(Number.MAX_SAFE_INTEGER) as string).toBe(
        `v1:${Number.MAX_SAFE_INTEGER}`,
      );
    });

    it("rejects negatives", () => {
      expect(() => lsnFromV1Number(-1)).toThrow(/non-negative safe integer/);
    });

    it("rejects non-integers", () => {
      expect(() => lsnFromV1Number(1.5)).toThrow(/non-negative safe integer/);
      expect(() => lsnFromV1Number(Number.NaN)).toThrow(/non-negative safe integer/);
      expect(() => lsnFromV1Number(Number.POSITIVE_INFINITY)).toThrow(/non-negative safe integer/);
    });

    it("rejects unsafe integers (mintability matches parseLsn's safe-integer guard)", () => {
      // Without this guard, the appender could mint a token via
      // `nextLsn(MAX_SAFE_INTEGER)` that parseLsn then rejects — verifier
      // and writer would disagree about which LSNs are valid.
      expect(() => lsnFromV1Number(Number.MAX_SAFE_INTEGER + 1)).toThrow(
        /non-negative safe integer/,
      );
    });
  });

  describe("nextLsn", () => {
    it("steps to the next sequence position", () => {
      expect(nextLsn(lsnFromV1Number(0)) as string).toBe("v1:1");
      expect(nextLsn(lsnFromV1Number(99)) as string).toBe("v1:100");
    });

    it("throws on overflow rather than minting an unparseable token", () => {
      expect(() => nextLsn(lsnFromV1Number(Number.MAX_SAFE_INTEGER))).toThrow(
        /non-negative safe integer/,
      );
    });
  });

  describe("GENESIS_LSN", () => {
    it("is v1:0", () => {
      expect(GENESIS_LSN as string).toBe("v1:0");
    });

    it("equals lsnFromV1Number(0)", () => {
      expect(isEqualLsn(GENESIS_LSN, lsnFromV1Number(0))).toBe(true);
    });
  });

  describe("parseLsn", () => {
    it("parses v1 format", () => {
      expect(parseLsn(lsnFromV1Number(0))).toEqual({ format: "v1", localLsn: 0 });
      expect(parseLsn(lsnFromV1Number(123))).toEqual({ format: "v1", localLsn: 123 });
      expect(parseLsn(lsnFromV1Number(Number.MAX_SAFE_INTEGER))).toEqual({
        format: "v1",
        localLsn: Number.MAX_SAFE_INTEGER,
      });
    });

    it("rejects unknown formats", () => {
      expect(() => parseLsn("v0:0" as EventLsn)).toThrow(/unrecognized LSN format/);
      expect(() => parseLsn("0" as EventLsn)).toThrow(/unrecognized LSN format/);
      expect(() => parseLsn("" as EventLsn)).toThrow(/unrecognized LSN format/);
    });

    it("rejects malformed v1 LSNs", () => {
      expect(() => parseLsn("v1:" as EventLsn)).toThrow(/empty v1 LSN/);
      expect(() => parseLsn("v1:01" as EventLsn)).toThrow(/malformed v1 LSN/);
      expect(() => parseLsn("v1:-1" as EventLsn)).toThrow(/malformed v1 LSN/);
      expect(() => parseLsn("v1:1.5" as EventLsn)).toThrow(/malformed v1 LSN/);
      expect(() => parseLsn("v1:abc" as EventLsn)).toThrow(/malformed v1 LSN/);
      expect(() => parseLsn("v1: 1" as EventLsn)).toThrow(/malformed v1 LSN/);
    });

    it("checks the EventLsn byte budget before format parsing", () => {
      expect(() => parseLsn("x".repeat(MAX_EVENT_LSN_BYTES) as EventLsn)).toThrow(
        /unrecognized LSN format/,
      );
      expect(() => parseLsn("x".repeat(MAX_EVENT_LSN_BYTES + 1) as EventLsn)).toThrow(
        `MAX_EVENT_LSN_BYTES exceeds budget: ${MAX_EVENT_LSN_BYTES + 1} > ${MAX_EVENT_LSN_BYTES}`,
      );
      expect(() => parseLsn("\u00a2".repeat(MAX_EVENT_LSN_BYTES / 2 + 1) as EventLsn)).toThrow(
        `MAX_EVENT_LSN_BYTES exceeds budget: ${MAX_EVENT_LSN_BYTES + 1} > ${MAX_EVENT_LSN_BYTES}`,
      );
    });

    it.each(["v1:00", "v1:000", "v1:0001"])("rejects leading-zero LSN %s", (lsn) => {
      expect(() => parseLsn(lsn as EventLsn)).toThrow(/malformed v1 LSN/);
    });

    it.each(["v1", "v1/0", "v1-0", "v10"])("rejects unbracketed v1 prefix %s", (lsn) => {
      expect(() => parseLsn(lsn as EventLsn)).toThrow(/unrecognized LSN format/);
    });

    it("rejects out-of-safe-integer v1 LSNs", () => {
      expect(() => parseLsn(`v1:${Number.MAX_SAFE_INTEGER + 1}` as EventLsn)).toThrow(
        /safe-integer range/,
      );
      expect(() => parseLsn("v1:99999999999999999" as EventLsn)).toThrow(/safe-integer range/);
    });
  });

  describe("compareLsn / isAfter / isBefore / isEqualLsn", () => {
    it("orders v1 LSNs by localLsn", () => {
      const a = lsnFromV1Number(1);
      const b = lsnFromV1Number(2);
      expect(compareLsn(a, b)).toBe(-1);
      expect(compareLsn(b, a)).toBe(1);
      expect(compareLsn(a, a)).toBe(0);
    });

    it("isAfter / isBefore agree with compareLsn", () => {
      const a = lsnFromV1Number(5);
      const b = lsnFromV1Number(10);
      expect(isAfter(b, a)).toBe(true);
      expect(isAfter(a, b)).toBe(false);
      expect(isAfter(a, a)).toBe(false);
      expect(isBefore(a, b)).toBe(true);
      expect(isBefore(b, a)).toBe(false);
      expect(isBefore(a, a)).toBe(false);
    });

    it("isEqualLsn matches string equality on canonical form", () => {
      expect(isEqualLsn(lsnFromV1Number(7), lsnFromV1Number(7))).toBe(true);
      expect(isEqualLsn(lsnFromV1Number(7), lsnFromV1Number(8))).toBe(false);
    });

    it("property: ordering is total and transitive", () => {
      fc.assert(
        fc.property(
          fc.integer({ min: 0, max: 1_000_000 }),
          fc.integer({ min: 0, max: 1_000_000 }),
          fc.integer({ min: 0, max: 1_000_000 }),
          (x, y, z) => {
            const a = lsnFromV1Number(x);
            const b = lsnFromV1Number(y);
            const c = lsnFromV1Number(z);
            const cmp = compareLsn(a, b);
            const flipped = compareLsn(b, a);
            if (cmp === 0 && flipped !== 0) return false;
            if (cmp === -1 && flipped !== 1) return false;
            if (cmp === 1 && flipped !== -1) return false;
            if (compareLsn(a, b) <= 0 && compareLsn(b, c) <= 0) {
              if (compareLsn(a, c) > 0) return false;
            }
            return true;
          },
        ),
        { numRuns: 500 },
      );
    });
  });

  describe("nextLsn", () => {
    it("returns the successor", () => {
      expect(isEqualLsn(nextLsn(lsnFromV1Number(0)), lsnFromV1Number(1))).toBe(true);
      expect(isEqualLsn(nextLsn(lsnFromV1Number(99)), lsnFromV1Number(100))).toBe(true);
    });

    it("nextLsn(GENESIS_LSN) is v1:1", () => {
      expect(nextLsn(GENESIS_LSN) as string).toBe("v1:1");
    });

    it("property: nextLsn(x) > x for any non-overflow x", () => {
      fc.assert(
        fc.property(fc.integer({ min: 0, max: Number.MAX_SAFE_INTEGER - 1 }), (n) => {
          const x = lsnFromV1Number(n);
          return isAfter(nextLsn(x), x);
        }),
        { numRuns: 500 },
      );
    });
  });

  describe("round-trip", () => {
    it("property: parseLsn(lsnFromV1Number(n)).localLsn === n", () => {
      fc.assert(
        fc.property(fc.integer({ min: 0, max: Number.MAX_SAFE_INTEGER }), (n) => {
          return parseLsn(lsnFromV1Number(n)).localLsn === n;
        }),
        { numRuns: 1000 },
      );
    });
  });
});
