import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useAppStore } from "../stores/app";
import { useBrokerEvents } from "./useBrokerEvents";

function navigateRouter(pathname: string): void {
  // The hook parses window.location.hash to decide whether the active
  // route already covers an emitted broker channel, so set the hash
  // directly. Matches what hash-history navigation produces at runtime.
  window.location.hash = `#${pathname}`;
}

class FakeEventSource {
  static created: FakeEventSource[] = [];
  listeners: Record<string, EventListener[]> = {};
  onerror: (() => void) | null = null;
  // Mirror the EventSource constants so `source.readyState !== source.OPEN`
  // checks behave the way the production code expects in tests.
  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSED = 2;
  readyState = 1; // default OPEN; tests flip to CLOSED to simulate disconnect

  constructor(_url: string) {
    FakeEventSource.created.push(this);
  }

  addEventListener(name: string, fn: EventListener) {
    if (!this.listeners[name]) {
      this.listeners[name] = [];
    }
    this.listeners[name].push(fn);
  }

  close() {}

  emit(name: string, data: unknown) {
    const event = new MessageEvent(name, { data: JSON.stringify(data) });
    for (const fn of this.listeners[name] ?? []) fn(event);
  }

  emitRaw(name: string, data: string) {
    const event = new MessageEvent(name, { data });
    for (const fn of this.listeners[name] ?? []) fn(event);
  }

  triggerError() {
    if (typeof this.onerror === "function") {
      this.onerror();
    }
  }
}

function BrokerEventsHarness() {
  useBrokerEvents(true);
  return null;
}

function renderHarness() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <BrokerEventsHarness />
    </QueryClientProvider>,
  );
}

describe("useBrokerEvents unread counts", () => {
  const originalEventSource = globalThis.EventSource;

  beforeEach(() => {
    FakeEventSource.created = [];
    (globalThis as { EventSource: unknown }).EventSource =
      FakeEventSource as unknown as typeof EventSource;
    useAppStore.setState({ unreadByChannel: {} });
    navigateRouter("/channels/general");
  });

  afterEach(() => {
    (globalThis as { EventSource: unknown }).EventSource = originalEventSource;
    useAppStore.setState({ unreadByChannel: {}, brokerConnected: false });
    navigateRouter("/channels/general");
  });

  it("counts message events for another channel, including thread replies", () => {
    renderHarness();
    const [source] = FakeEventSource.created;

    act(() => {
      source.emit("message", {
        message: {
          id: "msg-2",
          channel: "launch",
          reply_to: "msg-1",
        },
      });
    });

    expect(useAppStore.getState().unreadByChannel.launch).toBe(1);
  });

  it("does not count messages while viewing that channel's message feed", () => {
    renderHarness();
    const [source] = FakeEventSource.created;

    act(() => {
      source.emit("message", {
        message: {
          id: "msg-1",
          channel: "general",
        },
      });
    });

    expect(useAppStore.getState().unreadByChannel.general ?? 0).toBe(0);
  });

  it("counts messages for the selected channel while an app is open", () => {
    navigateRouter("/apps/tasks");
    useAppStore.setState({ unreadByChannel: {} });
    renderHarness();
    const [source] = FakeEventSource.created;

    act(() => {
      source.emit("message", {
        message: {
          id: "msg-1",
          channel: "general",
        },
      });
    });

    expect(useAppStore.getState().unreadByChannel.general).toBe(1);
  });

  it("suppresses unread for the canonical DM channel slug while viewing /dm/<agent>", () => {
    // The hook hashes /dm/<agent> through directChannelSlug to get the
    // broker's canonical "<lower>__<higher>" pairing — matching what
    // the broker emits on `message`. This regression-pins that mapping
    // for both ordering directions.
    navigateRouter("/dm/pm");
    renderHarness();
    const [source] = FakeEventSource.created;

    act(() => {
      source.emit("message", {
        message: { id: "msg-1", channel: "human__pm" },
      });
    });

    expect(useAppStore.getState().unreadByChannel.human__pm ?? 0).toBe(0);
  });

  it("suppresses unread for /dm/<agent> when the agent slug sorts after `human`", () => {
    navigateRouter("/dm/ceo");
    renderHarness();
    const [source] = FakeEventSource.created;

    act(() => {
      source.emit("message", {
        message: { id: "msg-1", channel: "ceo__human" },
      });
    });

    expect(useAppStore.getState().unreadByChannel.ceo__human ?? 0).toBe(0);
  });

  it("ignores message events without a channel", () => {
    renderHarness();
    const [source] = FakeEventSource.created;

    act(() => {
      source.emit("message", {
        message: {
          id: "msg-1",
        },
      });
    });

    expect(useAppStore.getState().unreadByChannel).toEqual({});
  });

  it("ignores message events with malformed JSON", () => {
    renderHarness();
    const [source] = FakeEventSource.created;

    act(() => {
      source.emitRaw("message", "{not-json");
    });

    expect(useAppStore.getState().unreadByChannel).toEqual({});
  });

  it("ignores message events with a blank channel", () => {
    renderHarness();
    const [source] = FakeEventSource.created;

    act(() => {
      source.emit("message", {
        message: {
          id: "msg-1",
          channel: "   ",
        },
      });
    });

    expect(useAppStore.getState().unreadByChannel).toEqual({});
  });

  it("ignores message events with a non-string channel", () => {
    renderHarness();
    const [source] = FakeEventSource.created;

    act(() => {
      source.emit("message", {
        message: {
          id: "msg-1",
          channel: 42,
        },
      });
    });

    expect(useAppStore.getState().unreadByChannel).toEqual({});
  });
});

describe("useBrokerEvents activity stream", () => {
  const originalEventSource = globalThis.EventSource;

  beforeEach(() => {
    FakeEventSource.created = [];
    (globalThis as { EventSource: unknown }).EventSource =
      FakeEventSource as unknown as typeof EventSource;
    useAppStore.setState({
      agentActivitySnapshots: {},
      isReconnecting: false,
    });
    navigateRouter("/channels/general");
  });

  afterEach(() => {
    (globalThis as { EventSource: unknown }).EventSource = originalEventSource;
    useAppStore.setState({
      agentActivitySnapshots: {},
      isReconnecting: false,
      brokerConnected: false,
    });
  });

  it("invalidates office-members AND records the snapshot on activity", () => {
    // CRITICAL REGRESSION: cache invalidation MUST keep firing after the
    // snapshot-store wiring lands. Tracking the invalidate call confirms
    // downstream surfaces (workspace presence, channel members) still
    // refresh.
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");

    render(
      <QueryClientProvider client={queryClient}>
        <BrokerEventsHarness />
      </QueryClientProvider>,
    );
    const [source] = FakeEventSource.created;

    act(() => {
      source.emit("activity", {
        slug: "tess",
        status: "active",
        activity: "drafting reply",
        kind: "routine",
      });
    });

    const invalidatedKeys = invalidateSpy.mock.calls.map(
      (call) => (call[0] as { queryKey?: unknown[] }).queryKey?.[0],
    );
    expect(invalidatedKeys).toContain("office-members");
    expect(invalidatedKeys).toContain("channel-members");

    const snap = useAppStore.getState().agentActivitySnapshots.tess;
    expect(snap).toBeDefined();
    expect(snap.activity).toBe("drafting reply");
    expect(snap.kind).toBe("routine");
    expect(typeof snap.receivedAtMs).toBe("number");
  });

  it("invalidates the cache even when the activity payload is malformed", () => {
    // A throw inside the snapshot-record path must not prevent the
    // existing cache invalidation from running — that would silently
    // freeze every member-list surface.
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    render(
      <QueryClientProvider client={queryClient}>
        <BrokerEventsHarness />
      </QueryClientProvider>,
    );
    const [source] = FakeEventSource.created;

    act(() => {
      source.emitRaw("activity", "{not-json");
    });

    const keys = invalidateSpy.mock.calls.map(
      (call) => (call[0] as { queryKey?: unknown[] }).queryKey?.[0],
    );
    expect(keys).toContain("office-members");
    expect(useAppStore.getState().agentActivitySnapshots).toEqual({});
    warnSpy.mockRestore();
  });
});

describe("useBrokerEvents disconnect grace", () => {
  const originalEventSource = globalThis.EventSource;

  beforeEach(() => {
    vi.useFakeTimers();
    FakeEventSource.created = [];
    (globalThis as { EventSource: unknown }).EventSource =
      FakeEventSource as unknown as typeof EventSource;
    useAppStore.setState({ isReconnecting: false });
  });

  afterEach(() => {
    vi.useRealTimers();
    (globalThis as { EventSource: unknown }).EventSource = originalEventSource;
    useAppStore.setState({ isReconnecting: false, brokerConnected: false });
  });

  it("flips isReconnecting after 5s of sustained CLOSED readyState", () => {
    renderHarness();
    const [source] = FakeEventSource.created;

    act(() => {
      source.readyState = source.CLOSED;
      source.triggerError();
    });

    // 4.5s in — still inside the grace window.
    act(() => {
      vi.advanceTimersByTime(4500);
    });
    expect(useAppStore.getState().isReconnecting).toBe(false);

    // Past the 5s threshold — now we treat it as a sustained disconnect.
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(useAppStore.getState().isReconnecting).toBe(true);
  });

  it("does not flip isReconnecting when the connection reopens within the grace window", () => {
    renderHarness();
    const [source] = FakeEventSource.created;

    act(() => {
      source.readyState = source.CLOSED;
      source.triggerError();
    });

    // Connection comes back at 3s — open event clears the grace timer.
    act(() => {
      vi.advanceTimersByTime(3000);
      source.readyState = source.OPEN;
      const event = new Event("open");
      for (const fn of source.listeners.open ?? []) fn(event);
    });

    // Past the original 5s window — grace was cleared, so still false.
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(useAppStore.getState().isReconnecting).toBe(false);
  });
});
