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

import type { CostLedger } from "@wuphf/broker/cost-ledger";
import {
  type CostEventAuditPayload,
  type CostUnits,
  isProviderKind,
  MAX_COST_EVENT_AMOUNT_MICRO_USD,
  type MicroUsd,
  type ProviderKind,
} from "@wuphf/protocol";

import { Caps, type CapsConfig, DEFAULT_CAPS_CONFIG } from "./caps.ts";
import { DEFAULT_DEDUPE_CONFIG, DedupeCache, type DedupeConfig, hashRequest } from "./dedupe.ts";
import { BadRequestError, ProviderError, UnknownModelError } from "./errors.ts";
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

    const replay = dedupe.lookup(ctx, req);
    if (replay !== null) {
      return replay;
    }

    caps.preflightDailyCap();
    caps.preflightWakeCap(ctx.agentSlug);

    const provider = providers.get(req.model);
    if (provider === undefined) {
      throw new UnknownModelError(req.model);
    }

    // Compute the context-scoped request key once and pass it to the
    // provider. Adapters that implement provider-side idempotency
    // (Anthropic, OpenAI) forward this as the Idempotency-Key header
    // so two different agents sending the same prompt do not share a
    // server-side dedup window. Adapters that don't (stub, ollama)
    // ignore it. The hash matches the local dedupe key for symmetry.
    // See triangulation #3 finding B3-2.
    const reqWithKey: ProviderRequest = { ...req, requestKey: hashRequest(ctx, req) };

    let providerResponse: ProviderResponse;
    try {
      providerResponse = await provider.complete(reqWithKey);
    } catch (err) {
      // Caller-input errors (400/413/422) do NOT count as breaker
      // strikes — bad input from one caller shouldn't open the breaker
      // for the whole agent. See triangulation #2 finding B2-7.
      if (err instanceof BadRequestError) {
        throw err;
      }
      caps.recordError(ctx.agentSlug);
      if (err instanceof ProviderError || err instanceof UnknownModelError) {
        throw err;
      }
      throw new ProviderError(provider.kind, err);
    }

    // Estimator + ledger append are post-provider. A failure here means
    // we paid the provider but couldn't account for it. Treat that as a
    // breaker-worthy event so a sustained estimator/ledger fault opens
    // the circuit instead of letting the gateway keep spending while
    // every accounting layer reads zero. See triangulation finding H5.
    let costMicroUsd: MicroUsd;
    let appended: ReturnType<CostLedger["appendCostEvent"]>;
    try {
      costMicroUsd = provider.costEstimator.estimate(req.model, providerResponse.usage);
      // Defense-in-depth (#824): MicroUsd allows up to $1M (budget limit
      // ceiling), but a single cost_event is capped at $100. Catch the
      // out-of-range case here so the breaker reacts to a runaway
      // estimator instead of letting the codec reject AFTER the paid
      // provider call has already happened.
      if ((costMicroUsd as number) > MAX_COST_EVENT_AMOUNT_MICRO_USD) {
        throw new Error(
          `cost estimate ${String(costMicroUsd)} exceeds per-event cap ${MAX_COST_EVENT_AMOUNT_MICRO_USD}`,
        );
      }

      // The "row before response" point: appendCostEvent is the only
      // place an EventLsn for this completion is produced. If this
      // throws, the response is discarded — we do NOT return a
      // completion without a matching ledger row.
      const payload: CostEventAuditPayload = buildCostEventPayload(
        ctx,
        // Audit-stable model id (#827): prefer the served snapshot the
        // SDK echoed back (e.g. claude-haiku-4-5-20251001) over the
        // request alias (claude-haiku-4-5). Adapters that don't expose
        // it fall back to req.model so the gateway path stays uniform.
        providerResponse.model ?? req.model,
        provider.kind,
        providerResponse.usage,
        costMicroUsd,
        deps.nowMs(),
      );
      appended = deps.ledger.appendCostEvent(payload);
    } catch (err) {
      caps.recordError(ctx.agentSlug);
      throw err;
    }

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
      ...(providerResponse.finishReason !== undefined
        ? { finishReason: providerResponse.finishReason }
        : {}),
      ...(providerResponse.refusal !== undefined ? { refusal: providerResponse.refusal } : {}),
    };
    dedupe.store(ctx, req, result);
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
  model: string,
  providerKind: ProviderKind,
  usage: CostUnits,
  costMicroUsd: MicroUsd,
  nowMs: number,
): CostEventAuditPayload {
  // Pull only the fields cost.ts validates; payload keys not listed in
  // COST_EVENT_KEYS are rejected by the validator (see protocol cost.ts).
  // `providerKind` comes from the resolved provider so the audit row
  // records who actually fulfilled the request — no hard-coded mapping.
  const base: CostEventAuditPayload = {
    agentSlug: ctx.agentSlug,
    providerKind,
    model,
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
 * Resolve a provider for a model name by exact lookup against each
 * provider's `models` list. Construction-time collision: if two
 * providers register the same model, throw immediately so the host
 * sees the conflict at gateway init rather than at first call.
 */
function indexProvidersByModel(providers: readonly Provider[]): Map<string, Provider> {
  const out = new Map<string, Provider>();
  for (const provider of providers) {
    // Defense-in-depth (#828): Provider.kind is a brand, but a custom
    // provider can forge it with `as ProviderKind`. Verify at construction
    // so a bad kind cannot reach a billable provider.complete() call —
    // the ledger codec would otherwise reject AFTER the paid call.
    if (!isProviderKind(provider.kind)) {
      throw new Error(
        `invalid Provider.kind: ${JSON.stringify(provider.kind)} is not a registered ProviderKind`,
      );
    }
    for (const model of provider.models) {
      const existing = out.get(model);
      if (existing !== undefined && existing !== provider) {
        throw new Error(
          `provider model collision: ${JSON.stringify(model)} claimed by ${existing.kind} and ${provider.kind}`,
        );
      }
      out.set(model, provider);
    }
  }
  return out;
}

// Re-export for callers that compose their own deps without pulling each
// helper file individually.
export { Caps } from "./caps.ts";
export { DedupeCache } from "./dedupe.ts";
