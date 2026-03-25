import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseInput } from "../../src/commands/parse-input.js";

describe("parseInput", () => {
  it("returns empty array for empty string", () => {
    assert.deepEqual(parseInput(""), []);
  });

  it("returns empty array for whitespace-only string", () => {
    assert.deepEqual(parseInput("   "), []);
  });

  it("parses a single command word", () => {
    assert.deepEqual(parseInput("objects"), ["objects"]);
  });

  it("parses multiple words", () => {
    assert.deepEqual(parseInput("record list person --limit 10"), [
      "record",
      "list",
      "person",
      "--limit",
      "10",
    ]);
  });

  it("handles double-quoted strings", () => {
    assert.deepEqual(parseInput('ask "hello world"'), ["ask", "hello world"]);
  });

  it("handles single-quoted strings", () => {
    assert.deepEqual(parseInput("remember 'this is a note'"), [
      "remember",
      "this is a note",
    ]);
  });

  it("handles JSON in single quotes", () => {
    assert.deepEqual(
      parseInput("record create person --data '{\"name\":\"John\"}'"),
      ["record", "create", "person", "--data", '{"name":"John"}'],
    );
  });

  it("handles mixed quotes and plain tokens", () => {
    assert.deepEqual(
      parseInput('ask "who is the CEO?" --format json'),
      ["ask", "who is the CEO?", "--format", "json"],
    );
  });

  it("handles extra spaces between tokens", () => {
    assert.deepEqual(parseInput("  ask   hello  "), ["ask", "hello"]);
  });

  it("handles empty quoted strings", () => {
    assert.deepEqual(parseInput('ask ""'), ["ask"]);
  });

  it("handles adjacent tokens without spaces after quotes", () => {
    // Edge case: quotes in the middle of a token
    assert.deepEqual(parseInput('hello"world"'), ["helloworld"]);
  });

  it("preserves content inside quotes with special characters", () => {
    assert.deepEqual(
      parseInput('search "O\'Brien & Co."'),
      ["search", "O'Brien & Co."],
    );
  });
});
