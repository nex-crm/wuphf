import { afterEach, describe, expect, it, vi } from "vitest";

import {
  type ApiError,
  connectBroker,
  GET_TIMEOUT_MS,
  get,
  getRequestSignal,
  initApi,
  post,
} from "./client";

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

  it("preserves structured broker error codes and retry hints on failed posts", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error: "store_busy" }), {
        status: 503,
        statusText: "Service Unavailable",
        headers: {
          "Content-Type": "application/json",
          "Retry-After": "1",
        },
      }),
    );

    await expect(post("/webauthn/cosign/challenge", {})).rejects.toMatchObject({
      status: 503,
      errorCode: "store_busy",
      retryAfter: "1",
    } satisfies Partial<ApiError>);
  });
});

// C3/C4 regression: a wedged broker must never leave a read surface on
// an eternal spinner — GETs carry a timeout signal by default, and a
// timeout abort surfaces as an honest "broker not responding" error
// the surfaces can render with a retry.
describe("get timeout", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("always attaches an abort signal to GETs (no infinite hang)", () => {
    // No caller-provided signal → a timeout signal is created. This is
    // the layer the Notebooks-tab 60s+ spinner bug lived at: plain
    // fetch with no signal never gives up on a wedged broker.
    const signal = getRequestSignal();
    expect(signal).toBeInstanceOf(AbortSignal);
    expect(signal.aborted).toBe(false);
    expect(GET_TIMEOUT_MS).toBeGreaterThan(0);

    // Caller-provided signals win (no double-abort surprises).
    const controller = new AbortController();
    expect(getRequestSignal({ signal: controller.signal })).toBe(
      controller.signal,
    );
  });

  it("surfaces a timeout abort as a broker-not-responding error", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      (_input: RequestInfo | URL, _init?: RequestInit) => {
        const err = new DOMException(
          "The operation timed out.",
          "TimeoutError",
        );
        return Promise.reject(err);
      },
    );

    await expect(get("/notebook/catalog")).rejects.toThrow(
      /broker not responding/i,
    );
  });

  it("passes the timeout signal to fetch", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    await get("/office/stats");

    const init = fetchMock.mock.calls.at(-1)?.[1];
    expect(init?.signal).toBeInstanceOf(AbortSignal);
  });
});
