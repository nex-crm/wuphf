import { describe, expect, it } from "vitest";
import { canonicalJSON } from "../src/canonical-json.ts";
import { FrozenArgs } from "../src/frozen-args.ts";

describe("canonicalJSON", () => {
  it("serializes objects and arrays with stable JCS ordering", () => {
    expect(canonicalJSON({ b: 2, a: [true, null, "x"] })).toBe('{"a":[true,null,"x"],"b":2}');
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

  it("rejects prototype-pollution keys at the JCS boundary", () => {
    for (const key of ["__proto__", "constructor", "prototype"]) {
      const input = JSON.parse(`{"${key}":{"x":1},"ok":1}`);

      expect(() => canonicalJSON(input)).toThrow(new RegExp(`forbidden key.*${key}`));
      expect(() => FrozenArgs.freeze(input)).toThrow(new RegExp(`forbidden key.*${key}`));
    }
  });
});
