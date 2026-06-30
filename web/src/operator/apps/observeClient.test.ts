import { beforeEach, describe, expect, it, type Mock, vi } from "vitest";

import {
  OBSERVE_UNAVAILABLE,
  type ObserveNavigate,
  type ObserveSnapshot,
  reduceObserved,
  runObserve,
} from "./observeClient";

vi.mock("../../api/client", () => ({ postStream: vi.fn() }));

import { postStream } from "../../api/client";

function snap(app: string, title: string, n: number): ObserveSnapshot {
  return {
    type: "snapshot",
    tick: 0,
    app,
    title,
    components: Array.from({ length: n }, (_, i) => ({
      role: "Button",
      label: `b${i}`,
    })),
    text_excerpt: "hello",
  };
}

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

describe("reduceObserved", () => {
  it("keeps distinct screens in first-seen order, richest snapshot of each", () => {
    const screens = reduceObserved([
      snap("Chrome", "HubSpot", 3),
      snap("Chrome", "HubSpot", 7), // richer → wins
      snap("Slack", "#ae-handoffs", 2),
      snap("Chrome", "HubSpot", 1), // poorer → ignored
    ]);
    expect(screens.map((s) => s.title)).toEqual(["HubSpot", "#ae-handoffs"]);
    expect(screens[0].components).toHaveLength(7);
    expect(screens[0].text).toBe("hello");
  });

  it("caps the number of screens", () => {
    const many = Array.from({ length: 20 }, (_, i) =>
      snap("Chrome", `t${i}`, 1),
    );
    expect(reduceObserved(many).length).toBeLessThanOrEqual(10);
  });
});

describe("runObserve", () => {
  beforeEach(() => vi.clearAllMocks());

  it("routes snapshot and event frames to the right callbacks", async () => {
    (postStream as Mock).mockResolvedValue(
      sseResponse([
        'data: {"type":"status","status":"observing"}\n\n',
        'data: {"type":"event","tick":0,"app":"Google Chrome","title":"HubSpot"}\n\n',
        'data: {"type":"snapshot","tick":1,"app":"Google Chrome","title":"HubSpot","components":[]}\n\n',
        "event: end\ndata: {}\n\n",
      ]),
    );
    const snaps: ObserveSnapshot[] = [];
    const navs: ObserveNavigate[] = [];
    await runObserve({
      onSnapshot: (s) => snaps.push(s),
      onNavigate: (n) => navs.push(n),
    });
    expect(snaps).toHaveLength(1);
    expect(snaps[0].app).toBe("Google Chrome");
    expect(navs).toHaveLength(1);
    expect(navs[0].title).toBe("HubSpot");
  });

  it("throws OBSERVE_UNAVAILABLE on 503 so the call proceeds without it", async () => {
    (postStream as Mock).mockResolvedValue(new Response(null, { status: 503 }));
    await expect(runObserve({})).rejects.toThrow(OBSERVE_UNAVAILABLE);
  });
});
