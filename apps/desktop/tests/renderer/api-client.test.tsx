// @vitest-environment happy-dom

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  BrokerHttpError,
  createBrokerApiClient,
  useBrokerApiClient,
} from "../../src/renderer/api/client.ts";
import { listThreads } from "../../src/renderer/api/threads.ts";
import { BrokerBootstrapContext } from "../../src/renderer/bootstrap/useBrokerBootstrap.ts";
import {
  VALID_BOOTSTRAP,
  jsonResponse,
  readyBootstrapState,
  VALID_BROKER_URL,
  VALID_TOKEN,
} from "./test-utils.tsx";

describe("broker API client", () => {
  it("sends bearer auth to the broker origin and decodes thread responses", async () => {
    const fetchMock = vi.fn<typeof fetch>((input, init) => {
      expect(String(input)).toBe(`${VALID_BROKER_URL}/api/v1/threads`);
      const headers = init?.headers;
      if (!(headers instanceof Headers)) {
        throw new Error("Expected Headers instance");
      }
      expect(headers.get("Authorization")).toBe(`Bearer ${VALID_TOKEN}`);
      expect(headers.get("Accept")).toBe("application/json");
      return Promise.resolve(jsonResponse(threadListWire()));
    });
    const client = createBrokerApiClient(
      {
        brokerUrl: VALID_BOOTSTRAP.brokerUrl,
        bearer: VALID_BOOTSTRAP.token,
      },
      fetchMock,
    );

    const response = await listThreads(client);

    expect(response.threads).toHaveLength(1);
    expect(response.threads[0]?.title).toBe("Renderer foundation");
  });

  it("rejects API paths that escape the broker origin", async () => {
    const client = createBrokerApiClient({
      brokerUrl: VALID_BOOTSTRAP.brokerUrl,
      bearer: VALID_BOOTSTRAP.token,
    });

    await expect(client.getJson("https://example.com/api", (value) => value)).rejects.toThrow(
      "Broker API path must be absolute",
    );
    await expect(client.getJson("//example.com/api", (value) => value)).rejects.toThrow(
      "Broker API path escaped broker origin",
    );
  });

  it("posts JSON through a required request encoder with bearer auth and content type", async () => {
    interface CreateThreadRequest {
      readonly title: string;
    }

    const encodeThreadRequest = vi.fn((request: CreateThreadRequest) => ({
      encodedTitle: request.title,
    }));
    const fetchMock = vi.fn<typeof fetch>((input, init) => {
      expect(String(input)).toBe(`${VALID_BROKER_URL}/api/v1/threads`);
      expect(init?.method).toBe("POST");
      expect(init?.body).toBe(JSON.stringify({ encodedTitle: "Renderer foundation" }));
      const headers = init?.headers;
      if (!(headers instanceof Headers)) {
        throw new Error("Expected Headers instance");
      }
      expect(headers.get("Authorization")).toBe(`Bearer ${VALID_TOKEN}`);
      expect(headers.get("Accept")).toBe("application/json");
      expect(headers.get("Content-Type")).toBe("application/json");
      return Promise.resolve(jsonResponse({ ok: true }));
    });
    const client = createBrokerApiClient(
      {
        brokerUrl: VALID_BOOTSTRAP.brokerUrl,
        bearer: VALID_BOOTSTRAP.token,
      },
      fetchMock,
    );

    const response = await client.postJson(
      "/api/v1/threads",
      { title: "Renderer foundation" },
      encodeThreadRequest,
      (value) => {
        if (typeof value !== "object" || value === null || !("ok" in value)) {
          throw new Error("bad response");
        }
        return value as { readonly ok: true };
      },
    );

    expect(encodeThreadRequest).toHaveBeenCalledWith({ title: "Renderer foundation" });
    expect(response.ok).toBe(true);
  });

  it("surfaces decoded route error envelopes on non-ok broker responses", async () => {
    const client = createBrokerApiClient(
      {
        brokerUrl: VALID_BOOTSTRAP.brokerUrl,
        bearer: VALID_BOOTSTRAP.token,
      },
      () =>
        Promise.resolve(
          jsonResponse({ error: "busy", message: "broker busy", retryAfterMs: 250 }, 503),
        ),
    );

    try {
      await client.getJson("/api/v1/threads", (value) => value);
      throw new Error("expected request to fail");
    } catch (error) {
      expect(error).toBeInstanceOf(BrokerHttpError);
      expect(error).toMatchObject({ status: 503 });
      expect(error).toHaveProperty("routeError", {
        error: "busy",
        message: "broker busy",
        retryAfterMs: 250,
      });
      expect(error).toHaveProperty("message", "Broker request failed with HTTP 503");
    }
  });

  it("keeps HTTP errors when a non-ok body is not JSON", async () => {
    const client = createBrokerApiClient(
      {
        brokerUrl: VALID_BOOTSTRAP.brokerUrl,
        bearer: VALID_BOOTSTRAP.token,
      },
      () => Promise.resolve(new Response("not-json", { status: 502 })),
    );

    await expect(client.getJson("/api/v1/threads", (value) => value)).rejects.toMatchObject({
      status: 502,
      routeError: null,
    });
  });

  it("builds a hook client from ready bootstrap state", async () => {
    let capturedClient: ReturnType<typeof useBrokerApiClient> | null = null;

    function Probe() {
      capturedClient = useBrokerApiClient();
      return <p>client ready</p>;
    }

    render(
      <BrokerBootstrapContext.Provider value={readyBootstrapState()}>
        <Probe />
      </BrokerBootstrapContext.Provider>,
    );

    expect(screen.getByText("client ready")).toBeInTheDocument();
    expect(capturedClient).not.toBeNull();
  });
});

function threadListWire(): unknown {
  const threadId = "01ARZ3NDEKTSV4RRFFQ69G5FAV";
  const revisionId = "01BRZ3NDEKTSV4RRFFQ69G5FAV";
  return {
    schemaVersion: 1,
    threads: [
      {
        thread_id: threadId,
        title: "Renderer foundation",
        status: "open",
        spec: {
          revision_id: revisionId,
          thread_id: threadId,
          content: { body: "R1" },
          content_hash: "a".repeat(64),
          authored_by: "agent:desktop",
          authored_at: "2026-05-10T12:00:00.000Z",
        },
        external_refs: { source_urls: [], entity_ids: [] },
        task_ids: [],
        created_by: "agent:desktop",
        created_at: "2026-05-10T12:00:00.000Z",
        updated_at: "2026-05-10T12:00:00.000Z",
        effectiveStatus: "open",
        boardColumn: "running",
        currentSeat: "agent",
        pendingApprovalCount: 0,
      },
    ],
  };
}
