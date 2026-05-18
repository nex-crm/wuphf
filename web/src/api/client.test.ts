import { afterEach, describe, expect, it, vi } from "vitest";

import { connectBroker, initApi, post } from "./client";

describe("connectBroker", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
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

  it("sends the bootstrap bearer on same-origin api calls", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            token: "same-origin-token",
            broker_url: "http://127.0.0.1:4567",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

    await initApi();
    await expect(post("/webauthn/cosign/challenge", {})).resolves.toEqual({
      ok: true,
    });

    expect(fetchMock).toHaveBeenLastCalledWith(
      "/api/webauthn/cosign/challenge",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({
          Authorization: "Bearer same-origin-token",
        }),
      }),
    );
  });
});
