import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { captureFilter } from "../../src/lib/capture-filter.js";

describe("captureFilter", () => {
  it("skips empty text", () => {
    const result = captureFilter("");
    assert.equal(result.skipped, true);
    if (result.skipped) assert.ok(result.reason.includes("empty"));
  });

  it("skips whitespace-only text", () => {
    const result = captureFilter("   \n\t  ");
    assert.equal(result.skipped, true);
  });

  it("skips too-short text", () => {
    const result = captureFilter("short");
    assert.equal(result.skipped, true);
    if (result.skipped) assert.ok(result.reason.includes("too short"));
  });

  it("skips too-long text", () => {
    const longText = "a".repeat(50_001);
    const result = captureFilter(longText);
    assert.equal(result.skipped, true);
    if (result.skipped) assert.ok(result.reason.includes("too long"));
  });

  it("passes normal-length text", () => {
    const text = "This is a normal prompt that should pass the filter easily.";
    const result = captureFilter(text);
    assert.equal(result.skipped, false);
    if (!result.skipped) assert.equal(result.text, text);
  });

  it("strips <nex-context> blocks before length check", () => {
    const nexBlock = "<nex-context>some long context data here</nex-context>";
    const shortContent = "hi";
    const result = captureFilter(shortContent + nexBlock);
    // After stripping, "hi" is too short
    assert.equal(result.skipped, true);
  });

  it("returns cleaned text without nex-context blocks", () => {
    const nexBlock = "<nex-context>context data goes here</nex-context>";
    const content = "This is a sufficiently long prompt for testing purposes.";
    const result = captureFilter(content + nexBlock);
    assert.equal(result.skipped, false);
    if (!result.skipped) {
      assert.ok(!result.text.includes("<nex-context>"));
      assert.ok(result.text.includes("sufficiently long"));
    }
  });
});
