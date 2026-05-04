import { describe, expect, it, vi } from "vitest";

import {
  createTerminalWriteBuffer,
  formatAgentTerminalChunk,
  type TerminalScheduler,
} from "./agentTerminalBuffer";

function manualScheduler() {
  let callback: (() => void) | null = null;
  const schedule: TerminalScheduler = (next) => {
    callback = next;
    return () => {
      callback = null;
    };
  };
  return {
    schedule,
    flush: () => callback?.(),
    hasPending: () => callback !== null,
  };
}

describe("agent terminal output formatting", () => {
  it("skips stream handshake lines", () => {
    expect(formatAgentTerminalChunk("[connected]")).toBeNull();
  });

  it("adds terminal newlines to line-based SSE chunks", () => {
    expect(formatAgentTerminalChunk("hello")).toBe("hello\r\n");
  });

  it("preserves chunks that already include line endings", () => {
    expect(formatAgentTerminalChunk("hello\n")).toBe("hello\n");
  });
});

describe("terminal write buffer", () => {
  it("batches multiple chunks into one scheduled write", () => {
    const scheduler = manualScheduler();
    const write = vi.fn();
    const buffer = createTerminalWriteBuffer(write, {
      schedule: scheduler.schedule,
    });

    buffer.enqueue("one");
    buffer.enqueue("two");
    expect(write).not.toHaveBeenCalled();

    scheduler.flush();
    expect(write).toHaveBeenCalledOnce();
    expect(write).toHaveBeenCalledWith("onetwo");
  });

  it("drops older pending output when the terminal falls behind", () => {
    const scheduler = manualScheduler();
    const write = vi.fn();
    const buffer = createTerminalWriteBuffer(write, {
      maxPendingChars: 5,
      schedule: scheduler.schedule,
    });

    buffer.enqueue("12345");
    buffer.enqueue("67890");
    scheduler.flush();

    const written = String(write.mock.calls[0]?.[0]);
    expect(written).toContain("output dropped");
    expect(written.endsWith("67890")).toBe(true);
  });

  it("cancels scheduled writes on dispose", () => {
    const scheduler = manualScheduler();
    const write = vi.fn();
    const buffer = createTerminalWriteBuffer(write, {
      schedule: scheduler.schedule,
    });

    buffer.enqueue("hello");
    expect(scheduler.hasPending()).toBe(true);
    buffer.dispose();
    scheduler.flush();

    expect(write).not.toHaveBeenCalled();
  });
});
