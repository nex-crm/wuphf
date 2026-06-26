/**
 * Tests for the shared broker event-stream multiplexer.
 *
 * The regression these guard against: each real-time feature used to open its
 * OWN EventSource to `/events`. A busy route opened ~8, exceeding the browser's
 * ~6-connection-per-origin cap and starving other requests (the create-page
 * POST hung forever). The multiplexer collapses every subscriber onto ONE
 * shared connection, so these tests lock in (a) single-connection sharing,
 * (b) ref-counted teardown, and (c) per-subscriber listener isolation.
 */
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { openSharedEventStream } from "./eventStream";

class FakeEventSource {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 2;
  CONNECTING = 0;
  OPEN = 1;
  CLOSED = 2;
  readyState = 1;
  url: string;
  closed = false;
  listeners: Record<string, EventListener[]> = {};
  constructor(url: string) {
    this.url = url;
  }
  addEventListener(name: string, fn: EventListener) {
    (this.listeners[name] ??= []).push(fn);
  }
  removeEventListener(name: string, fn: EventListener) {
    const arr = this.listeners[name];
    if (arr) this.listeners[name] = arr.filter((f) => f !== fn);
  }
  close() {
    this.closed = true;
    this.readyState = 2;
  }
  emit(name: string, data: unknown) {
    const ev = new MessageEvent(name, { data: JSON.stringify(data) });
    for (const fn of this.listeners[name] ?? []) fn(ev);
  }
  count(name: string) {
    return (this.listeners[name] ?? []).length;
  }
}

describe("openSharedEventStream", () => {
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

  it("multiplexes every subscriber onto a single EventSource", () => {
    const a = openSharedEventStream();
    const b = openSharedEventStream();
    const c = openSharedEventStream();
    expect(a && b && c).toBeTruthy();
    expect(created).toHaveLength(1);
    a?.close();
    b?.close();
    c?.close();
  });

  it("keeps the shared connection open until the last subscriber closes", () => {
    const a = openSharedEventStream();
    const b = openSharedEventStream();
    const shared = created[0];
    a?.close();
    expect(shared.closed).toBe(false); // b still attached
    b?.close();
    expect(shared.closed).toBe(true); // last one out closes it
  });

  it("detaches only the closing subscriber's listeners", () => {
    const a = openSharedEventStream();
    const b = openSharedEventStream();
    const shared = created[0];
    const seenA: unknown[] = [];
    const seenB: unknown[] = [];
    a?.addEventListener("wiki:write", (e) =>
      seenA.push((e as MessageEvent).data),
    );
    b?.addEventListener("wiki:write", (e) =>
      seenB.push((e as MessageEvent).data),
    );
    expect(shared.count("wiki:write")).toBe(2);

    shared.emit("wiki:write", { v: 1 });
    expect(seenA).toHaveLength(1);
    expect(seenB).toHaveLength(1);

    a?.close(); // removes only A's listener; shared stays open for B
    expect(shared.closed).toBe(false);
    expect(shared.count("wiki:write")).toBe(1);

    shared.emit("wiki:write", { v: 2 });
    expect(seenA).toHaveLength(1); // A no longer receives
    expect(seenB).toHaveLength(2);
    b?.close();
  });

  it("reopens a fresh connection after all subscribers have left", () => {
    const a = openSharedEventStream();
    a?.close();
    expect(created[0].closed).toBe(true);
    const b = openSharedEventStream();
    expect(created).toHaveLength(2);
    expect(created[1].closed).toBe(false);
    b?.close();
  });

  it("routes onerror through the shared source and clears it on close", () => {
    const a = openSharedEventStream();
    const shared = created[0];
    const handler = () => {};
    if (a) a.onerror = handler;
    expect(shared.count("error")).toBe(1);
    a?.close();
    expect(shared.count("error")).toBe(0);
  });
});
