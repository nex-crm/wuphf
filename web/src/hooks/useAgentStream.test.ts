import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  agentStreamURL,
  appendStreamLine,
  type StreamLine,
  useAgentStream,
} from "./useAgentStream";

vi.mock("../api/client", () => ({
  // Mirror the real sseURL contract: append ?token=… unconditionally so
  // the agentStreamURL caller has to merge query strings safely.
  sseURL: (path: string) => `http://broker${path}?token=ABC`,
}));

// MockEventSource is a minimal EventSource stand-in that lets the test
// push named events (replay-end) and onmessage entries directly. JSDOM
// does not ship an EventSource implementation, and patching window
// globals makes the tests robust to the hook's underlying EventSource
// reference.
class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  onopen: ((this: EventSource, ev: Event) => unknown) | null = null;
  onmessage: ((this: EventSource, ev: MessageEvent) => unknown) | null = null;
  onerror: ((this: EventSource, ev: Event) => unknown) | null = null;
  closed = false;
  private listeners = new Map<string, Set<(ev: MessageEvent) => unknown>>();

  constructor(url: string | URL) {
    this.url = String(url);
    MockEventSource.instances.push(this);
  }

  addEventListener(name: string, fn: (ev: MessageEvent) => unknown) {
    let bucket = this.listeners.get(name);
    if (!bucket) {
      bucket = new Set();
      this.listeners.set(name, bucket);
    }
    bucket.add(fn);
  }

  removeEventListener(name: string, fn: (ev: MessageEvent) => unknown) {
    this.listeners.get(name)?.delete(fn);
  }

  dispatchData(data: string) {
    if (this.onmessage)
      this.onmessage.call(
        this as unknown as EventSource,
        new MessageEvent("message", { data }),
      );
  }

  dispatchNamed(name: string, data: string) {
    const bucket = this.listeners.get(name);
    if (!bucket) return;
    for (const fn of bucket) {
      fn(new MessageEvent(name, { data }));
    }
  }

  close() {
    this.closed = true;
  }
}

describe("appendStreamLine", () => {
  it("starts a new raw line when the buffer is empty", () => {
    const { lines, usedId } = appendStreamLine([], "hello", undefined, 1);
    expect(lines).toHaveLength(1);
    expect(lines[0]).toMatchObject({ id: 1, data: "hello", parsed: undefined });
    expect(usedId).toBe(true);
  });

  it("coalesces consecutive raw chunks into a single line", () => {
    // Regression: the local-LLM path streams ~5ms per chunk; without
    // coalescing every chunk renders as its own <div> in the Live
    // Output panel and the user sees "one word per line". This
    // assertion ensures consecutive raw events merge into ONE line
    // with concatenated text and the original line id.
    let lines: StreamLine[] = [];
    let nextId = 1;
    for (const chunk of ["I'm ", "the ", "planner — ", "what next?"]) {
      const result = appendStreamLine(lines, chunk, undefined, nextId);
      const { lines: nextLines, usedId } = result;
      lines = nextLines;
      if (usedId) nextId += 1;
    }
    expect(lines).toHaveLength(1);
    expect(lines[0].data).toBe("I'm the planner — what next?");
    expect(lines[0].id).toBe(1);
    expect(nextId).toBe(2); // only the FIRST chunk consumed an id
  });

  it("starts a new line when the previous event was structured", () => {
    const initial: StreamLine[] = [
      {
        id: 1,
        data: '{"type":"mcp_tool_event"}',
        parsed: { type: "mcp_tool_event" },
      },
    ];
    const { lines, usedId } = appendStreamLine(
      initial,
      "raw text",
      undefined,
      2,
    );
    expect(lines).toHaveLength(2);
    expect(lines[1]).toMatchObject({ id: 2, data: "raw text" });
    expect(usedId).toBe(true);
  });

  it("never merges into a structured line", () => {
    // The defensive case — make sure we don't accidentally append a
    // raw chunk's text onto a parsed JSON line and break downstream
    // rendering that depends on `data` being valid JSON.
    const initial: StreamLine[] = [
      {
        id: 1,
        data: '{"phase":"call","tool":"team_broadcast"}',
        parsed: { phase: "call", tool: "team_broadcast" },
      },
    ];
    const { lines } = appendStreamLine(initial, "extra", undefined, 2);
    expect(lines[0].data).toBe('{"phase":"call","tool":"team_broadcast"}');
    expect(lines[1].data).toBe("extra");
  });

  it("structured event after raw still starts its own line", () => {
    // Mirror flow: model streams raw text, then emits a tool_event
    // on tool dispatch. The structured event must NOT merge into
    // the raw line.
    const initial: StreamLine[] = [
      { id: 1, data: "Hello world", parsed: undefined },
    ];
    const { lines, usedId } = appendStreamLine(
      initial,
      '{"type":"mcp_tool_event","tool":"team_broadcast"}',
      { type: "mcp_tool_event", tool: "team_broadcast" },
      2,
    );
    expect(lines).toHaveLength(2);
    expect(lines[0].data).toBe("Hello world");
    expect(lines[1].parsed).toBeDefined();
    expect(usedId).toBe(true);
  });

  it("merges task into the URL with & when sseURL already added ?token=", () => {
    // Regression: an earlier version produced
    // `…/agent-stream/ceo?task=task-1?token=ABC`, so the query parser
    // folded the token into the task value and auth silently broke
    // for every task-scoped subscription. Guard the contract: if the
    // base URL already contains '?', the task param uses '&'.
    const url = agentStreamURL("ceo", "task-1");
    expect(url).toBe("http://broker/agent-stream/ceo?token=ABC&task=task-1");
  });

  it("returns the bare base URL when no taskId is provided", () => {
    expect(agentStreamURL("ceo", null)).toBe(
      "http://broker/agent-stream/ceo?token=ABC",
    );
    expect(agentStreamURL("ceo", "  ")).toBe(
      "http://broker/agent-stream/ceo?token=ABC",
    );
  });

  it("encodes slug and task to keep odd characters from breaking the URL", () => {
    expect(agentStreamURL("a/b", "t&1")).toBe(
      "http://broker/agent-stream/a%2Fb?token=ABC&task=t%261",
    );
  });

  it("trims to MAX_LINES (50) on overflow", () => {
    // Each entry alternates structured/raw so coalescing doesn't
    // collapse them — we want the 50-cap behavior tested directly.
    let lines: StreamLine[] = [];
    let nextId = 1;
    for (let i = 0; i < 60; i++) {
      const isStructured = i % 2 === 0;
      const result = appendStreamLine(
        lines,
        isStructured ? `{"i":${i}}` : `r${i}`,
        isStructured ? { i } : undefined,
        nextId,
      );
      const { lines: nextLines, usedId } = result;
      lines = nextLines;
      if (usedId) nextId += 1;
    }
    expect(lines.length).toBeLessThanOrEqual(50);
  });
});

describe("useAgentStream phase + idle behavior", () => {
  beforeEach(() => {
    MockEventSource.instances = [];
    (global as unknown as { EventSource: typeof MockEventSource }).EventSource =
      MockEventSource;
  });

  afterEach(() => {
    delete (global as unknown as { EventSource?: typeof MockEventSource })
      .EventSource;
  });

  it("does NOT close the EventSource when an idle event arrives during replay", () => {
    // Regression: pre-fix, the hook closed on any parsed.status === "idle"
    // past the first counter tick — meaning a HeadlessEvent idle from the
    // recent-history buffer would silently kill the live stream the moment
    // the user opened the viewer for an agent that just went idle. With
    // the replay-end boundary in place, the hook must hold the connection
    // open until phase flips to "live".
    const { result } = renderHook(() => useAgentStream("ceo"));
    const [source] = MockEventSource.instances;
    expect(source).toBeDefined();
    if (!source) return;

    // Replay phase: dispatch a HeadlessEvent idle through the history
    // pipe. Phase ref is still "replay" (broker has not yet sent the
    // boundary), so the hook must NOT close.
    act(() => {
      source.dispatchData(
        JSON.stringify({
          kind: "headless_event",
          type: "idle",
          status: "idle",
          provider: "claude",
        }),
      );
    });
    expect(source.closed).toBe(false);
    expect(result.current.lines.length).toBeGreaterThan(0);

    // Cross the boundary into live, then a NEW idle. Now the hook
    // must close — the live HeadlessEvent idle is the legitimate
    // turn-end signal.
    act(() => {
      source.dispatchNamed("replay-end", "{}");
      source.dispatchData(
        JSON.stringify({
          kind: "headless_event",
          type: "idle",
          status: "idle",
          provider: "claude",
        }),
      );
    });
    expect(source.closed).toBe(true);
  });

  it("ignores non-headless_event JSON entries with a status field", () => {
    // The agent stream carries multiple event shapes today (raw provider
    // chunks, mcp_tool_event audit lines, pane-capture noise). Only the
    // typed HeadlessEvent envelope is allowed to drive auto-close — a
    // foreign JSON object that happens to carry status:"idle" must not.
    renderHook(() => useAgentStream("ceo"));
    const [source] = MockEventSource.instances;
    expect(source).toBeDefined();
    if (!source) return;

    act(() => {
      source.dispatchNamed("replay-end", "{}");
      source.dispatchData(
        JSON.stringify({ type: "result", status: "idle", note: "not us" }),
      );
    });
    expect(source.closed).toBe(false);
  });
});
