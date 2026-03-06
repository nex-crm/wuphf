import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { formatNexContext, stripNexContext } from "../../src/lib/context-format.js";

describe("formatNexContext", () => {
  it("wraps content in nex-context tags", () => {
    const result = formatNexContext({ answer: "Hello world", entityCount: 0 });
    assert.ok(result.startsWith("<nex-context>"));
    assert.ok(result.endsWith("</nex-context>"));
    assert.ok(result.includes("Hello world"));
  });

  it("includes entity count when > 0", () => {
    const result = formatNexContext({ answer: "data", entityCount: 5 });
    assert.ok(result.includes("[5 related entities found]"));
  });

  it("omits entity count when 0", () => {
    const result = formatNexContext({ answer: "data", entityCount: 0 });
    assert.ok(!result.includes("related entities found"));
  });
});

describe("stripNexContext", () => {
  it("removes complete nex-context blocks", () => {
    const input = "before <nex-context>some context</nex-context> after";
    const result = stripNexContext(input);
    assert.equal(result, "before  after");
  });

  it("removes unclosed nex-context tags", () => {
    const input = "before <nex-context>unclosed context here";
    const result = stripNexContext(input);
    assert.equal(result, "before");
  });

  it("returns original text when no tags present", () => {
    const input = "just regular text";
    assert.equal(stripNexContext(input), "just regular text");
  });

  it("handles multiple blocks", () => {
    const input = "a <nex-context>x</nex-context> b <nex-context>y</nex-context> c";
    const result = stripNexContext(input);
    assert.equal(result, "a  b  c");
  });
});
