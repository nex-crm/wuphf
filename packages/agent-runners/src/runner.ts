import type { ReadableStream } from "node:stream/web";

import type {
  AgentId,
  CostLedgerEntry,
  CredentialHandle,
  ReceiptSnapshot,
  RunnerEvent,
  RunnerId,
  RunnerKind,
  RunnerSpawnRequest,
} from "@wuphf/protocol";

export type Receipt = ReceiptSnapshot;

export interface AgentRunner {
  readonly id: RunnerId;
  readonly kind: RunnerKind;
  readonly agentId: AgentId;

  /** Returns a stream of events. Cancellation through .cancel() on the reader. */
  events(): ReadableStream<RunnerEvent>;

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
  readonly secretReader: (h: CredentialHandle) => Promise<string>;
  readonly costLedger: { readonly record: (entry: CostLedgerEntry) => Promise<void> };
  readonly receiptStore: {
    readonly put: (receipt: Receipt) => Promise<{ readonly stored: boolean }>;
  };
  readonly eventLog: {
    readonly append: (event: RunnerEvent) => Promise<void>;
  };
}

export type SpawnAgentRunner = (
  request: RunnerSpawnRequest,
  deps: RunnerSpawnDeps,
) => Promise<AgentRunner>;
