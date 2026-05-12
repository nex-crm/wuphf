// Gateway implementation. The order inside `complete()` is the contract:
//
//   1. Idle gate (cheap, runs before any state mutation).
//   2. Breaker gate (cheap; opens reject without consuming budget).
//   3. Dedupe lookup (cheap; replay short-circuits before wake/daily cost).
//   4. Daily cap pre-flight (reads ledger).
//   5. Wake cap pre-flight (in-memory).
//   6. Provider call.
//   7. Cost estimate.
//   8. Ledger write — this is the "row before response" point. On failure,
//      the response is discarded and the error is re-thrown.
//   9. Record wake + success in caps.
//  10. Store result in dedupe cache.
//  11. Return GatewayCompletionResult with the LSN from step 8.
//
// Step 8 is the only place a successful `complete()` can mint a
// `costEventLsn` to put in the result. No other code path returns a
// completion. That's the type-system enforcement.

import type { CostLedger } from "@wuphf/broker";
import {
  asProviderKind,
  type CostEventAuditPayload,
  type CostUnits,
  type MicroUsd,
  type ProviderKind,
} from "@wuphf/protocol";

import { Caps, type CapsConfig, DEFAULT_CAPS_CONFIG } from "./caps.ts";
import { DEFAULT_DEDUPE_CONFIG, DedupeCache, type DedupeConfig } from "./dedupe.ts";
import { ProviderError, UnknownModelError } from "./errors.ts";
import { isStubModel, STUB_MODEL_ERROR, STUB_MODEL_FIXED_COST } from "./providers/stub.ts";
import type {
  Gateway,
  GatewayCompletionResult,
  GatewayInspection,
  Provider,
  ProviderRequest,
  ProviderResponse,
  SupervisorContext,
} from "./types.ts";

export interface GatewayConfig {
  readonly caps?: Partial<CapsConfig>;
  readonly dedupe?: Partial<DedupeConfig>;
}

export interface GatewayDeps {
  readonly ledger: CostLedger;
  readonly providers: readonly Provider[];
  readonly nowMs: () => number;
  readonly config?: GatewayConfig;
}

export function createGateway(deps: GatewayDeps): Gateway {
  const capsConfig: CapsConfig = { ...DEFAULT_CAPS_CONFIG, ...(deps.config?.caps ?? {}) };
  const dedupeConfig: DedupeConfig = { ...DEFAULT_DEDUPE_CONFIG, ...(deps.config?.dedupe ?? {}) };
  const caps = new Caps({ ledger: deps.ledger, config: capsConfig, nowMs: deps.nowMs });
  const dedupe = new DedupeCache({ nowMs: deps.nowMs, config: dedupeConfig });
  const providers = indexProvidersByModel(deps.providers);

  async function complete(
    ctx: SupervisorContext,
    req: ProviderRequest,
  ): Promise<GatewayCompletionResult> {
    caps.preflightIdle();
    caps.preflightBreaker(ctx.agentSlug);

    const replay = dedupe.lookup(req);
    if (replay !== null) {
      return replay;
    }

    caps.preflightDailyCap();
    caps.preflightWakeCap(ctx.agentSlug);

    const provider = providers.get(req.model);
    if (provider === undefined) {
      throw new UnknownModelError(req.model);
    }

    let providerResponse: ProviderResponse;
    try {
      providerResponse = await provider.complete(req);
    } catch (err) {
      caps.recordError(ctx.agentSlug);
      if (err instanceof ProviderError || err instanceof UnknownModelError) {
        throw err;
      }
      throw new ProviderError(provider.kind, err);
    }

    const costMicroUsd: MicroUsd = provider.costEstimator.estimate(
      req.model,
      providerResponse.usage,
    );

    // The "row before response" point: appendCostEvent is the only place
    // an EventLsn for this completion is produced. If this throws, the
    // response is discarded — we do NOT return a completion without a
    // matching ledger row. The error is re-thrown so the caller sees
    // the underlying storage problem and can retry.
    const payload: CostEventAuditPayload = buildCostEventPayload(
      ctx,
      req,
      providerResponse.usage,
      costMicroUsd,
      deps.nowMs(),
    );
    const appended = deps.ledger.appendCostEvent(payload);

    // Caps housekeeping AFTER the ledger commit so a crash mid-commit
    // doesn't burn a wake slot that the ledger never saw.
    caps.recordWake(ctx.agentSlug);
    caps.recordSuccess(ctx.agentSlug);

    const result: GatewayCompletionResult = {
      text: providerResponse.text,
      usage: providerResponse.usage,
      costMicroUsd,
      costEventLsn: appended.lsn,
      dedupeReplay: false,
    };
    dedupe.store(req, result);
    return result;
  }

  function inspect(): GatewayInspection {
    dedupe.pruneExpired();
    return caps.inspect();
  }

  function noteHumanActivity(): void {
    caps.noteHumanActivity();
  }

  return { complete, inspect, noteHumanActivity };
}

function buildCostEventPayload(
  ctx: SupervisorContext,
  req: ProviderRequest,
  usage: CostUnits,
  costMicroUsd: MicroUsd,
  nowMs: number,
): CostEventAuditPayload {
  // Pull only the fields cost.ts validates; payload keys not listed in
  // COST_EVENT_KEYS are rejected by the validator (see protocol cost.ts).
  const base: CostEventAuditPayload = {
    agentSlug: ctx.agentSlug,
    providerKind: providerKindFor(req.model),
    model: req.model,
    amountMicroUsd: costMicroUsd,
    units: usage,
    occurredAt: new Date(nowMs),
  };
  // Omit undefined optionals — the protocol codec preserves absence (not
  // null), and this matches the canonical-JSON shape the audit chain
  // hashes against.
  if (ctx.taskId !== undefined && ctx.receiptId !== undefined) {
    return { ...base, taskId: ctx.taskId, receiptId: ctx.receiptId };
  }
  if (ctx.taskId !== undefined) {
    return { ...base, taskId: ctx.taskId };
  }
  if (ctx.receiptId !== undefined) {
    return { ...base, receiptId: ctx.receiptId };
  }
  return base;
}

/**
 * Resolve a provider for a model name. For PR B we route by exact-model
 * lookup; PR B.2 (real Anthropic SDK) will route by model prefix
 * (`claude-` → anthropic, `gpt-` → openai, etc.).
 */
function indexProvidersByModel(providers: readonly Provider[]): Map<string, Provider> {
  const out = new Map<string, Provider>();
  for (const provider of providers) {
    // PR B knows the stub models inline; the real-provider adapters in
    // PR B.2 / PR B.3 will register their own model lists. Until then,
    // the stub provider claims both stub-* models.
    if (provider.kind === "openai-compat") {
      out.set(STUB_MODEL_FIXED_COST, provider);
      out.set(STUB_MODEL_ERROR, provider);
    }
  }
  return out;
}

/**
 * Map an internal model name to its `ProviderKind`. The closed enum
 * lives in `@wuphf/protocol/receipt-types.ts`; adding a new kind is a
 * wire-shape change.
 */
function providerKindFor(model: string): ProviderKind {
  if (isStubModel(model)) {
    // Stubs masquerade as openai-compat so the audit payload's
    // providerKind stays inside the closed enum.
    return asProviderKind("openai-compat");
  }
  // PR B.2 will add prefix routing here; for PR B the stub is the only
  // working path and we fail loud on unknown models.
  throw new UnknownModelError(model);
}

// Re-export for callers that compose their own deps without pulling each
// helper file individually.
export { Caps } from "./caps.ts";
export { DedupeCache } from "./dedupe.ts";
