import { describe, test, expect } from "bun:test";
import { captureFilter } from "../../src/lib/capture-filter.ts";

describe("captureFilter", () => {
  test("skips empty text", () => {
    const result = captureFilter("");
    expect(result.skipped).toBe(true);
    if (result.skipped) expect(result.reason.includes("empty")).toBeTruthy();
  });

  test("skips whitespace-only text", () => {
    const result = captureFilter("   \n\t  ");
    expect(result.skipped).toBe(true);
  });

  test("skips too-short text", () => {
    const result = captureFilter("short");
    expect(result.skipped).toBe(true);
    if (result.skipped) expect(result.reason.includes("too short")).toBeTruthy();
  });

  test("skips too-long text", () => {
    const longText = "a".repeat(50_001);
    const result = captureFilter(longText);
    expect(result.skipped).toBe(true);
    if (result.skipped) expect(result.reason.includes("too long")).toBeTruthy();
  });

  test("passes normal-length text", () => {
    const text = "This is a normal prompt that should pass the filter easily.";
    const result = captureFilter(text);
    expect(result.skipped).toBe(false);
    if (!result.skipped) expect(result.text).toBe(text);
  });

  test("strips <nex-context> blocks before length check", () => {
    const nexBlock = "<nex-context>some long context data here</nex-context>";
    const shortContent = "hi";
    const result = captureFilter(shortContent + nexBlock);
    // After stripping, "hi" is too short
    expect(result.skipped).toBe(true);
  });

  test("returns cleaned text without nex-context blocks", () => {
    const nexBlock = "<nex-context>context data goes here</nex-context>";
    const content = "This is a sufficiently long prompt for testing purposes.";
    const result = captureFilter(content + nexBlock);
    expect(result.skipped).toBe(false);
    if (!result.skipped) {
      expect(!result.text.includes("<nex-context>")).toBeTruthy();
      expect(result.text.includes("sufficiently long")).toBeTruthy();
    }
  });
});
