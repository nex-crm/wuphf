import {
  type AgentId,
  asRunnerId,
  type RunnerEvent,
  type RunnerId,
  type RunnerKind,
  type RunnerSpawnRequest,
} from "@wuphf/protocol";
import { RunnerEventHub } from "../internal/event-hub.ts";
import { LifecycleStateMachine } from "../lifecycle.ts";
import type { AgentRunner, SpawnAgentRunner } from "../runner.ts";

export interface FakeAgentRunner extends AgentRunner {
  emit(event: RunnerEvent): Promise<void>;
  close(result?: {
    readonly exitCode?: number | undefined;
    readonly error?: string | undefined;
  }): void;
}

export interface FakeRunnerOptions {
  readonly id?: RunnerId | undefined;
  readonly kind: RunnerKind;
  readonly agentId: AgentId;
  readonly onEvent?: ((event: RunnerEvent) => Promise<void>) | undefined;
}

export function createFakeAgentRunner(options: FakeRunnerOptions): FakeAgentRunner {
  const id = options.id ?? asRunnerId("run_fake0123456789ABCDEFGHIJKLMNOPQRST");
  const hub = new RunnerEventHub();
  const lifecycle = new LifecycleStateMachine(id);
  lifecycle.markRunning();
  let terminatePromise: Promise<void> | null = null;
  let closed = false;
  let lsn = 0;
  return {
    id,
    kind: options.kind,
    agentId: options.agentId,
    events: (streamOptions) => hub.events(streamOptions),
    eventRecords: (streamOptions) => hub.eventRecords(streamOptions),
    async emit(event) {
      await options.onEvent?.(event);
      lsn += 1;
      hub.publish(event, lsn);
    },
    async terminate() {
      if (terminatePromise === null) {
        terminatePromise = Promise.resolve().then(() => {
          lifecycle.beginStopping();
          lifecycle.markStopped({ exitCode: 0 });
          closed = true;
          hub.close();
        });
      }
      return terminatePromise;
    },
    close(result = { exitCode: 0 }) {
      if (closed) return;
      closed = true;
      lifecycle.markStopped(result);
      hub.close();
    },
  };
}

export function createFakeSpawnAgentRunner(
  args: {
    readonly id?: RunnerId | undefined;
    readonly events?: readonly RunnerEvent[] | undefined;
    readonly onSpawn?: ((request: RunnerSpawnRequest) => void) | undefined;
  } = {},
): SpawnAgentRunner {
  return async (request) => {
    args.onSpawn?.(request);
    const runner = createFakeAgentRunner({
      id: args.id,
      kind: request.kind,
      agentId: request.agentId,
    });
    queueMicrotask(() => {
      for (const event of args.events ?? []) {
        runner.emit(event).catch((err: unknown) => {
          // Surface emit failures so flaky test fixtures don't silently mask
          // real assertions when the hub rejects. Writes to stderr instead of
          // console.error to stay within the package's no-console policy.
          const message = err instanceof Error ? (err.stack ?? err.message) : String(err);
          process.stderr.write(`createFakeSpawnAgentRunner: runner.emit() failed: ${message}\n`);
        });
      }
    });
    return runner;
  };
}
