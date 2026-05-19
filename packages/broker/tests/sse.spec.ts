import { EventEmitter } from "node:events";
import type { ServerResponse } from "node:http";

import { describe, expect, it } from "vitest";

import { createSseHub } from "../src/sse.ts";

const READY_OPTS = {
  idForReady: "ready_1",
  keepaliveMs: 60_000,
  nowIso: "2026-05-18T11:00:00.000Z",
} as const;

class FakeSseResponse extends EventEmitter {
  statusCode = 0;
  writableEnded = false;
  destroyed = false;
  writableLength = 0;
  writeResult = true;
  readonly chunks: string[] = [];
  readonly headers = new Map<string, string>();

  setHeader(name: string, value: string): this {
    this.headers.set(name.toLowerCase(), value);
    return this;
  }

  flushHeaders(): void {}

  write(chunk: string | Uint8Array): boolean {
    this.chunks.push(chunkToString(chunk));
    return this.writeResult;
  }

  writeHead(statusCode: number, headers: Readonly<Record<string, string>> = {}): this {
    this.statusCode = statusCode;
    for (const [name, value] of Object.entries(headers)) {
      this.headers.set(name.toLowerCase(), value);
    }
    return this;
  }

  end(chunk?: string | Uint8Array): this {
    if (chunk !== undefined) this.write(chunk);
    this.writableEnded = true;
    this.emit("close");
    return this;
  }

  destroy(): this {
    this.destroyed = true;
    this.emit("close");
    return this;
  }

  asResponse(): ServerResponse {
    return this as unknown as ServerResponse;
  }

  text(): string {
    return this.chunks.join("");
  }
}

describe("SseHub", () => {
  it("caps concurrent sessions", () => {
    const hub = createSseHub({ maxSessions: 1 });
    const first = new FakeSseResponse();
    const second = new FakeSseResponse();

    const firstSession = hub.startSession(first.asResponse(), READY_OPTS);
    const secondSession = hub.startSession(second.asResponse(), READY_OPTS);

    expect(first.statusCode).toBe(200);
    expect(second.statusCode).toBe(503);
    expect(second.headers.get("retry-after")).toBe("1");
    expect(second.writableEnded).toBe(true);
    expect(second.text()).toBe("sse_session_limit\n");

    hub.emit({ id: "event_1", event: "approval.requested", data: { ok: true } });
    expect(first.text()).toContain("event: approval.requested");
    expect(second.text()).not.toContain("approval.requested");

    firstSession.close();
    secondSession.close();
  });

  it("drops a client when write backpressure is reported", () => {
    const hub = createSseHub();
    const slow = new FakeSseResponse();
    const fast = new FakeSseResponse();

    hub.startSession(slow.asResponse(), READY_OPTS);
    hub.startSession(fast.asResponse(), READY_OPTS);
    slow.writeResult = false;

    hub.emit({ id: "event_1", event: "approval.requested", data: { ok: true } });
    expect(slow.destroyed).toBe(true);
    const slowChunkCount = slow.chunks.length;

    hub.emit({ id: "event_2", event: "approval.decided", data: { ok: true } });
    expect(slow.chunks).toHaveLength(slowChunkCount);
    expect(fast.text()).toContain("event: approval.requested");
    expect(fast.text()).toContain("event: approval.decided");
  });

  it("drops a client whose writable buffer exceeds the threshold", () => {
    const hub = createSseHub({ maxWritableLength: 8 });
    const res = new FakeSseResponse();

    hub.startSession(res.asResponse(), READY_OPTS);
    res.writableLength = 9;

    hub.emit({ id: "event_1", event: "approval.requested", data: { ok: true } });

    expect(res.destroyed).toBe(true);
    expect(res.text()).not.toContain("event: approval.requested");
  });
});

function chunkToString(chunk: string | Uint8Array): string {
  return typeof chunk === "string" ? chunk : Buffer.from(chunk).toString("utf8");
}
