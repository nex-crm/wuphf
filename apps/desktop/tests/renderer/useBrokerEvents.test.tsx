// @vitest-environment happy-dom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  type BrokerStreamState,
  consumeBrokerEvents,
  invalidateForBrokerFrame,
  useBrokerEvents,
  useBrokerStreamState,
} from "../../src/renderer/sse/useBrokerEvents.ts";
import { BrokerBootstrapContext } from "../../src/renderer/bootstrap/useBrokerBootstrap.ts";
import {
  VALID_BOOTSTRAP,
  readyBootstrapState,
  renderWithProviders,
  VALID_BROKER_URL,
  VALID_TOKEN,
} from "./test-utils.tsx";

describe("useBrokerEvents", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("fetches the SSE stream with bearer auth and invalidates thread frames", async () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const fetchMock = vi.fn<typeof fetch>((input, init) => {
      expect(String(input)).toBe(`${VALID_BROKER_URL}/api/events`);
      const headers = init?.headers;
      expect(headers).toEqual({
        Accept: "text/event-stream",
        Authorization: `Bearer ${VALID_TOKEN}`,
      });
      return Promise.resolve(sseResponse(threadUpdatedFrame("01ARZ3NDEKTSV4RRFFQ69G5FAV")));
    });

    await consumeBrokerEvents({
      brokerUrl: VALID_BOOTSTRAP.brokerUrl,
      bearer: VALID_BOOTSTRAP.token,
      queryClient,
      signal: new AbortController().signal,
      fetchImpl: fetchMock,
      reconnect: { maxReconnectAttempts: 0 },
    });

    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["threads", "list"] });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["threads", "detail", "01ARZ3NDEKTSV4RRFFQ69G5FAV"],
    });
  });

  it("subscribes when bootstrap is ready and invalidates pinned approvals", async () => {
    const fetchMock = vi.fn<typeof fetch>(() =>
      Promise.resolve(sseResponse(threadPinnedApprovalsFrame("01ARZ3NDEKTSV4RRFFQ69G5FAV"))),
    );
    vi.stubGlobal("fetch", fetchMock);

    const { queryClient, unmount } = renderWithProviders(<HookProbe />);
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(`${VALID_BROKER_URL}/api/events`, {
        headers: {
          Accept: "text/event-stream",
          Authorization: `Bearer ${VALID_TOKEN}`,
        },
        signal: expect.any(AbortSignal) as AbortSignal,
      });
    });

    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: [
          "threads",
          "detail",
          "01ARZ3NDEKTSV4RRFFQ69G5FAV",
          "pinned-approvals",
        ],
      });
      expect(invalidate).toHaveBeenCalledWith({ queryKey: ["approvals"] });
    });

    unmount();
  });

  it("invalidates approval frames by request and related thread", () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    expect(
      invalidateForBrokerFrame(queryClient, {
        event: "approval.requested",
        data: JSON.stringify(
          approvalEvent(
            "approval.requested",
            "01CRZ3NDEKTSV4RRFFQ69G5FAV",
            "01ARZ3NDEKTSV4RRFFQ69G5FAV",
          ),
        ),
      }),
    ).toBe(true);

    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["approvals", "list"] });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["approvals", "detail", "01CRZ3NDEKTSV4RRFFQ69G5FAV"],
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["threads", "detail", "01ARZ3NDEKTSV4RRFFQ69G5FAV", "pinned-approvals"],
    });
  });

  it("aborts the stream signal on unmount", async () => {
    let streamSignal: AbortSignal | null = null;
    const fetchMock = vi.fn<typeof fetch>((_input, init) => {
      const signal = init?.signal;
      if (!(signal instanceof AbortSignal)) {
        throw new Error("expected AbortSignal");
      }
      streamSignal = signal;
      return Promise.resolve(neverEndingSseResponse());
    });
    vi.stubGlobal("fetch", fetchMock);

    const { unmount } = renderWithProviders(<HookProbe />);

    await waitFor(() => {
      expect(streamSignal).not.toBeNull();
    });
    expect(requireSignal(streamSignal).aborted).toBe(false);

    unmount();

    expect(requireSignal(streamSignal).aborted).toBe(true);
  });

  it("reconnects after a transient stream close", async () => {
    const queryClient = new QueryClient();
    const states: BrokerStreamState[] = [];
    const fetchMock = vi.fn<typeof fetch>(() => Promise.resolve(sseResponse("")));

    await consumeBrokerEvents({
      brokerUrl: VALID_BOOTSTRAP.brokerUrl,
      bearer: VALID_BOOTSTRAP.token,
      queryClient,
      signal: new AbortController().signal,
      fetchImpl: fetchMock,
      onStateChange: (state) => states.push(state),
      reconnect: {
        initialDelayMs: 1,
        maxDelayMs: 1,
        maxReconnectAttempts: 1,
      },
    });

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(states).toContainEqual({ status: "reconnecting", attempt: 1, retryInMs: 1 });
    expect(states.at(-1)).toMatchObject({ status: "dead" });
  });

  it("surfaces bearer expiry without reconnecting on a 401 stream open", async () => {
    const fetchMock = vi.fn<typeof fetch>(() =>
      Promise.resolve({ ok: false, status: 401, body: null } as Response),
    );
    vi.stubGlobal("fetch", fetchMock);

    renderWithProviders(<HookAndStreamStateProbe />);

    expect(await screen.findByText("bearer-expired")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("can consume events without a stream-state provider", async () => {
    const fetchMock = vi.fn<typeof fetch>(() =>
      Promise.resolve({ ok: false, status: 401, body: null } as Response),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(
      <QueryClientProvider client={new QueryClient()}>
        <BrokerBootstrapContext.Provider value={readyBootstrapState()}>
          <HookProbe />
        </BrokerBootstrapContext.Provider>
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });
  });

  it("requires the stream-state hook to be used inside its provider", () => {
    expect(() => render(<StreamStateOnlyProbe />)).toThrow(
      "useBrokerStreamState must be used inside BrokerStreamStateProvider",
    );
  });

  it("aborts and marks the stream dead when an unterminated frame exceeds the cap", async () => {
    const queryClient = new QueryClient();
    const states: BrokerStreamState[] = [];
    let streamSignal: AbortSignal | null = null;
    const fetchMock = vi.fn<typeof fetch>((_input, init) => {
      const signal = init?.signal;
      if (!(signal instanceof AbortSignal)) {
        throw new Error("expected AbortSignal");
      }
      streamSignal = signal;
      return Promise.resolve(unterminatedSseResponse("x".repeat(32)));
    });

    await consumeBrokerEvents({
      brokerUrl: VALID_BOOTSTRAP.brokerUrl,
      bearer: VALID_BOOTSTRAP.token,
      queryClient,
      signal: new AbortController().signal,
      fetchImpl: fetchMock,
      onStateChange: (state) => states.push(state),
      reconnect: { frameByteLimit: 16 },
    });

    expect(requireSignal(streamSignal).aborted).toBe(true);
    expect(states.at(-1)).toEqual({ status: "dead", reason: "SSE frame exceeded 16 bytes" });
  });

  it("returns without fetching when the caller signal is already aborted", async () => {
    const controller = new AbortController();
    controller.abort();
    const queryClient = new QueryClient();
    const fetchMock = vi.fn<typeof fetch>(() => Promise.resolve(sseResponse("")));

    await consumeBrokerEvents({
      brokerUrl: VALID_BOOTSTRAP.brokerUrl,
      bearer: VALID_BOOTSTRAP.token,
      queryClient,
      signal: controller.signal,
      fetchImpl: fetchMock,
    });

    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("uses the generic dead reason for non-Error stream failures", async () => {
    const queryClient = new QueryClient();
    const states: BrokerStreamState[] = [];

    await consumeBrokerEvents({
      brokerUrl: VALID_BOOTSTRAP.brokerUrl,
      bearer: VALID_BOOTSTRAP.token,
      queryClient,
      signal: new AbortController().signal,
      fetchImpl: () => Promise.reject("closed"),
      onStateChange: (state) => states.push(state),
      reconnect: { maxReconnectAttempts: 0 },
    });

    expect(states.at(-1)).toEqual({ status: "dead", reason: "stream failed" });
  });

  it("stops a reconnect wait when the caller aborts during backoff", async () => {
    const queryClient = new QueryClient();
    const states: BrokerStreamState[] = [];
    const controller = new AbortController();
    const fetchMock = vi.fn<typeof fetch>(() => Promise.resolve(sseResponse("")));

    await consumeBrokerEvents({
      brokerUrl: VALID_BOOTSTRAP.brokerUrl,
      bearer: VALID_BOOTSTRAP.token,
      queryClient,
      signal: controller.signal,
      fetchImpl: fetchMock,
      onStateChange: (state) => {
        states.push(state);
        if (state.status === "reconnecting") {
          controller.abort();
        }
      },
      reconnect: {
        initialDelayMs: 1,
        maxDelayMs: 1,
        maxReconnectAttempts: 1,
      },
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(states).toContainEqual({ status: "reconnecting", attempt: 1, retryInMs: 1 });
  });

  it("does not subscribe until bootstrap is ready", () => {
    const fetchMock = vi.fn<typeof fetch>(() =>
      Promise.resolve(sseResponse(threadUpdatedFrame("01ARZ3NDEKTSV4RRFFQ69G5FAV"))),
    );
    vi.stubGlobal("fetch", fetchMock);

    renderWithProviders(<HookProbe />, {
      status: "loading",
      brokerStatus: null,
      bearer: null,
      brokerUrl: null,
      error: null,
      retry: vi.fn<() => void>(),
    });

    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("swallows stream failures from the hook effect", async () => {
    const fetchMock = vi.fn<typeof fetch>(() => Promise.reject(new Error("stream down")));
    vi.stubGlobal("fetch", fetchMock);

    const { unmount } = renderWithProviders(<HookProbe />);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });

    unmount();
  });

  it("does not invalidate caches on failed or empty SSE responses", async () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await consumeBrokerEvents({
      brokerUrl: VALID_BOOTSTRAP.brokerUrl,
      bearer: VALID_BOOTSTRAP.token,
      queryClient,
      signal: new AbortController().signal,
      fetchImpl: () => Promise.resolve({ ok: false, status: 503, body: null } as Response),
      reconnect: { maxReconnectAttempts: 0 },
    });
    await consumeBrokerEvents({
      brokerUrl: VALID_BOOTSTRAP.brokerUrl,
      bearer: VALID_BOOTSTRAP.token,
      queryClient,
      signal: new AbortController().signal,
      fetchImpl: () => Promise.resolve({ ok: true, status: 200, body: null } as Response),
      reconnect: { maxReconnectAttempts: 0 },
    });

    expect(invalidate).not.toHaveBeenCalled();
  });

  it("invalidates broad caches on ready frames and rejects invalid thread frames", () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    expect(invalidateForBrokerFrame(queryClient, { event: "ready", data: "{}" })).toBe(true);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["threads"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["approvals"] });

    expect(invalidateForBrokerFrame(queryClient, { event: "thread.updated", data: "not-json" }))
      .toBe(false);
    expect(
      invalidateForBrokerFrame(queryClient, {
        event: "thread.updated",
        data: JSON.stringify({ kind: "thread.deleted", payload: {} }),
      }),
    ).toBe(false);
    expect(
      invalidateForBrokerFrame(queryClient, {
        event: "thread.updated",
        data: JSON.stringify({ kind: "thread.updated", payload: null }),
      }),
    ).toBe(false);
    expect(
      invalidateForBrokerFrame(queryClient, {
        event: "thread.updated",
        data: JSON.stringify(threadEvent("thread.created", "01ARZ3NDEKTSV4RRFFQ69G5FAV")),
      }),
    ).toBe(false);
    expect(
      invalidateForBrokerFrame(queryClient, {
        event: "thread.updated",
        data: JSON.stringify({ kind: "thread.updated", payload: { threadId: 42 } }),
      }),
    ).toBe(false);
    expect(
      invalidateForBrokerFrame(queryClient, {
        event: "receipt.created",
        data: JSON.stringify({ kind: "receipt.created", payload: {} }),
      }),
    ).toBe(false);
    expect(
      invalidateForBrokerFrame(queryClient, {
        event: "approval.requested",
        data: JSON.stringify({
          id: "1",
          kind: "approval.requested",
          emittedAt: "2026-05-10T12:00:00.000Z",
          payload: null,
        }),
      }),
    ).toBe(false);
    expect(
      invalidateForBrokerFrame(queryClient, {
        event: "approval.decided",
        data: JSON.stringify(
          approvalEvent("approval.requested", "01CRZ3NDEKTSV4RRFFQ69G5FAV", undefined),
        ),
      }),
    ).toBe(false);
    expect(
      invalidateForBrokerFrame(queryClient, {
        event: "approval.decided",
        data: JSON.stringify(
          approvalEvent("approval.decided", "01CRZ3NDEKTSV4RRFFQ69G5FAV", undefined),
        ),
      }),
    ).toBe(true);
  });
});

function HookProbe() {
  useBrokerEvents();
  return null;
}

function HookAndStreamStateProbe() {
  useBrokerEvents();
  const state = useBrokerStreamState();
  return <p>{state.status}</p>;
}

function StreamStateOnlyProbe() {
  const state = useBrokerStreamState();
  return <p>{state.status}</p>;
}

function sseResponse(frame: string): Response {
  return {
    ok: true,
    status: 200,
    body: new ReadableStream<Uint8Array>({
      start(controller) {
        if (frame.length > 0) {
          controller.enqueue(new TextEncoder().encode(frame));
        }
        controller.close();
      },
    }),
  } as Response;
}

function neverEndingSseResponse(): Response {
  return {
    ok: true,
    status: 200,
    body: new ReadableStream<Uint8Array>(),
  } as Response;
}

function unterminatedSseResponse(payload: string): Response {
  return {
    ok: true,
    status: 200,
    body: new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode(`event: thread.updated\ndata: ${payload}`));
      },
    }),
  } as Response;
}

function threadUpdatedFrame(threadId: string): string {
  return `event: thread.updated\ndata: ${JSON.stringify(threadEvent("thread.updated", threadId))}\n\n`;
}

function threadPinnedApprovalsFrame(threadId: string): string {
  return `event: thread.pinned_approvals.changed\ndata: ${JSON.stringify(threadEvent("thread.pinned_approvals.changed", threadId))}\n\n`;
}

function threadEvent(kind: string, threadId: string): unknown {
  return {
    id: "1",
    kind,
    emittedAt: "2026-05-10T12:00:00.000Z",
    payload: { threadId, headLsn: "v1:1" },
  };
}

function approvalEvent(
  kind: string,
  requestId: string,
  threadId: string | undefined,
): unknown {
  return {
    id: "1",
    kind,
    emittedAt: "2026-05-10T12:00:00.000Z",
    payload: threadId === undefined ? { requestId, headLsn: "v1:2" } : { requestId, threadId, headLsn: "v1:2" },
  };
}

function requireSignal(signal: AbortSignal | null): AbortSignal {
  if (signal === null) {
    throw new Error("expected stream signal");
  }
  return signal;
}
