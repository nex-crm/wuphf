import * as fc from "fast-check";
import { describe, expect, it } from "vitest";
import { canonicalJSON } from "../src/canonical-json.ts";
import { FrozenArgs } from "../src/frozen-args.ts";

type JsonObject = { [key: string]: JsonValue };
type JsonValue = null | boolean | number | string | JsonValue[] | JsonObject;

const jsonKey = fc.fullUnicodeString({ maxLength: 16 }).filter((key) => key !== "__proto__");

const jsonNumber = fc
  .double({
    min: -1_000_000_000,
    max: 1_000_000_000,
    noNaN: true,
  })
  .filter((value) => Number.isFinite(value) && !Object.is(value, -0));

const { jsonValue, jsonObject } = fc.letrec<{
  jsonArray: JsonValue[];
  jsonObject: JsonObject;
  jsonValue: JsonValue;
}>((tie) => ({
  jsonArray: fc.array(tie("jsonValue"), { maxLength: 6 }),
  jsonObject: fc.dictionary(jsonKey, tie("jsonValue"), { maxKeys: 6 }),
  jsonValue: fc.oneof(
    { depthSize: "small" },
    fc.constant(null),
    fc.boolean(),
    jsonNumber,
    fc.fullUnicodeString({ maxLength: 64 }),
    tie("jsonArray"),
    tie("jsonObject"),
  ),
}));

function objectFromEntries(entries: [string, JsonValue][]): JsonObject {
  const output: JsonObject = {};
  for (const [key, value] of entries) {
    output[key] = value;
  }
  return output;
}

function shuffledObject(input: JsonObject): JsonObject {
  return objectFromEntries(
    Object.entries(input).sort(([left], [right]) => {
      if (left < right) {
        return 1;
      }
      if (left > right) {
        return -1;
      }
      return 0;
    }),
  );
}

describe("FrozenArgs", () => {
  it("freezes identity-stable hashes", () => {
    fc.assert(
      fc.property(jsonValue, (input) => {
        expect(FrozenArgs.freeze(input).hash).toBe(FrozenArgs.freeze(input).hash);
      }),
      { numRuns: 5000 },
    );
  });

  it("is independent of object key insertion order", () => {
    fc.assert(
      fc.property(jsonObject, (input) => {
        expect(FrozenArgs.freeze(shuffledObject(input)).hash).toBe(FrozenArgs.freeze(input).hash);
      }),
      { numRuns: 5000 },
    );
  });

  it("rejects runtime mutation of frozen instances", () => {
    fc.assert(
      fc.property(jsonValue, (input) => {
        const frozen = FrozenArgs.freeze(input);
        const canonicalJson = frozen.canonicalJson;
        const hash = frozen.hash;
        const mutable = frozen as {
          canonicalJson: string;
          extra?: string;
          hash: string;
        };

        expect(() => {
          mutable.canonicalJson = '"tampered"';
        }).toThrow(TypeError);
        expect(() => {
          mutable.hash = "0".repeat(64);
        }).toThrow(TypeError);
        expect(() => {
          mutable.extra = "tampered";
        }).toThrow(TypeError);

        expect(frozen.canonicalJson).toBe(canonicalJson);
        expect(frozen.hash).toBe(hash);
        expect("extra" in frozen).toBe(false);
        expect(Object.isFrozen(frozen)).toBe(true);
      }),
    );
  });

  it("preserves hash across JCS round-trips", () => {
    fc.assert(
      fc.property(jsonValue, (input) => {
        const frozen = FrozenArgs.freeze(input);
        const parsed: unknown = JSON.parse(frozen.canonicalJson);
        expect(FrozenArgs.freeze(parsed).hash).toBe(frozen.hash);
      }),
      { numRuns: 5000 },
    );
  });

  it("produces unequal hashes for differing canonical inputs", () => {
    fc.assert(
      fc.property(
        fc
          .tuple(jsonValue, jsonValue)
          .filter(([left, right]) => canonicalJSON(left) !== canonicalJSON(right)),
        ([left, right]) => {
          expect(FrozenArgs.freeze(left).hash).not.toBe(FrozenArgs.freeze(right).hash);
        },
      ),
    );
  });

  it("rejects undefined and non-finite numbers", () => {
    expect(() => FrozenArgs.freeze(undefined)).toThrow(/undefined/);

    fc.assert(
      fc.property(
        fc.constantFrom(Number.NaN, Number.POSITIVE_INFINITY, Number.NEGATIVE_INFINITY),
        (value) => {
          expect(() => FrozenArgs.freeze(value)).toThrow(/non-finite/);
          expect(() => FrozenArgs.freeze({ value })).toThrow(/non-finite/);
          expect(() => FrozenArgs.freeze([value])).toThrow(/non-finite/);
        },
      ),
    );
  });

  it("rejects functions, symbols, bigints", () => {
    expect(() => FrozenArgs.freeze(() => 1)).toThrow(/function/);
    expect(() => FrozenArgs.freeze({ fn: () => 1 })).toThrow(/function/);
    expect(() => FrozenArgs.freeze(Symbol("x"))).toThrow(/symbol/);
    expect(() => FrozenArgs.freeze({ s: Symbol("x") })).toThrow(/symbol/);
    expect(() => FrozenArgs.freeze(1n)).toThrow(/bigint/);
    expect(() => FrozenArgs.freeze({ b: 1n })).toThrow(/bigint/);
  });

  it("rejects sparse array holes", () => {
    // biome-ignore lint/suspicious/noSparseArray: deliberate hole for the test
    const sparse = [1, , 2];
    expect(() => FrozenArgs.freeze(sparse)).toThrow(/sparse/);
  });

  it("rejects non-plain objects", () => {
    expect(() => FrozenArgs.freeze(new Map())).toThrow(/non-plain/);
    expect(() => FrozenArgs.freeze(new Set())).toThrow(/non-plain/);
    expect(() => FrozenArgs.freeze(new Date())).toThrow(/non-plain/);
    expect(() => FrozenArgs.freeze({ d: new Date() })).toThrow(/non-plain/);
  });

  it("accepts Object.create(null) as plain", () => {
    const plain: { x?: number } = Object.create(null);
    plain.x = 1;
    expect(() => FrozenArgs.freeze(plain)).not.toThrow();
  });

  it("rejects accessor properties", () => {
    const obj = {} as Record<string, unknown>;
    Object.defineProperty(obj, "x", { get: () => 1, enumerable: true });
    expect(() => FrozenArgs.freeze(obj)).toThrow(/accessor/);
  });

  it("rejects non-enumerable own properties", () => {
    const obj = {} as Record<string, unknown>;
    Object.defineProperty(obj, "x", { value: 1, enumerable: false });
    expect(() => FrozenArgs.freeze(obj)).toThrow(/non-enumerable/);
  });

  it("rejects symbol keys", () => {
    const sym = Symbol("k");
    const obj: Record<string | symbol, unknown> = {};
    obj[sym] = 1;
    expect(() => FrozenArgs.freeze(obj)).toThrow(/symbol keys/);
  });

  it("rejects strings containing lone surrogates", () => {
    expect(() => FrozenArgs.freeze("\ud800")).toThrow(/surrogate/);
    expect(() => FrozenArgs.freeze({ x: "\udc00" })).toThrow(/surrogate/);
  });
});
