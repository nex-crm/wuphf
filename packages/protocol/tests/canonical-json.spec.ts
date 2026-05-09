import { describe, expect, it } from "vitest";
import { MAX_CANONICAL_JSON_NODES } from "../src/budgets.ts";
import { canonicalJSON } from "../src/canonical-json.ts";
import { FrozenArgs } from "../src/frozen-args.ts";

describe("canonicalJSON", () => {
  it.each([
    { input: null, expected: "null" },
    { input: true, expected: "true" },
    { input: false, expected: "false" },
    { input: 12.5, expected: "12.5" },
    { input: {}, expected: "{}" },
    { input: [], expected: "[]" },
  ])("serializes top-level JCS value $expected", ({ input, expected }) => {
    expect(canonicalJSON(input)).toBe(expected);
  });

  it("serializes objects and arrays with stable JCS ordering", () => {
    expect(canonicalJSON({ b: 2, a: [true, null, "x"] })).toBe('{"a":[true,null,"x"],"b":2}');
  });

  it("accepts canonical JSON inputs exactly at the node budget", () => {
    const atCap = new Array<null>(MAX_CANONICAL_JSON_NODES - 1).fill(null);

    expect(() => canonicalJSON(atCap)).not.toThrow();
  });

  it("rejects canonical JSON inputs one node over budget with path and count", () => {
    const overCap = new Array<null>(MAX_CANONICAL_JSON_NODES).fill(null);

    expect(() => canonicalJSON(overCap)).toThrow(
      `canonicalJSON node count at $[${MAX_CANONICAL_JSON_NODES - 1}] exceeds budget: ${
        MAX_CANONICAL_JSON_NODES + 1
      } > ${MAX_CANONICAL_JSON_NODES}`,
    );
  });

  it.each([
    { name: "undefined", input: undefined, message: /undefined/ },
    { name: "function", input: () => 1, message: /function/ },
    { name: "symbol", input: Symbol("x"), message: /symbol/ },
    { name: "bigint", input: 1n, message: /bigint/ },
    { name: "NaN", input: Number.NaN, message: /non-finite number/ },
    { name: "positive infinity", input: Number.POSITIVE_INFINITY, message: /non-finite number/ },
    { name: "negative infinity", input: Number.NEGATIVE_INFINITY, message: /non-finite number/ },
  ])("rejects top-level non-JCS value: $name", ({ input, message }) => {
    expect(() => canonicalJSON(input)).toThrow(message);
  });

  it("rejects array accessor indices without invoking getters", () => {
    const arr: unknown[] = [];
    let getterInvoked = false;
    Object.defineProperty(arr, "0", {
      enumerable: true,
      get() {
        getterInvoked = true;
        return "x";
      },
    });

    expect(() => canonicalJSON(arr)).toThrow(/accessor property at \$\[0\]/);
    expect(getterInvoked).toBe(false);
  });

  it("rejects non-index own properties on arrays", () => {
    const arr: unknown[] = ["x"];
    Object.defineProperty(arr, "extra", { value: "y" });

    expect(() => canonicalJSON(arr)).toThrow(/non-array-index own property at \$\.extra/);
  });

  it("rejects noncanonical array index spellings", () => {
    const arr: unknown[] = [];
    Object.defineProperty(arr, "01", { value: "x", enumerable: true });

    expect(() => canonicalJSON(arr)).toThrow(/non-array-index own property at \$\.01/);
  });

  it("rejects sparse arrays with a boundary hole", () => {
    const arr: unknown[] = [];
    arr.length = 1;

    expect(() => canonicalJSON(arr)).toThrow(/sparse array hole at \$\[0\]/);
  });

  it("rejects array own properties at the uint32 index boundary", () => {
    const arr: unknown[] = [];
    Object.defineProperty(arr, "4294967295", { value: "x", enumerable: true });

    expect(() => canonicalJSON(arr)).toThrow(/non-array-index own property at \$\.4294967295/);
  });

  it("rejects non-enumerable array indices", () => {
    const arr: unknown[] = ["x"];
    Object.defineProperty(arr, "0", { value: "x", enumerable: false });

    expect(() => canonicalJSON(arr)).toThrow(/non-enumerable own property at \$\[0\]/);
  });

  it("rejects symbol keys on arrays", () => {
    const arr: unknown[] = ["x"];
    Object.defineProperty(arr, Symbol("x"), { value: "y", enumerable: true });

    expect(() => canonicalJSON(arr)).toThrow(/symbol keys are not representable/);
  });

  it("rejects non-JCS array elements", () => {
    expect(() => canonicalJSON([() => 1])).toThrow(/function at \$\[0\]/);
  });

  it("rejects inherited Object.prototype.toJSON before serialization", () => {
    withPrototypeToJson(Object.prototype, () => {
      expect(() => canonicalJSON({ ok: 1 })).toThrow(/inherited toJSON method at \$/);
    });
  });

  it("rejects inherited Array.prototype.toJSON before serialization", () => {
    withPrototypeToJson(Array.prototype, () => {
      expect(() => canonicalJSON(["ok"])).toThrow(/inherited toJSON method at \$/);
    });
  });

  it("rejects inherited toJSON accessors without invoking them", () => {
    let getterInvoked = false;
    withPrototypeToJsonDescriptor(
      Object.prototype,
      {
        configurable: true,
        get() {
          getterInvoked = true;
          return () => ({ polluted: true });
        },
      },
      () => {
        expect(() => canonicalJSON({ ok: 1 })).toThrow(/inherited accessor toJSON at \$/);
      },
    );

    expect(getterInvoked).toBe(false);
  });

  it("rejects prototype-pollution keys at the JCS boundary", () => {
    for (const key of ["__proto__", "constructor", "prototype"]) {
      const input = JSON.parse(`{"${key}":{"x":1},"ok":1}`);

      expect(() => canonicalJSON(input)).toThrow(new RegExp(`forbidden key.*${key}`));
      expect(() => FrozenArgs.freeze(input)).toThrow(new RegExp(`forbidden key.*${key}`));
    }
  });

  it("rejects prototype-pollution accessors without invoking them", () => {
    for (const key of ["__proto__", "constructor"]) {
      const input: Record<string, unknown> = Object.create(null) as Record<string, unknown>;
      let getterInvoked = false;
      Object.defineProperty(input, key, {
        enumerable: true,
        get() {
          getterInvoked = true;
          return { polluted: true };
        },
      });

      expect(() => canonicalJSON(input)).toThrow(new RegExp(`forbidden key.*${key}`));
      expect(getterInvoked).toBe(false);
    }
  });

  it("rejects object accessors without invoking them", () => {
    let getterInvoked = false;
    const input: Record<string, unknown> = {};
    Object.defineProperty(input, "x", {
      enumerable: true,
      get() {
        getterInvoked = true;
        return 1;
      },
    });

    expect(() => canonicalJSON(input)).toThrow(/accessor property at \$\.x/);
    expect(getterInvoked).toBe(false);
  });

  it("rejects non-enumerable object properties", () => {
    const input: Record<string, unknown> = {};
    Object.defineProperty(input, "x", { value: 1, enumerable: false });

    expect(() => canonicalJSON(input)).toThrow(/non-enumerable own property at \$\.x/);
  });

  it("rejects symbol keys on objects", () => {
    const input: Record<string | symbol, unknown> = { x: 1 };
    input[Symbol("x")] = 2;

    expect(() => canonicalJSON(input)).toThrow(/symbol keys are not representable/);
  });

  it("rejects non-plain objects with descriptive prototypes", () => {
    expect(() => canonicalJSON(new Date(0))).toThrow(/non-plain object at \$ \(got Date\)/);
  });

  it("rejects non-plain objects with anonymous prototypes", () => {
    const proto = Object.create(null) as object;
    const input = Object.create(proto) as object;

    expect(() => canonicalJSON(input)).toThrow(/non-plain object at \$ \(got non-plain\)/);
  });

  it("rejects lone surrogates in object keys", () => {
    for (const key of ["\ud800", "\ud800x", "\udc00"]) {
      expect(() => canonicalJSON({ [key]: 1 })).toThrow(/lone (high|low) surrogate/);
    }
  });

  it("accepts valid surrogate pairs in object keys", () => {
    expect(() => canonicalJSON({ "𝄞": 1 })).not.toThrow();
  });

  it("rejects max-depth recursion before serialization", () => {
    let nested: unknown = "leaf";
    for (let i = 0; i < 65; i++) {
      nested = { next: nested };
    }

    expect(() => canonicalJSON(nested)).toThrow(/max recursion depth exceeded/);
  });
});

function withPrototypeToJson(prototype: object, run: () => void): void {
  withPrototypeToJsonDescriptor(
    prototype,
    {
      configurable: true,
      value() {
        return { polluted: true };
      },
    },
    run,
  );
}

function withPrototypeToJsonDescriptor(
  prototype: object,
  descriptor: PropertyDescriptor,
  run: () => void,
): void {
  const original = Object.getOwnPropertyDescriptor(prototype, "toJSON");
  try {
    Object.defineProperty(prototype, "toJSON", descriptor);
    run();
  } finally {
    if (original === undefined) {
      Reflect.deleteProperty(prototype, "toJSON");
    } else {
      Object.defineProperty(prototype, "toJSON", original);
    }
  }
}
