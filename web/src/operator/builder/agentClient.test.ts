// Regression: buildPlanSmart must only fall back to the deterministic mock when
// the agent service is genuinely unreachable. A reachable service that returns
// an HTTP/schema error must surface — otherwise the UI "builds" a mock plan and
// then fails on run, because runOperatorPlan() still hits the live broker.

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

  it("surfaces a terminal error event (service's { message } shape) instead of the mock", async () => {
    // The service emits failures as `event: error` with { message }, matching
    // agent/src/service.ts — the regression must use that real payload shape.
    globalThis.fetch = vi
      .fn()
      .mockResolvedValue(
        streamResponse('event: error\ndata: {"message":"boom"}\n\n'),
      ) as unknown as typeof fetch;

    await expect(buildPlanSmart("anything")).rejects.toThrow(/boom/i);
  });

  it("grounds the build by forwarding the operator's connected integrations", async () => {
    // The client fetches connected integrations from WUPHF's GET /integrations
    // and includes them in the /build/stream body so the agent's
    // list_integrations tool can ground the plan. Only connected toolkits go.
    const spec =
      'event: spec\ndata: {"spec":{"name":"X","tool_id":"inbound-routing","steps":[]}}\n\n';
    let buildBody: unknown = null;
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("/integrations")) {
        return {
          ok: true,
          status: 200,
          json: async () => ({
            providers: [],
            items: [
              { provider: "composio", platform: "hubspot", name: "HubSpot", state: "connected", can_connect: false, can_disconnect: true },
              { provider: "composio", platform: "slack", name: "Slack", state: "disconnected", can_connect: true, can_disconnect: false },
            ],
          }),
        } as unknown as Response;
      }
      buildBody = init?.body ? JSON.parse(String(init.body)) : null;
      return streamResponse(spec);
    }) as unknown as typeof fetch;

    await buildPlanSmart("route hot demos");

    const body = buildBody as {
      integrations?: { provider: string; name: string; connected: boolean }[];
    };
    expect(body.integrations).toEqual([
      { provider: "hubspot", name: "HubSpot", connected: true },
    ]);
  });

  it("forwards `step` events to onActivity and resolves the terminal spec", async () => {
    // The service streams one `step` event per WorkflowStep ({ type, step }),
    // then a terminal `spec` event — the client must surface the steps live.
    const sse =
      'event: step\ndata: {"type":"step","step":{"title":"Look up the record","integration":"HubSpot"}}\n\n' +
      'event: spec\ndata: {"spec":{"name":"X","tool_id":"inbound-routing","steps":[]}}\n\n';
    globalThis.fetch = vi
      .fn()
      .mockResolvedValue(streamResponse(sse)) as unknown as typeof fetch;

    const activity: string[] = [];
    const plan = await buildPlanSmart("anything", (a) => activity.push(a.text));
    expect(activity).toContain("Look up the record");
    expect(plan.toolId).toBe("inbound-routing");
  });
});
