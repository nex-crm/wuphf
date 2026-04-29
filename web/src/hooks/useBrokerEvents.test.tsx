import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { useAppStore } from "../stores/app";
import { useBrokerEvents } from "./useBrokerEvents";

class FakeEventSource {
  static created: FakeEventSource[] = [];
  listeners: Record<string, EventListener[]> = {};
  onerror: (() => void) | null = null;

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
    useAppStore.setState({
      currentChannel: "general",
      currentApp: null,
      unreadByChannel: {},
    });
  });

  afterEach(() => {
    (globalThis as { EventSource: unknown }).EventSource = originalEventSource;
    useAppStore.setState({
      currentChannel: "general",
      currentApp: null,
      unreadByChannel: {},
      brokerConnected: false,
    });
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
    useAppStore.setState({
      currentChannel: "general",
      currentApp: "tasks",
      unreadByChannel: {},
    });
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
