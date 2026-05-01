import { afterEach, describe, expect, it, vi } from "vitest";

import { connectBroker } from "./client";

describe("connectBroker", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("times out stalled broker handshakes", async () => {
    vi.useFakeTimers();
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (_input: RequestInfo | URL, init?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => {
            const err = new Error("aborted");
            err.name = "AbortError";
            reject(err);
          });
        }),
    );

    const pending = expect(
      connectBroker("http://localhost:7890"),
    ).rejects.toThrow(/timed out connecting to broker/i);
    await vi.advanceTimersByTimeAsync(8000);

    await pending;
  });
});
