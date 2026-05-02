import { describe, expect, it } from "vitest";

import { keyedByOccurrence } from "./reactKeys";

describe("keyedByOccurrence", () => {
  it("keeps semantic keys unchanged when they are unique", () => {
    expect(keyedByOccurrence(["a", "b"], (value) => value)).toEqual([
      { key: "a", value: "a", index: 0 },
      { key: "b", value: "b", index: 1 },
    ]);
  });

  it("suffixes repeated semantic keys by occurrence", () => {
    expect(keyedByOccurrence(["repeat", "repeat"], (value) => value)).toEqual([
      { key: "repeat", value: "repeat", index: 0 },
      { key: "repeat#1", value: "repeat", index: 1 },
    ]);
  });

  it("avoids collisions between duplicate suffixes and literal bases", () => {
    expect(keyedByOccurrence(["x", "x", "x#1"], (value) => value)).toEqual([
      { key: "x", value: "x", index: 0 },
      { key: "x#1", value: "x", index: 1 },
      { key: "x#1#1", value: "x#1", index: 2 },
    ]);
  });
});
