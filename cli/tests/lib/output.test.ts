import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { formatOutput } from "../../src/lib/output.js";

describe("formatOutput", () => {
  describe("json format", () => {
    it("formats objects as pretty JSON", () => {
      const result = formatOutput({ a: 1, b: "hello" }, "json");
      assert.equal(result, JSON.stringify({ a: 1, b: "hello" }, null, 2));
    });

    it("formats arrays as pretty JSON", () => {
      const result = formatOutput([1, 2, 3], "json");
      assert.equal(result, JSON.stringify([1, 2, 3], null, 2));
    });

    it("formats primitives as JSON", () => {
      assert.equal(formatOutput("hello", "json"), '"hello"');
      assert.equal(formatOutput(42, "json"), "42");
    });
  });

  describe("text format", () => {
    it("renders strings directly", () => {
      assert.equal(formatOutput("hello world", "text"), "hello world");
    });

    it("renders objects as key-value pairs", () => {
      const result = formatOutput({ name: "Nex", version: 1 }, "text")!;
      assert.ok(result.includes("name: Nex"));
      assert.ok(result.includes("version: 1"));
    });

    it("renders arrays with indices", () => {
      const result = formatOutput(["a", "b"], "text")!;
      assert.ok(result.includes("[0]"));
      assert.ok(result.includes("[1]"));
    });

    it("renders nested objects with indentation", () => {
      const result = formatOutput({ outer: { inner: "val" } }, "text")!;
      assert.ok(result.includes("outer:"));
      assert.ok(result.includes("inner: val"));
    });

    it("returns (empty) for empty arrays", () => {
      const result = formatOutput([], "text")!;
      assert.ok(result.includes("(empty)"));
    });

    it("returns empty string for null/undefined", () => {
      assert.equal(formatOutput(null, "text"), "");
      assert.equal(formatOutput(undefined, "text"), "");
    });
  });

  describe("quiet format", () => {
    it("returns undefined", () => {
      assert.equal(formatOutput({ a: 1 }, "quiet"), undefined);
      assert.equal(formatOutput("hello", "quiet"), undefined);
    });
  });
});
