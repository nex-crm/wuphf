import { describe, test, expect, beforeEach } from "bun:test";
import { writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import { shouldRecall } from "../../src/lib/recall-filter.ts";

const DATA_DIR = join(homedir(), ".wuphf");
const STATE_FILE = join(DATA_DIR, "recall-state.json");

describe("shouldRecall", () => {
  // Reset recall state before each test to avoid debounce interference
  beforeEach(() => {
    mkdirSync(DATA_DIR, { recursive: true });
    writeFileSync(STATE_FILE, JSON.stringify({ lastRecallAt: 0 }), "utf-8");
  });

  test("always recalls on first prompt", () => {
    const result = shouldRecall("anything at all", true);
    expect(result.shouldRecall).toBe(true);
    expect(result.reason).toBe("first-prompt");
  });

  test("skips when prompt starts with !", () => {
    const result = shouldRecall("!do something without recall", false);
    expect(result.shouldRecall).toBe(false);
    expect(result.reason).toBe("opt-out");
  });

  test("skips when prompt is too short", () => {
    const result = shouldRecall("short", false);
    expect(result.shouldRecall).toBe(false);
    expect(result.reason).toBe("too-short");
  });

  test("recalls on question words", () => {
    const result = shouldRecall("What is the status of my contacts?", false);
    expect(result.shouldRecall).toBe(true);
    expect(result.reason).toBe("question");
  });

  test("skips tool commands without questions", () => {
    const result = shouldRecall("run the build script for production", false);
    expect(result.shouldRecall).toBe(false);
    expect(result.reason).toBe("tool-command");
  });

  test("skips code-heavy content without questions", () => {
    // Code-heavy: less than 50% alpha characters + has file references
    const result = shouldRecall("src/lib/config.ts:42 => { a: 1, b: 2 }", false);
    expect(result.shouldRecall).toBe(false);
    expect(result.reason).toBe("code-prompt");
  });

  test("recalls on question even with tool command", () => {
    const result = shouldRecall("how do I run the build script?", false);
    expect(result.shouldRecall).toBe(true);
    expect(result.reason).toBe("question");
  });
});
