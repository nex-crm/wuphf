import { afterEach, describe, expect, it, vi } from "vitest";

import {
  type ApiError,
  connectBroker,
  GET_TIMEOUT_MS,
  get,
  getRequestSignal,
  humanizeApiErrorBody,
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

// Regression: a broker-down wiki rendered the raw JSON envelope
// `{"error":"wiki backend is not active"}` verbatim. The shared client
// must surface the JSON body's message as plain human text so every
// GET/POST surface inherits the fix.
describe("error body humanization", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("renders a JSON error body as a plain sentence, never raw JSON", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error: "wiki backend is not active" }), {
        status: 503,
        statusText: "Service Unavailable",
        headers: { "Content-Type": "application/json" },
      }),
    );

    const err = await get("/wiki/article").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(Error);
    const { message } = err as Error;
    expect(message).toBe("Wiki backend is not active.");
    expect(message).not.toContain("{");
  });

  it("keeps code-shaped error bodies untouched for programmatic consumers", () => {
    expect(humanizeApiErrorBody(JSON.stringify({ error: "store_busy" }))).toBe(
      null,
    );
    expect(humanizeApiErrorBody("plain text failure")).toBe(null);
    expect(humanizeApiErrorBody("")).toBe(null);
  });

  it("leaves data-carrying envelopes (wiki 409 conflict shape) intact", () => {
    // tryParseConflict re-parses the conflict envelope out of err.message;
    // humanizing it would silently break concurrent-edit recovery.
    expect(
      humanizeApiErrorBody(
        JSON.stringify({
          error: "article changed since you opened it",
          current_sha: "abc123",
          current_content: "# Title",
        }),
      ),
    ).toBe(null);
  });

  it("preserves the raw bodyText on the ApiError for callers that parse it", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error: "wiki backend is not active" }), {
        status: 503,
        statusText: "Service Unavailable",
        headers: { "Content-Type": "application/json" },
      }),
    );

    const err = (await get("/wiki/article").catch(
      (e: unknown) => e,
    )) as ApiError;
    expect(err.bodyText).toContain("wiki backend is not active");
    expect(err.status).toBe(503);
  });
});
