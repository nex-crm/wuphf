import { afterEach, describe, expect, it, vi } from "vitest";

import {
  type AgentStreamEventSource,
  agentStreamPath,
  subscribeAgentStream,
} from "./agentStreamClient";

class FakeEventSource implements AgentStreamEventSource {
  static instances: FakeEventSource[] = [];

  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  readyState = 1;
  closed = false;

  constructor(readonly url: string) {
    FakeEventSource.instances.push(this);
  }

  close() {
    this.closed = true;
    this.readyState = 2;
  }

  emitLine(data: string) {
    this.onmessage?.({ data } as MessageEvent<string>);
  }
}

describe("agent stream client", () => {
  const originalEventSource = globalThis.EventSource;

  afterEach(() => {
    FakeEventSource.instances = [];
    (globalThis as { EventSource?: typeof EventSource }).EventSource =
      originalEventSource;
  });

  it("builds encoded stream paths", () => {
    expect(agentStreamPath("builder one")).toBe("/agent-stream/builder%20one");
  });

  it("opens one EventSource and forwards lifecycle callbacks", () => {
    const events: string[] = [];
    const subscription = subscribeAgentStream(
      "builder",
      {
        onOpen: () => events.push("open"),
        onLine: (line) => events.push(`line:${line}`),
        onClose: () => events.push("close"),
      },
      { eventSourceFactory: (url) => new FakeEventSource(url) },
    );

    const [source] = FakeEventSource.instances;
    expect(source.url).toBe("/api/agent-stream/builder");
    source.onopen?.({} as Event);
    source.emitLine("hello");
    subscription.close();
    source.emitLine("ignored");

    expect(events).toEqual(["open", "line:hello", "close"]);
    expect(source.closed).toBe(true);
  });

  it("closes immediately without an error when slug is empty", () => {
    const events: string[] = [];

    const subscription = subscribeAgentStream("  ", {
      onError: () => events.push("error"),
      onClose: () => events.push("close"),
    });
    subscription.close();

    expect(events).toEqual(["close"]);
    expect(FakeEventSource.instances).toHaveLength(0);
  });

  it("keeps transient EventSource errors reconnectable", () => {
    const onError = vi.fn();
    subscribeAgentStream(
      "builder",
      { onError },
      { eventSourceFactory: (url) => new FakeEventSource(url) },
    );

    const [source] = FakeEventSource.instances;
    source.readyState = 0;
    source.onerror?.({} as Event);

    expect(onError).toHaveBeenCalledOnce();
    expect(source.closed).toBe(false);
  });

  it("closes once on terminal EventSource errors", () => {
    const events: string[] = [];
    subscribeAgentStream(
      "builder",
      {
        onError: () => events.push("error"),
        onClose: () => events.push("close"),
      },
      { eventSourceFactory: (url) => new FakeEventSource(url) },
    );

    const [source] = FakeEventSource.instances;
    source.readyState = 2;
    source.onerror?.({} as Event);
    source.onerror?.({} as Event);

    expect(events).toEqual(["error", "close"]);
    expect(source.closed).toBe(true);
  });

  it("treats errors without readyState as terminal", () => {
    const events: string[] = [];
    const sourceWithoutReadyState: AgentStreamEventSource = {
      onopen: null,
      onmessage: null,
      onerror: null,
      close: () => events.push("source-close"),
    };
    subscribeAgentStream(
      "builder",
      {
        onError: () => events.push("error"),
        onClose: () => events.push("close"),
      },
      { eventSourceFactory: () => sourceWithoutReadyState },
    );

    sourceWithoutReadyState.onerror?.({} as Event);

    expect(events).toEqual(["error", "source-close", "close"]);
  });

  it("returns a closed no-op when EventSource is unavailable", () => {
    (globalThis as { EventSource?: typeof EventSource }).EventSource =
      undefined;
    const events: string[] = [];

    const subscription = subscribeAgentStream("builder", {
      onError: () => events.push("error"),
      onClose: () => events.push("close"),
    });
    subscription.close();

    expect(events).toEqual(["error", "close"]);
  });

  it("routes factory failures through error and close handlers", () => {
    const events: string[] = [];

    const subscription = subscribeAgentStream(
      "builder",
      {
        onError: () => events.push("error"),
        onClose: () => events.push("close"),
      },
      {
        eventSourceFactory: () => {
          throw new Error("factory failed");
        },
      },
    );
    subscription.close();

    expect(events).toEqual(["error", "close"]);
  });
});
