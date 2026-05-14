import type { RunnerId } from "@wuphf/protocol";

import { RunnerLifecycleError } from "./errors.ts";

export type LifecyclePhase = "pending" | "running" | "stopping" | "stopped";

export interface LifecycleSnapshot {
  readonly runnerId: RunnerId;
  readonly phase: LifecyclePhase;
  readonly exitCode?: number | undefined;
  readonly error?: string | undefined;
}

/**
 * Single-writer runner lifecycle state.
 *
 * v0 raced a status write against event drain: one goroutine could observe
 * stale state while another was still draining output. v1 makes the owner
 * fiber the only writer and limits transitions to
 * `Pending -> Running -> Stopping -> Stopped`; subscribers only read
 * `RunnerEvent` streams and cannot mutate lifecycle state. `terminate()`
 * waits on the owner fiber's stopped promise, not on signal delivery.
 */
export class LifecycleStateMachine {
  readonly #runnerId: RunnerId;
  #phase: LifecyclePhase = "pending";
  #exitCode: number | undefined;
  #error: string | undefined;
  #stoppedResolve: (() => void) | null = null;
  readonly #stopped: Promise<void>;

  constructor(runnerId: RunnerId) {
    this.#runnerId = runnerId;
    this.#stopped = new Promise((resolve) => {
      this.#stoppedResolve = resolve;
    });
  }

  snapshot(): LifecycleSnapshot {
    return {
      runnerId: this.#runnerId,
      phase: this.#phase,
      ...(this.#exitCode === undefined ? {} : { exitCode: this.#exitCode }),
      ...(this.#error === undefined ? {} : { error: this.#error }),
    };
  }

  stopped(): Promise<void> {
    return this.#stopped;
  }

  markRunning(): void {
    if (this.#phase !== "pending") {
      throw new RunnerLifecycleError(`cannot mark running from ${this.#phase}`);
    }
    this.#phase = "running";
  }

  beginStopping(): boolean {
    if (this.#phase === "stopped") return false;
    if (this.#phase === "stopping") return false;
    this.#phase = "stopping";
    return true;
  }

  markStopped(result: {
    readonly exitCode?: number | undefined;
    readonly error?: string | undefined;
  }): void {
    if (this.#phase === "stopped") return;
    this.#phase = "stopped";
    this.#exitCode = result.exitCode;
    this.#error = result.error;
    this.#stoppedResolve?.();
    this.#stoppedResolve = null;
  }
}
