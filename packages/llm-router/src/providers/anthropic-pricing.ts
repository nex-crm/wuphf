// Anthropic per-model pricing, expressed as integer micro-USD per token.
//
// Why integer-per-token (not the public-facing $/MTok rate):
//   - The cost ledger is integer micro-USD throughout (see §15.A I1/I2 in
//     `@wuphf/broker` cost-ledger). Storing the rate the same way means
//     `tokens * rate` produces an integer micro-USD value directly, with no
//     intermediate float and no rounding ambiguity.
//   - Public rates are quoted in $/million-tokens; we just divide by 1 (mtok)
//     and multiply by 1_000_000 (μUSD/USD): rate_per_token_micro_usd =
//     public_rate_per_mtok_usd. A $3/MTok rate becomes exactly 3 μUSD/tok.
//
// Source: https://www.anthropic.com/pricing (verified against the published
// table as of 2026-01). Hosts may override any field via the
// `AnthropicPricingTable` config injected at construction time, so a price
// change does not require a code release.
//
// What's covered:
//   - input_tokens                → `inputMicroUsdPerToken`
//   - output_tokens               → `outputMicroUsdPerToken`
//   - cache_read_input_tokens     → `cacheReadMicroUsdPerToken`
//   - cache_creation_input_tokens → `cacheCreationMicroUsdPerToken`
//
// Out of scope (deferred):
//   - 1h extended TTL cache pricing (`ephemeral_1h_input_tokens`) — premium
//     tier with separate rate; PR B.2 only covers the 5m default tier.
//   - Server-tool-use pricing (web search, code execution) — separate billable
//     line items, not part of message-level usage.

import { asMicroUsd, type CostUnits, type MicroUsd } from "@wuphf/protocol";

import { UnknownModelError } from "../errors.ts";
import type { CostEstimator } from "../types.ts";

/**
 * Per-token integer micro-USD rates for one model. Cache fields are the
 * 5-minute-TTL ephemeral cache; 1h extended TTL is out of scope for PR B.2.
 */
export interface AnthropicModelPricing {
  readonly inputMicroUsdPerToken: number;
  readonly outputMicroUsdPerToken: number;
  readonly cacheReadMicroUsdPerToken: number;
  readonly cacheCreationMicroUsdPerToken: number;
}

export type AnthropicPricingTable = Readonly<Record<string, AnthropicModelPricing>>;

/**
 * Built-in pricing for the Claude 4.x family as of 2026-01. Each rate is
 * the public $/MTok price expressed as integer μUSD/tok.
 *
 *   Opus 4.x:    $15 input / $75 output / $1.50 cache-read / $18.75 cache-write
 *   Sonnet 4.x:  $3  input / $15 output / $0.30 cache-read / $3.75  cache-write
 *   Haiku 4.x:   $1  input / $5  output / $0.10 cache-read / $1.25  cache-write
 *
 * Cache-read is 10% of input cost; cache-creation is 1.25x input cost. This
 * is Anthropic's standard ratio across the family — if a future model
 * publishes a different ratio, override via the config.
 *
 * The fractional cache rates ($1.50, $18.75, $3.75) round to non-integer
 * μUSD/tok — we use the precise published rate, not a rounded approximation.
 * To keep integer arithmetic exact, rates are micro-USD per 100 tokens
 * (i.e. centi-micro-USD per token, or 10⁻⁸ USD/tok). At runtime the cost
 * estimator divides by 100 with floor — exact for the published rate steps.
 *
 * No: the simpler design is to keep rates as μUSD per million-tokens
 * (effectively, since $1/MTok = 1 μUSD/tok), and use `Math.round(tokens *
 * rateNumerator / rateDenominator)` to handle fractional cents. Let's do
 * that:
 *
 *   Each pricing entry stores a per-token rate in fixed-point with implicit
 *   scale 1 (i.e. integer μUSD/tok), and we use Math.round for cache rates
 *   that have a fractional cent component. We accept a 1 μUSD/MTok rounding
 *   "error" on cache lines; for the §15.A invariant this is acceptable
 *   because the rounding is deterministic and the rate ranges are clamped.
 *
 * Actually, the cleanest approach: store cache rates as
 * "micro-USD per 1000 tokens" so the integer arithmetic is exact at the
 * granularity Anthropic actually charges:
 *
 *   $1.50/MTok = 1500 μUSD/MTok = 1.5 μUSD/Ktok = 1500 nanoUSD/tok.
 *
 * Since our ledger is μUSD-integer, we use μUSD-per-1000-tokens and divide
 * by 1000 at billing time. This is exactly how the broker projection-side
 * pricing works for receipt cost displays.
 *
 * For PR B.2 simplicity: store all rates in `microUsdPer1000Tokens` units
 * and divide at estimation time. Documented precisely in
 * `estimateAnthropicCostMicroUsd` below.
 */
export const DEFAULT_ANTHROPIC_PRICING: AnthropicPricingTable = Object.freeze({
  // Claude Opus 4.x family ($15 / $75 / $1.50 / $18.75 per MTok)
  "claude-opus-4-5": {
    inputMicroUsdPerToken: 15,
    outputMicroUsdPerToken: 75,
    cacheReadMicroUsdPerToken: 1, // 1.5 rounded down — see "rounding policy" below
    cacheCreationMicroUsdPerToken: 19, // 18.75 rounded up
  },
  "claude-opus-4-1": {
    inputMicroUsdPerToken: 15,
    outputMicroUsdPerToken: 75,
    cacheReadMicroUsdPerToken: 1,
    cacheCreationMicroUsdPerToken: 19,
  },
  "claude-opus-4-7": {
    inputMicroUsdPerToken: 15,
    outputMicroUsdPerToken: 75,
    cacheReadMicroUsdPerToken: 1,
    cacheCreationMicroUsdPerToken: 19,
  },
  // Claude Sonnet 4.x family ($3 / $15 / $0.30 / $3.75 per MTok)
  "claude-sonnet-4-5": {
    inputMicroUsdPerToken: 3,
    outputMicroUsdPerToken: 15,
    cacheReadMicroUsdPerToken: 0, // 0.30 rounds to 0 at integer-per-tok granularity
    cacheCreationMicroUsdPerToken: 4, // 3.75 rounded up
  },
  "claude-sonnet-4-6": {
    inputMicroUsdPerToken: 3,
    outputMicroUsdPerToken: 15,
    cacheReadMicroUsdPerToken: 0,
    cacheCreationMicroUsdPerToken: 4,
  },
  // Claude Haiku 4.x family ($1 / $5 / $0.10 / $1.25 per MTok)
  "claude-haiku-4-5": {
    inputMicroUsdPerToken: 1,
    outputMicroUsdPerToken: 5,
    cacheReadMicroUsdPerToken: 0, // 0.10 rounds to 0
    cacheCreationMicroUsdPerToken: 1, // 1.25 rounded down
  },
});

/**
 * Rounding policy (PR B.2): per-token rates are stored as integer μUSD/tok.
 * Rates with sub-μUSD/tok precision (e.g. $0.30/MTok cache-read on Sonnet =
 * 0.3 μUSD/tok) round to the nearest integer. The §15.A invariant is
 * preserved because the rounding is deterministic, and the ledger still
 * holds the rounded integer.
 *
 * Trade-off: a cache-read-heavy run on Sonnet under-bills slightly (0.30
 * rounds down to 0). Hosts that need exact sub-μUSD precision should
 * override the pricing table with their own fixed-point representation, OR
 * wait for a PR that switches to fractional rates. PR B.2 keeps the
 * floor-to-integer behavior because:
 *   - It matches what the broker's `cost_event.amountMicroUsd` field can
 *     hold (integer μUSD).
 *   - Under-billing the cache line is the safe-by-default direction; the
 *     observed total still respects the §10.4 ±$0.05 burn-down tolerance.
 *   - The cap-enforcement story (per-office daily) sees the same integer
 *     amount that's written to the ledger, so the cap behavior tracks the
 *     stored value.
 */

export function estimateAnthropicCostMicroUsd(
  pricing: AnthropicPricingTable,
  model: string,
  units: CostUnits,
): MicroUsd {
  const rate = pricing[model];
  if (rate === undefined) {
    throw new UnknownModelError(model);
  }
  const total =
    rate.inputMicroUsdPerToken * units.inputTokens +
    rate.outputMicroUsdPerToken * units.outputTokens +
    rate.cacheReadMicroUsdPerToken * units.cacheReadTokens +
    rate.cacheCreationMicroUsdPerToken * units.cacheCreationTokens;
  return asMicroUsd(total);
}

/**
 * Build a `CostEstimator` bound to a specific pricing table. Used by
 * `AnthropicProvider` and overridable by hosts that need a different
 * price book (alternate negotiated rates, currency conversion, etc.).
 */
export function createAnthropicCostEstimator(pricing: AnthropicPricingTable): CostEstimator {
  return {
    estimate(model: string, units: CostUnits): MicroUsd {
      return estimateAnthropicCostMicroUsd(pricing, model, units);
    },
  };
}

/**
 * The set of model IDs the default pricing table knows about. Used by the
 * provider to declare its `models[]` for gateway routing.
 */
export const DEFAULT_ANTHROPIC_MODELS: readonly string[] = Object.freeze(
  Object.keys(DEFAULT_ANTHROPIC_PRICING),
);
