import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { streamText, splitIntoWordChunks } from "../../../src/tui/hooks/use-streaming.js";

// ─── splitIntoWordChunks ───

describe("splitIntoWordChunks", () => {
  it("splits text into word-level chunks", () => {
    const chunks = splitIntoWordChunks("hello world foo");
    assert.equal(chunks.length, 3);
    assert.equal(chunks[0], "hello ");
    assert.equal(chunks[1], "world ");
    assert.equal(chunks[2], "foo");
  });

  it("handles single word", () => {
    const chunks = splitIntoWordChunks("hello");
    assert.equal(chunks.length, 1);
    assert.equal(chunks[0], "hello");
  });

  it("handles empty string", () => {
    const chunks = splitIntoWordChunks("");
    assert.equal(chunks.length, 0);
  });

  it("preserves multiple spaces between words", () => {
    const chunks = splitIntoWordChunks("a  b");
    // regex matches "a  " and "b"
    assert.equal(chunks.length, 2);
    assert.equal(chunks[0], "a  ");
    assert.equal(chunks[1], "b");
  });

  it("handles whitespace-only string", () => {
    const chunks = splitIntoWordChunks("   ");
    // No \S+ matches, falls back to single chunk
    assert.equal(chunks.length, 1);
    assert.equal(chunks[0], "   ");
  });
});

// ─── streamText (async generator) ───

describe("streamText", () => {
  it("yields progressively longer word-level substrings", async () => {
    const text = "hello world foo";
    const chunks: string[] = [];

    for await (const { text: t } of streamText(text, { chunkMs: 1 })) {
      chunks.push(t);
    }

    assert.equal(chunks.length, 3, `expected 3 chunks, got ${chunks.length}`);
    assert.equal(chunks[0], "hello ");
    assert.equal(chunks[1], "hello world ");
    assert.equal(chunks[2], "hello world foo");
  });

  it("marks last chunk as not streaming", async () => {
    const text = "hi there";
    let lastState = { text: "", isStreaming: true };

    for await (const state of streamText(text, { chunkMs: 1 })) {
      lastState = state;
    }

    assert.equal(lastState.text, text);
    assert.equal(lastState.isStreaming, false);
  });

  it("marks intermediate chunks as streaming", async () => {
    const text = "one two three";
    const states: Array<{ text: string; isStreaming: boolean }> = [];

    for await (const state of streamText(text, { chunkMs: 1 })) {
      states.push(state);
    }

    assert.equal(states[0].isStreaming, true);
    assert.equal(states[1].isStreaming, true);
    assert.equal(states[2].isStreaming, false);
  });

  it("handles empty text", async () => {
    const chunks: string[] = [];

    for await (const { text } of streamText("", { chunkMs: 1 })) {
      chunks.push(text);
    }

    assert.equal(chunks.length, 0, "empty text should yield no chunks");
  });

  it("handles single word", async () => {
    const chunks: string[] = [];

    for await (const { text } of streamText("hello", { chunkMs: 1 })) {
      chunks.push(text);
    }

    assert.equal(chunks.length, 1);
    assert.equal(chunks[0], "hello");
  });

  it("completes immediately when signal is already aborted", async () => {
    const controller = new AbortController();
    controller.abort();

    const chunks: Array<{ text: string; isStreaming: boolean }> = [];

    for await (const state of streamText("hello world", { chunkMs: 1 }, controller.signal)) {
      chunks.push(state);
    }

    assert.equal(chunks.length, 1);
    assert.equal(chunks[0].text, "hello world");
    assert.equal(chunks[0].isStreaming, false);
  });

  it("completes remaining text when signal aborts mid-stream", async () => {
    const controller = new AbortController();
    const text = "one two three four";
    const chunks: Array<{ text: string; isStreaming: boolean }> = [];

    for await (const state of streamText(text, { chunkMs: 1 }, controller.signal)) {
      chunks.push(state);
      if (chunks.length === 1) {
        controller.abort();
      }
    }

    assert.ok(chunks.length >= 2, `expected >=2 chunks, got ${chunks.length}`);
    const last = chunks[chunks.length - 1];
    assert.equal(last.text, text);
    assert.equal(last.isStreaming, false);
  });
});
