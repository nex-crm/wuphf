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
  return {
    id,
    kind: options.kind,
    agentId: options.agentId,
    events: () => hub.events(),
    async emit(event) {
      await options.onEvent?.(event);
      hub.publish(event);
    },
    async terminate() {
      if (terminatePromise === null) {
        terminatePromise = Promise.resolve().then(() => {
          lifecycle.beginStopping();
          lifecycle.markStopped({ exitCode: 0 });
          hub.close();
        });
      }
      return terminatePromise;
    },
    close(result = { exitCode: 0 }) {
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
        runner.emit(event).catch(() => undefined);
      }
    });
    return runner;
  };
}
