import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "./client";
import {
  createSurface,
  listSurfaces,
  patchWidget,
  subscribeSurfaceEvents,
} from "./surfaces";

describe("surfaces API", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("listSurfaces passes the human viewer slug", async () => {
    const getSpy = vi
      .spyOn(client, "get")
      .mockResolvedValue({ surfaces: [] });

    await listSurfaces();

    expect(getSpy).toHaveBeenCalledWith("/surfaces", {
      viewer_slug: "human",
    });
  });

  it("createSurface writes through the broker surfaces endpoint", async () => {
    const postSpy = vi
      .spyOn(client, "post")
      .mockResolvedValue({ surface: { id: "launch" } });

    await createSurface({ title: "Launch", channel: "general" });

    expect(postSpy).toHaveBeenCalledWith("/surfaces", {
      title: "Launch",
      channel: "general",
      created_by: "human",
      my_slug: "human",
    });
  });

  it("patchWidget sends a bounded PATCH body", async () => {
    const patchSpy = vi
      .spyOn(client, "patch")
      .mockResolvedValue({ widget: { id: "notes" } });

    await patchWidget("launch", "notes", {
      mode: "snippet",
      search: "old",
      replacement: "new",
    });

    expect(patchSpy).toHaveBeenCalledWith("/surfaces/launch/widgets/notes", {
      mode: "snippet",
      search: "old",
      replacement: "new",
      actor: "human",
    });
  });
});

describe("subscribeSurfaceEvents", () => {
  const originalEventSource = globalThis.EventSource;

  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    (globalThis as { EventSource?: unknown }).EventSource = originalEventSource;
  });

  it("returns a no-op when EventSource is unavailable", () => {
    (globalThis as { EventSource?: unknown }).EventSource = undefined;
    const unsub = subscribeSurfaceEvents(() => {});
    expect(typeof unsub).toBe("function");
    unsub();
  });

  it("opens /events and listens for named surface events", () => {
    type Listener = (ev: MessageEvent) => void;
    const listeners: Record<string, Listener[]> = {};
    const close = vi.fn();
    let createdURL = "";
    class FakeES {
      constructor(url: string) {
        createdURL = url;
      }
      addEventListener(name: string, cb: Listener) {
        (listeners[name] ??= []).push(cb);
      }
      removeEventListener(name: string, cb: Listener) {
        listeners[name] = (listeners[name] ?? []).filter((l) => l !== cb);
      }
      close = close;
    }
    (globalThis as { EventSource?: unknown }).EventSource = FakeES;

    const hits: unknown[] = [];
    const unsub = subscribeSurfaceEvents((event) => hits.push(event));

    expect(createdURL).toMatch(/\/events(\?|$)/);
    expect(createdURL).not.toContain("/surfaces/stream");

    listeners["surface:widget_updated"][0](
      new MessageEvent("message", {
        data: JSON.stringify({
          type: "surface:widget_updated",
          surface_id: "launch",
          widget_id: "notes",
          channel: "general",
          created_at: "2026-04-30T00:00:00Z",
        }),
      }),
    );
    listeners["surface:widget_updated"][0](
      new MessageEvent("message", { data: "{not-json" }),
    );

    expect(hits).toHaveLength(1);
    unsub();
    expect(close).toHaveBeenCalled();
  });
});
