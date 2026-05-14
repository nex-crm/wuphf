import type { ReadableStream } from "node:stream/web";

import type {
  AgentId,
  CostLedgerEntry,
  CredentialHandle,
  ProviderKind,
  ReceiptSnapshot,
  RunnerEvent,
  RunnerId,
  RunnerKind,
  RunnerSpawnRequest,
} from "@wuphf/protocol";
import type { RunnerEventRecord, RunnerEventStreamOptions } from "./internal/event-hub.ts";

export type Receipt = ReceiptSnapshot;

export interface AgentRunner {
  readonly id: RunnerId;
  readonly kind: RunnerKind;
  readonly agentId: AgentId;

  /** Returns a stream of events. Cancellation through .cancel() on the reader. */
  events(options?: RunnerEventStreamOptions): ReadableStream<RunnerEvent>;

  /** Returns events with durable event-log metadata for broker SSE framing. */
  eventRecords(options?: RunnerEventStreamOptions): ReadableStream<RunnerEventRecord>;

  /**
   * Terminates the runner. Idempotent. Returns when the subprocess or request
   * has actually exited, not when the signal was sent.
   *
   * Lifecycle state is owned by the runner's single state machine. Consumers
   * observe the `ReadableStream<RunnerEvent>` but do not write state, which
   * prevents the v0 status-write/event-drain race by construction.
   */
  terminate(opts?: { readonly gracePeriodMs?: number }): Promise<void>;
}

export interface RunnerSpawnDeps {
  readonly credential: CredentialHandle;
  /** Broker-resolved authoritative provider for cost and receipt attribution. */
  readonly resolvedProviderKind: ProviderKind;
  readonly secretReader: (h: CredentialHandle) => Promise<string>;
  readonly costLedger: { readonly record: (entry: CostLedgerEntry) => Promise<void> };
  readonly receiptStore: {
    readonly put: (receipt: Receipt) => Promise<{ readonly stored: boolean }>;
  };
  readonly eventLog: {
    readonly append: (event: RunnerEvent) => Promise<number>;
  };
}

export type RunnerEventLog = RunnerSpawnDeps["eventLog"];

export type SpawnAgentRunner = (
  request: RunnerSpawnRequest,
  deps: RunnerSpawnDeps,
) => Promise<AgentRunner>;
