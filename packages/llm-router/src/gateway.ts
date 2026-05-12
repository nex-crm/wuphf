// Gateway implementation. The order inside `complete()` is the contract:
//
//   1. Validate SupervisorContext brands (cheap, rejects forged input).
//   2. Idle gate (cheap, runs before any state mutation).
//   3. Breaker gate (cheap; opens reject without consuming budget).
//   4. In-flight coalescing: if an identical call is in flight, await
//      its result with dedupeReplay=true. Closes the concurrent-call
//      bypass (round-1 adversarial BLOCK).
//   5. Dedupe lookup (60s window); replay short-circuits without budget.
//   6. Atomic daily-cap + wake-cap pre-flight WITH RESERVATION — pending
//      reservations count toward both caps so a burst of concurrent
//      calls cannot all pass the same stale snapshot.
//   7. Provider call.
//   8. Cost estimate + integer guard.
//   9. Ledger write — this is the "row before response" point. On
//      failure, the response is discarded and the error is re-thrown.
//  10. commitReservation (drops reservation, records wake) + recordSuccess.
//  11. Store result in dedupe cache.
//  12. Return GatewayCompletionResult with the LSN from step 9.
//
// Step 9 is the only place a successful `complete()` can mint a
// `costEventLsn` to put in the result. No other code path returns a
// completion. That's the type-system enforcement.

import type { CostLedger } from "@wuphf/broker/cost-ledger";
import {
  type CostEventAuditPayload,
  type CostUnits,
  isAgentSlug,
  isProviderKind,
  isReceiptId,
  isTaskId,
  MAX_COST_EVENT_AMOUNT_MICRO_USD,
  MAX_COST_MODEL_BYTES,
  type MicroUsd,
  type ProviderKind,
} from "@wuphf/protocol";

import { Caps, type CapsConfig, DEFAULT_CAPS_CONFIG, type Reservation } from "./caps.ts";
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
    // Validate brands at the entry: a caller that forges `agentSlug`,
    // `taskId`, or `receiptId` with `as` casts can otherwise sail past
    // every gate and only get rejected at audit-payload serialization
    // — after the paid provider call. The check is cheap (string regex
    // per field) and runs before any state mutation.
    validateSupervisorContext(ctx);

    caps.preflightIdle();
    caps.preflightBreaker(ctx.agentSlug);

    // In-flight coalescing: if an identical call is already in flight,
    // share its eventual result. Two concurrent identical complete()
    // calls share ONE paid provider call and ONE cost_event row.
    const pending = dedupe.lookupInFlight(ctx, req);
    if (pending !== null) {
      const original = await pending;
      return { ...original, dedupeReplay: true };
    }

    const replay = dedupe.lookup(ctx, req);
    if (replay !== null) {
      return replay;
    }

    const provider = providers.get(req.model);
    if (provider === undefined) {
      throw new UnknownModelError(req.model);
    }

    // Pessimistic cost estimate for the reservation. Uses
    // `req.maxOutputTokens` as a worst-case for both directions; the
    // provider's own estimator handles the per-model rate so the
    // reservation reflects the model's pricing. The actual cost from
    // the response usage is what gets billed.
    const reservationEstimate = pessimisticReservationMicroUsd(provider, req);
    const reservation = caps.reserveAndPreflight(ctx.agentSlug, reservationEstimate);

    // Run the provider call and ledger append inside a promise that we
    // register as in-flight so any concurrent identical call can coalesce.
    const promise = doProviderCallAndLedger(provider, ctx, req, reservation);
    dedupe.registerInFlight(ctx, req, promise);
    try {
      return await promise;
    } finally {
      dedupe.clearInFlight(ctx, req);
    }
  }

  async function doProviderCallAndLedger(
    provider: Provider,
    ctx: SupervisorContext,
    req: ProviderRequest,
    reservation: Reservation,
  ): Promise<GatewayCompletionResult> {
    // Context-scoped request key. Adapters that implement provider-side
    // idempotency (Anthropic, OpenAI) forward this as the Idempotency-Key
    // header so two different agents sending the same prompt do not share
    // a server-side dedup window. Adapters that don't (stub, ollama)
    // ignore it. The hash matches the local dedupe key for symmetry.
    const reqWithKey: ProviderRequest = { ...req, requestKey: hashRequest(ctx, req) };

    let providerResponse: ProviderResponse;
    try {
      providerResponse = await provider.complete(reqWithKey);
    } catch (err) {
      caps.releaseReservation(reservation);
      // Caller-input errors (400/413/422) do NOT count as breaker
      // strikes — bad input from one caller shouldn't open the breaker
      // for the whole agent.
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
    // every accounting layer reads zero.
    let costMicroUsd: MicroUsd;
    let appended: ReturnType<CostLedger["appendCostEvent"]>;
    try {
      costMicroUsd = provider.costEstimator.estimate(req.model, providerResponse.usage);
      // The estimator returns a branded MicroUsd, but the brand is a
      // compile-time guarantee — a forged `as MicroUsd` cast can produce
      // NaN/floats/negatives. Validate at runtime so a bad estimator can
      // never produce a payload the codec will reject AFTER the provider
      // has already billed.
      validateCostEstimate(costMicroUsd);

      // Audit-stable model id: prefer the served snapshot the SDK echoed
      // back (e.g. claude-haiku-4-5-20251001) over the request alias
      // (claude-haiku-4-5). Adapters that don't expose it fall back to
      // req.model so the gateway path stays uniform.
      const auditModel = providerResponse.model ?? req.model;
      validateAuditModel(auditModel);

      // The "row before response" point: appendCostEvent is the only
      // place an EventLsn for this completion is produced. If this
      // throws, the response is discarded — we do NOT return a
      // completion without a matching ledger row.
      const payload: CostEventAuditPayload = buildCostEventPayload(
        ctx,
        auditModel,
        provider.kind,
        providerResponse.usage,
        costMicroUsd,
        deps.nowMs(),
      );
      appended = deps.ledger.appendCostEvent(payload);
    } catch (err) {
      caps.releaseReservation(reservation);
      caps.recordError(ctx.agentSlug);
      throw err;
    }

    // Commit the reservation (drops it, records the wake) and mark
    // success in the breaker AFTER the ledger commit so a crash
    // mid-commit doesn't burn a wake slot that the ledger never saw.
    caps.commitReservation(reservation);
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
 *
 * Also validates `Provider.kind` against the protocol's closed
 * `ProviderKind` enum: the brand is a compile-time guarantee, but a
 * custom provider can forge it with `as ProviderKind`. If a forged
 * kind reaches `provider.complete()`, the ledger codec rejects only
 * AFTER the paid call has happened — that's the failure mode we're
 * preventing here.
 */
function indexProvidersByModel(providers: readonly Provider[]): Map<string, Provider> {
  const out = new Map<string, Provider>();
  for (const provider of providers) {
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

/**
 * Branded identifiers (`AgentSlug`, `TaskId`, `ReceiptId`) carry a
 * compile-time guarantee, but a caller that uses `as AgentSlug` (or
 * builds an object with no `as*` constructor) can sneak invalid strings
 * past tsc. Reject at the gateway boundary so the cost-event codec
 * never has to reject AFTER the paid provider call.
 */
function validateSupervisorContext(ctx: SupervisorContext): void {
  if (!isAgentSlug(ctx.agentSlug)) {
    throw new Error(`invalid SupervisorContext.agentSlug: ${JSON.stringify(ctx.agentSlug)}`);
  }
  if (ctx.taskId !== undefined && !isTaskId(ctx.taskId)) {
    throw new Error(`invalid SupervisorContext.taskId: ${JSON.stringify(ctx.taskId)}`);
  }
  if (ctx.receiptId !== undefined && !isReceiptId(ctx.receiptId)) {
    throw new Error(`invalid SupervisorContext.receiptId: ${JSON.stringify(ctx.receiptId)}`);
  }
}

/**
 * Validate the estimator's return value before it reaches the ledger
 * codec. The `MicroUsd` brand is decorative at runtime — `NaN`, floats,
 * negatives, and amounts > MAX_COST_EVENT_AMOUNT_MICRO_USD can all be
 * forged with `as MicroUsd`. Throwing here triggers the
 * breaker-eligible failure path (paid provider, no ledger row) which
 * opens the breaker after the configured threshold instead of letting
 * the gateway keep spending while accounting silently drops rows.
 */
function validateCostEstimate(value: MicroUsd): void {
  const n = value as unknown as number;
  if (typeof n !== "number" || !Number.isSafeInteger(n) || n < 0) {
    throw new Error(
      `invalid cost estimate: ${String(n)} — must be a non-negative safe integer μUSD`,
    );
  }
  if (n > MAX_COST_EVENT_AMOUNT_MICRO_USD) {
    throw new Error(
      `cost estimate ${n} exceeds per-event cap ${MAX_COST_EVENT_AMOUNT_MICRO_USD} μUSD`,
    );
  }
}

/**
 * Model id byte length bound. Mirrors the protocol's
 * `MAX_COST_MODEL_BYTES` (128 UTF-8 bytes) so the gateway rejects
 * oversized model ids BEFORE the audit payload is built. Round-2 fix:
 * the round-1 implementation used `model.length` (JS chars) bounded
 * at 256 — a 129-char ASCII model would pass the gateway and then fail
 * codec encoding. Use UTF-8 byte length and the protocol-exported cap.
 */
const MODEL_BYTE_ENCODER = new TextEncoder();

function validateAuditModel(model: string): void {
  if (typeof model !== "string" || model.length === 0) {
    throw new Error(
      `invalid audit model id: ${typeof model === "string" ? "empty" : typeof model}`,
    );
  }
  const byteLen = MODEL_BYTE_ENCODER.encode(model).length;
  if (byteLen > MAX_COST_MODEL_BYTES) {
    throw new Error(
      `invalid audit model id: ${byteLen} UTF-8 bytes exceeds MAX_COST_MODEL_BYTES (${MAX_COST_MODEL_BYTES})`,
    );
  }
}

/**
 * Conservative character-to-token ratio for the input estimate. Real
 * tokenization (Anthropic, GPT) sits in the 3-4 chars/token range for
 * English prose and lower for code; we use 3 to stay on the side of
 * over-reservation rather than under-reservation. This is only used to
 * bound the cap reservation — actual billing comes from the provider's
 * response usage.
 */
const CHARS_PER_INPUT_TOKEN_ESTIMATE = 3;

/**
 * Pessimistic worst-case cost estimate for a request, used to reserve
 * cap headroom before the provider call. Round-2 fix: the round-1
 * implementation used `maxOutputTokens` for BOTH input and output token
 * estimates, which silently underreserves when the prompt is large and
 * `maxOutputTokens` is small. A request with a 100k-character prompt
 * (~33k tokens) and `maxOutputTokens=64` would reserve as if input were
 * only 64 tokens — concurrent distinct large-prompt calls can then all
 * pass `reserveAndPreflight` and overshoot the daily cap.
 *
 * Now: input tokens are estimated from `prompt.length` using a
 * conservative chars-per-token ratio (`CHARS_PER_INPUT_TOKEN_ESTIMATE`).
 * Output tokens are still bounded by `maxOutputTokens`. The estimator
 * gets called with the larger of the two as a defensive ceiling.
 *
 * Real cost from the provider's response usage is what gets billed; the
 * reservation just ensures concurrent calls can't all pass a stale
 * snapshot of `cost_by_agent`.
 */
function pessimisticReservationMicroUsd(provider: Provider, req: ProviderRequest): number {
  const outputTokens = Math.max(0, Math.floor(req.maxOutputTokens));
  const promptLen = typeof req.prompt === "string" ? req.prompt.length : 0;
  const inputTokens = Math.ceil(promptLen / CHARS_PER_INPUT_TOKEN_ESTIMATE);
  // Use the provider's own estimator so each adapter's pricing applies.
  // Cache fields are zeroed — a fresh-input worst case bills at the
  // higher (non-cached) rate.
  try {
    const estimate = provider.costEstimator.estimate(req.model, {
      inputTokens,
      outputTokens,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    }) as unknown as number;
    if (typeof estimate !== "number" || !Number.isSafeInteger(estimate) || estimate < 0) {
      return 0;
    }
    return Math.min(estimate, MAX_COST_EVENT_AMOUNT_MICRO_USD);
  } catch {
    // Estimator threw (unknown model already filtered upstream, but
    // belt-and-suspenders). Use 0 — the real check still runs after
    // the provider call.
    return 0;
  }
}

// Re-export for callers that compose their own deps without pulling each
// helper file individually.
export { Caps } from "./caps.ts";
export { DedupeCache } from "./dedupe.ts";
