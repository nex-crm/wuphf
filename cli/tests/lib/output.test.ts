import { describe, test, expect } from "bun:test";
import { formatOutput } from "../../src/lib/output.ts";

describe("formatOutput", () => {
  describe("json format", () => {
    test("formats objects as pretty JSON", () => {
      const result = formatOutput({ a: 1, b: "hello" }, "json");
      expect(result).toBe(JSON.stringify({ a: 1, b: "hello" }, null, 2));
    });

    test("formats arrays as pretty JSON", () => {
      const result = formatOutput([1, 2, 3], "json");
      expect(result).toBe(JSON.stringify([1, 2, 3], null, 2));
    });

    test("formats primitives as JSON", () => {
      expect(formatOutput("hello", "json")).toBe('"hello"');
      expect(formatOutput(42, "json")).toBe("42");
    });
  });

  describe("text format", () => {
    test("renders strings directly", () => {
      expect(formatOutput("hello world", "text")).toBe("hello world");
    });

    test("renders objects as key-value pairs", () => {
      const result = formatOutput({ name: "WUPHF", version: 1 }, "text")!;
      expect(result.includes("name: WUPHF")).toBeTruthy();
      expect(result.includes("version: 1")).toBeTruthy();
    });

    test("renders arrays with indices", () => {
      const result = formatOutput(["a", "b"], "text")!;
      expect(result.includes("[0]")).toBeTruthy();
      expect(result.includes("[1]")).toBeTruthy();
    });

    test("renders nested objects with indentation", () => {
      const result = formatOutput({ outer: { inner: "val" } }, "text")!;
      expect(result.includes("outer:")).toBeTruthy();
      expect(result.includes("inner: val")).toBeTruthy();
    });

    test("returns (empty) for empty arrays", () => {
      const result = formatOutput([], "text")!;
      expect(result.includes("(empty)")).toBeTruthy();
    });

    test("returns empty string for null/undefined", () => {
      expect(formatOutput(null, "text")).toBe("");
      expect(formatOutput(undefined, "text")).toBe("");
    });
  });

  describe("quiet format", () => {
    test("returns undefined", () => {
      expect(formatOutput({ a: 1 }, "quiet")).toBe(undefined);
      expect(formatOutput("hello", "quiet")).toBe(undefined);
    });
  });
});
