import { MAX_RUNNER_STDIO_CHUNK_BYTES } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import {
  BoundedLineBuffer,
  chunkStdio,
  RunnerInputBufferOverflow,
} from "../../src/internal/chunk.ts";

describe("chunkStdio", () => {
  it("splits logical lines at the protocol stdio byte budget", () => {
    const line = "x".repeat(200 * 1024);
    const chunks = chunkStdio(line);

    expect(chunks.length).toBeGreaterThanOrEqual(4);
    expect(
      chunks.every((chunk) => Buffer.byteLength(chunk, "utf8") <= MAX_RUNNER_STDIO_CHUNK_BYTES),
    ).toBe(true);
    expect(chunks.join("")).toBe(line);
  });
});

describe("BoundedLineBuffer", () => {
  it("returns complete lines and flushes trailing carryover", () => {
    const buffer = new BoundedLineBuffer(16);

    expect(buffer.push("one\nt")).toEqual(["one"]);
    expect(buffer.push("wo\nthree")).toEqual(["two"]);
    expect(buffer.flush()).toEqual(["three"]);
  });

  it("throws before an incomplete line exceeds the input budget", () => {
    const buffer = new BoundedLineBuffer(4);

    expect(() => buffer.push("12345")).toThrow(RunnerInputBufferOverflow);
  });
});
