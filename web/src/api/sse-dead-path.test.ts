/**
 * Regression test for the SSE dead-path bug.
 *
 * Before this fix, `subscribeEditLog` built an EventSource pointed at
 * `/wiki/stream` — a path that does NOT exist on the broker (only
 * `/events` does). Every live update was silently dropped.
 *
 * These tests lock in two guarantees:
 *   1. The subscriber URL ends in `/events` (not a per-surface stream).
 *   2. Named-event handlers (`addEventListener('wiki:write', ...)` etc.)
 *      get wired — broker emits `event: wiki:write` lines, not unnamed
 *      `data:` lines, so `onmessage` would never fire.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { subscribeEditLog } from "./wiki";

class FakeEventSource {
  static lastURL: string | null = null;
  url: string;
  closed = false;
  listeners: Record<string, EventListener[]> = {};
  constructor(url: string) {
    this.url = url;
    FakeEventSource.lastURL = url;
  }
  addEventListener(name: string, fn: EventListener) {
    this.listeners[name] ??= [];
    this.listeners[name].push(fn);
  }
  removeEventListener(name: string, fn: EventListener) {
    const arr = this.listeners[name];
    if (!arr) return;
    this.listeners[name] = arr.filter((f) => f !== fn);
  }
  close() {
    this.closed = true;
  }
  emit(name: string, data: unknown) {
    const fns = this.listeners[name] ?? [];
    const ev = new MessageEvent("message", { data: JSON.stringify(data) });
    for (const fn of fns) fn(ev);
  }
}

describe("SSE dead-path fix", () => {
  const originalES = globalThis.EventSource;
  let created: FakeEventSource[] = [];

  beforeEach(() => {
    created = [];
    (globalThis as { EventSource: unknown }).EventSource =
      class extends FakeEventSource {
        constructor(url: string) {
          super(url);
          created.push(this);
        }
      } as unknown as typeof EventSource;
  });

  afterEach(() => {
    (globalThis as { EventSource: unknown }).EventSource = originalES;
  });

  it("subscribeEditLog opens /events (not /wiki/stream)", () => {
    const unsub = subscribeEditLog(() => {});
    const { lastURL } = FakeEventSource;
    expect(lastURL).not.toBeNull();
    if (!lastURL) throw new Error("Expected EventSource URL to be captured");
    expect(lastURL).not.toContain("/wiki/stream");
    expect(lastURL).toMatch(/\/events(\?|$)/);
    unsub();
  });

  it("subscribeEditLog uses named wiki:write listener", () => {
    const handler = vi.fn();
    const unsub = subscribeEditLog(handler);
    expect(created).toHaveLength(1);
    const [src] = created;
    if (!src) throw new Error("Expected EventSource to be created");
    // Broker sends an onmessage for the heartbeat `ready` event — handler
    // MUST NOT fire for that. Only the named `wiki:write` event counts.
    src.emit("message", { type: "ready" });
    expect(handler).not.toHaveBeenCalled();
    src.emit("wiki:write", {
      path: "team/people/sarah.md",
      author_slug: "ceo",
      date: "2026-04-21T10:00:00Z",
    });
    expect(handler).toHaveBeenCalledTimes(1);
    unsub();
    expect(src.closed).toBe(true);
  });
});
