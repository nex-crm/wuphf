import { describe, expect, it } from "vitest";

import { readEventStream } from "./sse";

// Build a streaming Response whose body yields the given chunks in order, so a
// test can control exactly where frame boundaries land relative to reads.
function streamResponse(chunks: string[]): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      const enc = new TextEncoder();
      for (const c of chunks) controller.enqueue(enc.encode(c));
      controller.close();
    },
  });
  return new Response(body, { status: 200 });
}

async function collect(chunks: string[]): Promise<unknown[]> {
  const frames: unknown[] = [];
  await readEventStream(streamResponse(chunks), (d) => frames.push(d));
  return frames;
}

describe("readEventStream", () => {
  it("parses LF-delimited events", async () => {
    const frames = await collect([
      'data: {"n":1}\n\n',
      'data: {"n":2}\n\n',
      "event: end\ndata: {}\n\n",
    ]);
    expect(frames).toEqual([{ n: 1 }, { n: 2 }]);
  });

  it("parses CRLF-delimited events", async () => {
    const frames = await collect([
      'data: {"n":1}\r\n\r\n',
      'data: {"n":2}\r\n\r\n',
      "event: end\r\ndata: {}\r\n\r\n",
    ]);
    expect(frames).toEqual([{ n: 1 }, { n: 2 }]);
  });

  it("parses a CRLF stream split across read boundaries", async () => {
    const frames = await collect([
      'data: {"n":',
      "1}\r\n",
      '\r\ndata: {"n":2}\r\n\r\n',
    ]);
    expect(frames).toEqual([{ n: 1 }, { n: 2 }]);
  });

  it("flushes a trailing frame that is not blank-line terminated (LF)", async () => {
    const frames = await collect(['data: {"n":1}\n\n', 'data: {"n":2}']);
    expect(frames).toEqual([{ n: 1 }, { n: 2 }]);
  });

  it("flushes a trailing frame that is not blank-line terminated (CRLF)", async () => {
    const frames = await collect(['data: {"n":1}\r\n\r\n', 'data: {"n":2}']);
    expect(frames).toEqual([{ n: 1 }, { n: 2 }]);
  });

  it("skips the closing event: end / data: {} boundary", async () => {
    const frames = await collect(["event: end\r\ndata: {}\r\n\r\n"]);
    expect(frames).toEqual([]);
  });
});
