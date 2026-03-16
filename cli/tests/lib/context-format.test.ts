import { describe, test, expect } from "bun:test";
import { formatNexContext, stripNexContext } from "../../src/lib/context-format.ts";

describe("formatNexContext", () => {
  test("wraps content in nex-context tags", () => {
    const result = formatNexContext({ answer: "Hello world", entityCount: 0 });
    expect(result.startsWith("<nex-context>")).toBeTruthy();
    expect(result.endsWith("</nex-context>")).toBeTruthy();
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
  test("removes complete nex-context blocks", () => {
    const input = "before <nex-context>some context</nex-context> after";
    const result = stripNexContext(input);
    expect(result).toBe("before  after");
  });

  test("removes unclosed nex-context tags", () => {
    const input = "before <nex-context>unclosed context here";
    const result = stripNexContext(input);
    expect(result).toBe("before");
  });

  test("returns original text when no tags present", () => {
    const input = "just regular text";
    expect(stripNexContext(input)).toBe("just regular text");
  });

  test("handles multiple blocks", () => {
    const input = "a <nex-context>x</nex-context> b <nex-context>y</nex-context> c";
    const result = stripNexContext(input);
    expect(result).toBe("a  b  c");
  });
});
