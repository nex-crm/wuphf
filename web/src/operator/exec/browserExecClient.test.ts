import { beforeEach, describe, expect, it, type Mock, vi } from "vitest";

import {
  EXEC_UNAVAILABLE,
  type RunnerEvent,
  runBrowserExec,
} from "./browserExecClient";

vi.mock("../../api/client", () => ({ postStream: vi.fn() }));

import { postStream } from "../../api/client";

function sseResponse(chunks: string[], status = 200): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      const enc = new TextEncoder();
      for (const c of chunks) controller.enqueue(enc.encode(c));
      controller.close();
    },
  });
  return new Response(body, { status });
}

describe("runBrowserExec", () => {
  beforeEach(() => vi.clearAllMocks());

  it("parses each SSE data frame into a RunnerEvent and skips the end frame", async () => {
    (postStream as Mock).mockResolvedValue(
      sseResponse([
        'data: {"type":"status","status":"running"}\n\n',
        'data: {"type":"action","label":"Clicked Search","reasoning":"to search"}\n\n',
        'data: {"type":"done","result":"ok"}\n\n',
        "event: end\ndata: {}\n\n",
      ]),
    );
    const events: RunnerEvent[] = [];
    await runBrowserExec({
      goal: "open search",
      onEvent: (e) => events.push(e),
    });

    expect(events).toHaveLength(3);
    expect(events[0]).toMatchObject({ type: "status", status: "running" });
    expect(events[1]).toMatchObject({
      type: "action",
      label: "Clicked Search",
    });
    expect(events[2]).toMatchObject({ type: "done", result: "ok" });
  });

  it("reassembles a frame split across chunk boundaries", async () => {
    (postStream as Mock).mockResolvedValue(
      sseResponse(['data: {"type":"act', 'ion","label":"X"}\n\n']),
    );
    const events: RunnerEvent[] = [];
    await runBrowserExec({ goal: "g", onEvent: (e) => events.push(e) });
    expect(events).toEqual([{ type: "action", label: "X" }]);
  });

  it("throws EXEC_UNAVAILABLE on a 503 so the caller can fall back to the mock", async () => {
    (postStream as Mock).mockResolvedValue(new Response(null, { status: 503 }));
    await expect(
      runBrowserExec({ goal: "g", onEvent: () => {} }),
    ).rejects.toThrow(EXEC_UNAVAILABLE);
  });

  it("sends goal, app and window_id to the endpoint", async () => {
    (postStream as Mock).mockResolvedValue(
      sseResponse(["event: end\ndata: {}\n\n"]),
    );
    await runBrowserExec({
      goal: "g",
      app: "Google Chrome",
      windowId: 7,
      onEvent: () => {},
    });
    expect(postStream).toHaveBeenCalledWith(
      "/execute/browser",
      { goal: "g", app: "Google Chrome", window_id: 7 },
      expect.objectContaining({}),
    );
  });
});
