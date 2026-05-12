// Gateway types — public surface.
//
// The "type-system enforced row-before-response" contract works like this:
// `Gateway.complete()` returns a `GatewayCompletionResult` that carries the
// `cost_event` LSN. Inside the gateway implementation, the only way to fill
// that LSN is to call `ledger.appendCostEvent()` first; a developer who
// short-circuits the ledger write has no LSN to return. The unit tests
// verify the LSN matches an actual row in `event_log`.

import type {
  AgentSlug,
  CostUnits,
  EventLsn,
  MicroUsd,
  ProviderKind,
  ReceiptId,
  TaskId,
} from "@wuphf/protocol";

/**
 * Caller context for every `Gateway.complete()` call. Identifies the agent
 * (drives wake-cap and per-agent budget routing), and optionally the task
 * and receipt the call is being made on behalf of (so the cost_event
 * payload can attribute spend).
 */
export interface SupervisorContext {
  readonly agentSlug: AgentSlug;
  readonly taskId?: TaskId;
  readonly receiptId?: ReceiptId;
}

/**
 * Wire-shape-agnostic request. Real provider SDKs receive a translated
 * subset of this in their own request types; the gateway only needs the
 * fields it must hash for dedupe and pass to the cost estimator.
 */
export interface ProviderRequest {
  readonly model: string;
  readonly prompt: string;
  readonly maxOutputTokens: number;
}

/**
 * What providers return. `usage` populates the cost_event's `units`
 * field directly so cache accounting (Anthropic-style) survives intact.
 */
export interface ProviderResponse {
  readonly text: string;
  readonly usage: CostUnits;
}

export interface CostEstimator {
  /**
   * Given a model name and post-call usage counters, return the
   * integer-micro-USD cost. Pure function; per-model pricing tables
   * live here. The estimator MUST NOT do I/O — it runs inside the
   * gateway's hot path and on the §10.4 burn-down.
   */
  estimate(model: string, usage: CostUnits): MicroUsd;
}

export interface Provider {
  readonly kind: ProviderKind;
  /**
   * Exact model IDs this provider handles. The gateway routes by exact
   * match of `ProviderRequest.model` against this list — NOT by `kind`.
   * Two providers can share a `kind` (e.g. real openai-compat + stub
   * both report `"openai-compat"` for audit purposes) without colliding
   * because each owns a disjoint set of model strings. See
   * triangulation finding H4.
   */
  readonly models: readonly string[];
  readonly costEstimator: CostEstimator;
  complete(req: ProviderRequest): Promise<ProviderResponse>;
}

/**
 * Result returned by `Gateway.complete()`. The presence of `costEventLsn`
 * is the public proof that the cost ledger row was written. Callers
 * should never have to handle a "completion without a cost row" case;
 * either the gateway returned a result with the LSN, or it threw.
 */
export interface GatewayCompletionResult {
  readonly text: string;
  readonly usage: CostUnits;
  readonly costMicroUsd: MicroUsd;
  readonly costEventLsn: EventLsn;
  /** True iff this response was served from the 60s dedupe cache. */
  readonly dedupeReplay: boolean;
}

export interface Gateway {
  complete(ctx: SupervisorContext, req: ProviderRequest): Promise<GatewayCompletionResult>;

  /**
   * Mark "human / scheduler activity just happened" so the idle clock
   * resets. The supervisor calls this on every inbound human action;
   * agent-initiated activity does NOT reset the clock (otherwise an
   * agent loop would keep itself awake indefinitely).
   */
  noteHumanActivity(): void;

  /**
   * Diagnostic snapshot of the gateway's process-local state (wake-cap
   * window, breaker state, idle window). Used by the §10.4 burn-down
   * and the cost-tile API. Not a fresh transaction with the ledger.
   */
  inspect(): GatewayInspection;
}

export interface GatewayInspection {
  readonly idleSinceMs: number | null;
  readonly idle: boolean;
  readonly perAgent: ReadonlyMap<string, AgentInspection>;
}

export interface AgentInspection {
  readonly recentWakeCount: number;
  readonly breaker: BreakerState;
}

export type BreakerState =
  | { readonly status: "closed"; readonly recentErrors: number }
  | { readonly status: "open"; readonly openedAtMs: number; readonly cooldownEndsMs: number };
