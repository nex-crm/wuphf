import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { shouldRecall } from "../../src/lib/recall-filter.js";

describe("shouldRecall", () => {
  it("always recalls on first prompt", () => {
    const result = shouldRecall("anything at all", true);
    assert.equal(result.shouldRecall, true);
    assert.equal(result.reason, "first-prompt");
  });

  it("skips when prompt starts with !", () => {
    const result = shouldRecall("!do something without recall", false);
    assert.equal(result.shouldRecall, false);
    assert.equal(result.reason, "opt-out");
  });

  it("skips when prompt is too short", () => {
    const result = shouldRecall("short", false);
    assert.equal(result.shouldRecall, false);
    assert.equal(result.reason, "too-short");
  });

  it("recalls on question words", () => {
    const result = shouldRecall("What is the status of my contacts?", false);
    assert.equal(result.shouldRecall, true);
    assert.equal(result.reason, "question");
  });

  it("skips tool commands without questions", () => {
    const result = shouldRecall("run the build script for production", false);
    assert.equal(result.shouldRecall, false);
    assert.equal(result.reason, "tool-command");
  });

  it("skips code-heavy content without questions", () => {
    // Code-heavy: less than 50% alpha characters + has file references
    const result = shouldRecall("src/lib/config.ts:42 => { a: 1, b: 2 }", false);
    assert.equal(result.shouldRecall, false);
    assert.equal(result.reason, "code-prompt");
  });

  it("recalls on question even with tool command", () => {
    const result = shouldRecall("how do I run the build script?", false);
    assert.equal(result.shouldRecall, true);
    assert.equal(result.reason, "question");
  });
});
