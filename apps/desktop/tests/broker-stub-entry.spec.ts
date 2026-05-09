import { EventEmitter } from "node:events";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const SIGNALS = ["SIGTERM", "SIGINT"] as const;

class FakeParentPort extends EventEmitter {
  readonly postMessage = vi.fn<(message: unknown) => void>();
}

class ProcessExitError extends Error {
  readonly code: string | number | null | undefined;

  constructor(code: string | number | null | undefined) {
    super(`process.exit(${String(code)})`);
    this.code = code;
  }
}

type ProcessListener = (...args: readonly unknown[]) => void;

let parentPortDescriptor: PropertyDescriptor | undefined;
let originalSignalListeners = new Map<NodeJS.Signals, readonly ProcessListener[]>();

describe("broker-stub-entry", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.resetModules();
    parentPortDescriptor = Object.getOwnPropertyDescriptor(process, "parentPort");
    originalSignalListeners = new Map(
      SIGNALS.map((signal) => [signal, process.listeners(signal) as readonly ProcessListener[]]),
    );
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    vi.spyOn(process, "exit").mockImplementation(((code?: string | number | null): never => {
      throw new ProcessExitError(code);
    }) as typeof process.exit);
  });

  afterEach(() => {
    restoreParentPort();
    restoreSignalListeners();
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("exits with an error when parentPort is missing", async () => {
    setParentPort(null);

    await expect(importBrokerStubEntry()).rejects.toMatchObject({ code: 1 });

    expect(console.error).toHaveBeenCalledWith("broker-stub must run as a utility process");
    expect(process.exit).toHaveBeenCalledWith(1);
  });

  it("sends alive pings until a shutdown message exits cleanly", async () => {
    const parentPort = new FakeParentPort();
    setParentPort(parentPort);

    await importBrokerStubEntry();

    expect(parentPort.postMessage).toHaveBeenCalledTimes(1);
    expect(parentPort.postMessage).toHaveBeenCalledWith({ alive: true });

    await vi.advanceTimersByTimeAsync(999);
    expect(parentPort.postMessage).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(1);
    expect(parentPort.postMessage).toHaveBeenCalledTimes(2);

    expect(() => {
      parentPort.emit("message", { data: { type: "shutdown" } });
    }).toThrow(ProcessExitError);

    expect(process.exit).toHaveBeenCalledWith(0);

    await vi.advanceTimersByTimeAsync(1_000);
    expect(parentPort.postMessage).toHaveBeenCalledTimes(2);
  });
});

function setParentPort(parentPort: FakeParentPort | null): void {
  Object.defineProperty(process, "parentPort", {
    configurable: true,
    writable: true,
    value: parentPort,
  });
}

function restoreParentPort(): void {
  if (parentPortDescriptor === undefined) {
    Reflect.deleteProperty(process, "parentPort");
    return;
  }

  Object.defineProperty(process, "parentPort", parentPortDescriptor);
}

function restoreSignalListeners(): void {
  for (const signal of SIGNALS) {
    const originalListeners = originalSignalListeners.get(signal) ?? [];
    for (const listener of process.listeners(signal) as readonly ProcessListener[]) {
      if (!originalListeners.includes(listener)) {
        process.off(signal, listener);
      }
    }
  }
}

async function importBrokerStubEntry(): Promise<void> {
  await import("../src/main/broker-stub-entry.ts");
}
