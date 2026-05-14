import { MAX_RUNNER_STDIO_CHUNK_BYTES } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { chunkStdio } from "../../src/internal/chunk.ts";

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
