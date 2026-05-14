import { asRunnerId, type RunnerEvent } from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import {
  RunnerEventHub,
  RunnerResumeWindowExpired,
  SerializedEmitter,
} from "../../src/internal/event-hub.ts";

const runnerId = asRunnerId("run_0123456789ABCDEFGHIJKLMNOPQRSTUV");

describe("RunnerEventHub", () => {
  it("disconnects a subscriber whose unread backlog exceeds the bound", async () => {
    const hub = new RunnerEventHub(10, 2);
    const stream = hub.eventRecords();
    for (let index = 0; index < 4; index += 1) {
      hub.publish(stdout(`chunk-${index}`), index + 1);
    }

    const reader = stream.getReader();
    const events: RunnerEvent[] = [];
    while (true) {
      const next = await reader.read();
      if (next.done) break;
      events.push(next.value.event);
    }

    expect(events.map((event) => event.kind)).toEqual(["stdout", "stdout", "stdout", "failed"]);
    const failed = events.at(-1);
    expect(failed?.kind).toBe("failed");
    if (failed?.kind === "failed") {
      expect(failed.error).toContain("subscriber_backpressure_exceeded");
      expect(failed.code).toBe("subscriber_backpressure_exceeded");
    }
  });

  it("rejects resume requests before the retained LSN window", () => {
    const hub = new RunnerEventHub(2, 10);
    hub.publish(stdout("one"), 1);
    hub.publish(stdout("two"), 2);
    hub.publish(stdout("three"), 3);

    expect(() => hub.eventRecords({ afterLsn: 0 })).toThrow(RunnerResumeWindowExpired);
  });

  it("serializes durable appends in FIFO order", async () => {
    const hub = new RunnerEventHub();
    const appended: string[] = [];
    let nextLsn = 0;
    const emitter = new SerializedEmitter({
      eventHub: hub,
      eventLog: {
        append: async (event) => {
          await Promise.resolve();
          appended.push(event.kind === "stdout" ? event.chunk : event.kind);
          nextLsn += 1;
          return nextLsn;
        },
      },
    });

    await Promise.all([
      emitter.emit(stdout("one")),
      emitter.emit(stdout("two")),
      emitter.emit(stdout("three")),
    ]);

    expect(appended).toEqual(["one", "two", "three"]);
  });
});

function stdout(chunk: string): RunnerEvent {
  return {
    kind: "stdout",
    runnerId,
    chunk,
    at: "2026-05-08T18:00:00.000Z",
  };
}
