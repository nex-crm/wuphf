// @vitest-environment happy-dom

import { QueryClient } from "@tanstack/react-query";
import { waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  consumeBrokerEvents,
  invalidateForBrokerFrame,
  useBrokerEvents,
} from "../../src/renderer/sse/useBrokerEvents.ts";
import {
  apiTokenFromBootstrap,
  brokerUrlFromBootstrap,
} from "../../src/renderer/bootstrap/types.ts";
import { renderWithProviders, VALID_BROKER_URL, VALID_TOKEN } from "./test-utils.tsx";

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
      brokerUrl: brokerUrlFromBootstrap(VALID_BROKER_URL),
      bearer: apiTokenFromBootstrap(VALID_TOKEN),
      queryClient,
      signal: new AbortController().signal,
      fetchImpl: fetchMock,
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

    const { queryClient } = renderWithProviders(<HookProbe />);
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

    renderWithProviders(<HookProbe />);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
  });

  it("ignores failed or empty SSE responses", async () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await consumeBrokerEvents({
      brokerUrl: brokerUrlFromBootstrap(VALID_BROKER_URL),
      bearer: apiTokenFromBootstrap(VALID_TOKEN),
      queryClient,
      signal: new AbortController().signal,
      fetchImpl: () => Promise.resolve({ ok: false, status: 401, body: null } as Response),
    });
    await consumeBrokerEvents({
      brokerUrl: brokerUrlFromBootstrap(VALID_BROKER_URL),
      bearer: apiTokenFromBootstrap(VALID_TOKEN),
      queryClient,
      signal: new AbortController().signal,
      fetchImpl: () => Promise.resolve({ ok: true, status: 200, body: null } as Response),
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
  });
});

function HookProbe() {
  useBrokerEvents();
  return null;
}

function sseResponse(frame: string): Response {
  return {
    ok: true,
    status: 200,
    body: new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode(frame));
        controller.close();
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
    payload: { threadId, headLsn: "1" },
  };
}
