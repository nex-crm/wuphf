import { ReadableStream } from "node:stream/web";

import type { RunnerEvent } from "@wuphf/protocol";

export const DEFAULT_MAX_EVENT_HISTORY = 1_000;

export class RunnerEventHub {
  readonly #history: RunnerEvent[] = [];
  readonly #controllers = new Set<ReadableStreamDefaultController<RunnerEvent>>();
  #closed = false;

  constructor(readonly maxHistory = DEFAULT_MAX_EVENT_HISTORY) {}

  events(): ReadableStream<RunnerEvent> {
    let activeController: ReadableStreamDefaultController<RunnerEvent> | null = null;
    return new ReadableStream<RunnerEvent>({
      start: (controller) => {
        activeController = controller;
        for (const event of this.#history) {
          controller.enqueue(event);
        }
        if (this.#closed) {
          controller.close();
          return;
        }
        this.#controllers.add(controller);
      },
      cancel: () => {
        if (activeController !== null) {
          this.#controllers.delete(activeController);
        }
      },
    });
  }

  publish(event: RunnerEvent): void {
    if (this.#closed) return;
    this.#history.push(event);
    if (this.#history.length > this.maxHistory) {
      this.#history.shift();
    }
    for (const controller of this.#controllers) {
      try {
        controller.enqueue(event);
      } catch {
        this.#controllers.delete(controller);
      }
    }
  }

  close(): void {
    if (this.#closed) return;
    this.#closed = true;
    for (const controller of this.#controllers) {
      try {
        controller.close();
      } catch {
        // Already closed by a consumer cancellation.
      }
    }
    this.#controllers.clear();
  }
}
