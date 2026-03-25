import { describe, test, expect } from "bun:test";
import { formatNexContext, stripNexContext } from "../../src/lib/context-format.ts";

describe("formatNexContext", () => {
  test("wraps content in wuphf-context tags", () => {
    const result = formatNexContext({ answer: "Hello world", entityCount: 0 });
    expect(result.startsWith("<wuphf-context>")).toBeTruthy();
    expect(result.endsWith("</wuphf-context>")).toBeTruthy();
    expect(result.includes("Hello world")).toBeTruthy();
  });

  test("includes entity count when > 0", () => {
    const result = formatNexContext({ answer: "data", entityCount: 5 });
    expect(result.includes("[5 related entities found]")).toBeTruthy();
  });

  test("omits entity count when 0", () => {
    const result = formatNexContext({ answer: "data", entityCount: 0 });
    expect(!result.includes("related entities found")).toBeTruthy();
  });
});

describe("stripNexContext", () => {
  test("removes complete wuphf-context blocks", () => {
    const input = "before <wuphf-context>some context</wuphf-context> after";
    const result = stripNexContext(input);
    expect(result).toBe("before  after");
  });

  test("removes unclosed wuphf-context tags", () => {
    const input = "before <wuphf-context>unclosed context here";
    const result = stripNexContext(input);
    expect(result).toBe("before");
  });

  test("returns original text when no tags present", () => {
    const input = "just regular text";
    expect(stripNexContext(input)).toBe("just regular text");
  });

  test("handles multiple blocks", () => {
    const input = "a <wuphf-context>x</wuphf-context> b <wuphf-context>y</wuphf-context> c";
    const result = stripNexContext(input);
    expect(result).toBe("a  b  c");
  });
});
