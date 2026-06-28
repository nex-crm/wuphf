// Regression: buildPlanSmart must only fall back to the deterministic mock when
// the agent service is genuinely unreachable. A reachable service that returns
// an HTTP/schema error must surface — otherwise the UI "builds" a mock plan and
// then fails on run, because runWorkflowViaService() still hits the live backend.

import { afterEach, describe, expect, it, vi } from "vitest";

import { buildPlanSmart } from "./agentClient";

const realFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = realFetch;
  vi.restoreAllMocks();
});

// The build response streams over SSE: fake a one-chunk ReadableStream reader so
// buildPlanViaService can parse it the same way it parses the live service.
function streamResponse(
  text: string,
  init: { ok?: boolean; status?: number } = {},
): Response {
  const chunk = new TextEncoder().encode(text);
  let sent = false;
  const reader = {
    read: async () =>
      sent
        ? { done: true, value: undefined }
        : ((sent = true), { done: false, value: chunk }),
  };
  return {
    ok: init.ok ?? true,
    status: init.status ?? 200,
    body: { getReader: () => reader },
  } as unknown as Response;
}

describe("buildPlanSmart fallback boundary", () => {
  it("falls back to the mock plan when the service is unreachable", async () => {
    // fetch rejects with a TypeError on connection refused / offline.
    globalThis.fetch = vi
      .fn()
      .mockRejectedValue(
        new TypeError("Failed to fetch"),
      ) as unknown as typeof fetch;

    const plan = await buildPlanSmart(
      "Score inbound demo requests and route hot ones to Slack",
    );

    // The deterministic mock produced a real plan, not an empty/error shape.
    expect(plan.steps.length).toBeGreaterThan(0);
    expect(plan.toolId).toBe("inbound-routing");
  });

  it("surfaces an HTTP error from a reachable service instead of returning the mock", async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      body: null,
    } as unknown as Response) as unknown as typeof fetch;

    await expect(buildPlanSmart("anything")).rejects.toThrow(/500/);
  });

  it("surfaces a schema error (no spec event) instead of returning the mock", async () => {
    globalThis.fetch = vi
      .fn()
      .mockResolvedValue(
        streamResponse("event: status\ndata: {}\n\n"),
      ) as unknown as typeof fetch;

    await expect(buildPlanSmart("anything")).rejects.toThrow(/spec/i);
  });

  it("surfaces a terminal error event instead of returning the mock", async () => {
    globalThis.fetch = vi
      .fn()
      .mockResolvedValue(
        streamResponse('event: error\ndata: {"error":"boom"}\n\n'),
      ) as unknown as typeof fetch;

    await expect(buildPlanSmart("anything")).rejects.toThrow(/boom/i);
  });
});
