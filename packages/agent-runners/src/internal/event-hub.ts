import { ReadableStream } from "node:stream/web";

import type { RunnerEvent } from "@wuphf/protocol";

export const DEFAULT_MAX_EVENT_HISTORY = 1_000;
export const DEFAULT_MAX_SUBSCRIBER_BACKLOG = 1_000;

export interface RunnerEventRecord {
  readonly event: RunnerEvent;
  readonly lsn?: number | undefined;
}

interface SubscriberState {
  readonly controller: ReadableStreamDefaultController<RunnerEventRecord>;
  backlog: number;
}

export interface RunnerEventStreamOptions {
  readonly afterLsn?: number | undefined;
}

export class RunnerResumeWindowExpired extends Error {
  override readonly name = "RunnerResumeWindowExpired";

  constructor(readonly oldestAvailableLsn: number) {
    super(`runner resume window expired before LSN ${oldestAvailableLsn}`);
  }
}

export class RunnerEventHub {
  readonly #history: RunnerEventRecord[] = [];
  readonly #subscribers = new Map<
    ReadableStreamDefaultController<RunnerEventRecord>,
    SubscriberState
  >();
  #closed = false;

  constructor(
    readonly maxHistory = DEFAULT_MAX_EVENT_HISTORY,
    readonly maxSubscriberBacklog = DEFAULT_MAX_SUBSCRIBER_BACKLOG,
  ) {}

  events(options: RunnerEventStreamOptions = {}): ReadableStream<RunnerEvent> {
    const records = this.eventRecords(options);
    return new ReadableStream<RunnerEvent>({
      async start(controller) {
        const reader = records.getReader();
        try {
          while (true) {
            const next = await reader.read();
            if (next.done) break;
            controller.enqueue(next.value.event);
          }
          controller.close();
        } catch (error) {
          controller.error(error);
        }
      },
      cancel() {
        return records.cancel();
      },
    });
  }

  eventRecords(options: RunnerEventStreamOptions = {}): ReadableStream<RunnerEventRecord> {
    const oldestAvailableLsn = this.oldestAvailableLsn();
    if (
      options.afterLsn !== undefined &&
      oldestAvailableLsn !== undefined &&
      options.afterLsn < oldestAvailableLsn - 1
    ) {
      throw new RunnerResumeWindowExpired(oldestAvailableLsn);
    }
    let activeController: ReadableStreamDefaultController<RunnerEventRecord> | null = null;
    return new ReadableStream<RunnerEventRecord>({
      start: (controller) => {
        activeController = controller;
        for (const record of this.#history) {
          if (
            options.afterLsn !== undefined &&
            record.lsn !== undefined &&
            record.lsn <= options.afterLsn
          ) {
            continue;
          }
          controller.enqueue(record);
        }
        if (this.#closed) {
          controller.close();
          return;
        }
        this.#subscribers.set(controller, { controller, backlog: 0 });
      },
      cancel: () => {
        if (activeController !== null) {
          this.#subscribers.delete(activeController);
        }
      },
    });
  }

  publish(event: RunnerEvent, lsn?: number | undefined): void {
    if (this.#closed) return;
    const record = lsn === undefined ? { event } : { event, lsn };
    this.#history.push(record);
    if (this.#history.length > this.maxHistory) {
      this.#history.shift();
    }
    for (const state of this.#subscribers.values()) {
      const controller = state.controller;
      try {
        if ((controller.desiredSize ?? 1) <= 0) {
          state.backlog += 1;
        } else {
          state.backlog = 0;
        }
        if (state.backlog > this.maxSubscriberBacklog) {
          controller.enqueue({ event: disconnectEvent(event, this.maxSubscriberBacklog) });
          controller.close();
          this.#subscribers.delete(controller);
          continue;
        }
        controller.enqueue(record);
      } catch {
        this.#subscribers.delete(controller);
      }
    }
  }

  close(): void {
    if (this.#closed) return;
    this.#closed = true;
    for (const controller of this.#subscribers.keys()) {
      try {
        controller.close();
      } catch {
        // Already closed by a consumer cancellation.
      }
    }
    this.#subscribers.clear();
  }

  oldestAvailableLsn(): number | undefined {
    for (const record of this.#history) {
      if (record.lsn !== undefined) return record.lsn;
    }
    return undefined;
  }
}

export interface SerializedEmitterDeps {
  readonly eventLog: {
    readonly append: (event: RunnerEvent) => Promise<number>;
  };
  readonly eventHub: RunnerEventHub;
}

export class SerializedEmitter {
  readonly #eventLog: SerializedEmitterDeps["eventLog"];
  readonly #eventHub: RunnerEventHub;
  #closed = false;
  #tail: Promise<void> = Promise.resolve();

  constructor(deps: SerializedEmitterDeps) {
    this.#eventLog = deps.eventLog;
    this.#eventHub = deps.eventHub;
  }

  emit(event: RunnerEvent): Promise<void> {
    if (this.#closed) {
      return Promise.reject(new Error("serialized emitter is closed"));
    }
    const job = this.#tail.then(async () => {
      const lsn = await this.#eventLog.append(event);
      this.#eventHub.publish(event, lsn);
    });
    this.#tail = job.catch(() => undefined);
    return job;
  }

  async close(): Promise<void> {
    this.#closed = true;
    await this.#tail;
  }
}

function disconnectEvent(source: RunnerEvent, maxBacklog: number): RunnerEvent {
  return {
    kind: "failed",
    runnerId: source.runnerId,
    error: JSON.stringify({ reason: "subscriber_backpressure_exceeded", maxBacklog }),
    code: "subscriber_backpressure_exceeded",
    at: source.at,
  };
}
