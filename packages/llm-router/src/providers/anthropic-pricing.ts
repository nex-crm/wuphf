// Anthropic per-model pricing, stored as integer micro-USD per million
// tokens (μUSD/MTok).
//
// Why this unit and not μUSD/tok?
//
//   Anthropic publishes rates in $/MTok (e.g. "$3/MTok input"). Storing
//   the rate at the same scale Anthropic publishes lets us copy public
//   prices in exact, then accumulate per call in μUSD-million-tokens
//   space and divide once at the end. That preserves sub-μUSD/tok
//   precision (cache reads at $0.30/MTok = 300 μUSD/MTok) instead of
//   floor-rounding to zero per token.
//
//   Earlier draft (PR B.2 round 1, commit 0e5c8c4b) stored rates as
//   μUSD/tok. Sonnet/Haiku cache-read rates at $0.30/$0.10 per MTok
//   rounded to 0 μUSD/tok, so a cache-heavy workload could spend
//   real money while `cost_event.amountMicroUsd` recorded 0 — bypassing
//   the daily cap. See triangulation #2 finding B2-2 (security BLOCK,
//   api/sre HIGH).
//
// All rates are integer μUSD per million tokens:
//
//   public price → integer rate
//   ─────────────────────────────
//   $15.00/MTok  → 15_000_000
//   $3.00/MTok   →  3_000_000
//   $1.00/MTok   →  1_000_000
//   $0.30/MTok   →    300_000
//   $0.10/MTok   →    100_000
//   $0.50/MTok   →    500_000
//   $6.25/MTok   →  6_250_000
//   $18.75/MTok  → 18_750_000
//
// Per-call cost is:
//
//   sum_micro_million = sum(tokens_i * rate_i)
//   amountMicroUsd    = (sum_micro_million + 500_000) / 1_000_000  (integer)
//
// The +500_000 / 1_000_000 is round-half-up; deterministic, matches
// the §15.A integer-math invariant, and never produces a sub-μUSD
// rounding loss greater than 0.5 μUSD per total call.
//
// Pricing source: https://www.anthropic.com/pricing (verified 2026-05-12
// against the public table). Hosts override any rate via the
// `AnthropicPricingTable` config injected at construction time — price
// changes never require a code release.
//
// Out of scope (deferred):
//   - 1h extended TTL cache pricing (`ephemeral_1h_input_tokens`)
//   - Server-tool-use pricing (web search, code execution)

import { asMicroUsd, type CostUnits, type MicroUsd } from "@wuphf/protocol";

import { UnknownModelError } from "../errors.ts";
import type { CostEstimator } from "../types.ts";

/**
 * Per-model rates in integer micro-USD per million tokens (μUSD/MTok).
 * All four fields MUST be non-negative safe integers; validation is
 * enforced at provider construction (`createAnthropicProvider`).
 *
 * Cache fields are the 5-minute-TTL ephemeral cache. 1h extended TTL
 * is out of scope.
 */
export interface AnthropicModelPricing {
  readonly inputMicroUsdPerMTok: number;
  readonly outputMicroUsdPerMTok: number;
  readonly cacheReadMicroUsdPerMTok: number;
  readonly cacheCreationMicroUsdPerMTok: number;
}

export type AnthropicPricingTable = Readonly<Record<string, AnthropicModelPricing>>;

/**
 * Built-in pricing for the Claude 4.x family, current as of 2026-05.
 *
 *   Opus 4.5 / 4.6 / 4.7:  $5  / $25 / $0.50 / $6.25  per MTok
 *   Opus 4.1 (legacy):     $15 / $75 / $1.50 / $18.75 per MTok
 *   Sonnet 4.5 / 4.6:      $3  / $15 / $0.30 / $3.75  per MTok
 *   Haiku 4.5:             $1  / $5  / $0.10 / $1.25  per MTok
 *
 * Opus 4.5+ dropped 3x from the 4.1 generation per Anthropic's
 * 2025-11 announcement. The previous draft of this table used 4.1
 * rates for 4.5/4.7, which over-billed by 3x and would have caused
 * false daily-cap trips — see triangulation #2 finding B2-1.
 *
 * Hosts that want negotiated rates pass a full `AnthropicPricingTable`
 * to `createAnthropicProvider`. The model registry derives from the
 * passed table's keys, so adding a model is one config change.
 */
export const DEFAULT_ANTHROPIC_PRICING: AnthropicPricingTable = Object.freeze({
  // Opus 4.1 (legacy generation): $15 / $75 / $1.50 / $18.75 per MTok
  "claude-opus-4-1": {
    inputMicroUsdPerMTok: 15_000_000,
    outputMicroUsdPerMTok: 75_000_000,
    cacheReadMicroUsdPerMTok: 1_500_000,
    cacheCreationMicroUsdPerMTok: 18_750_000,
  },
  // Opus 4.5 / 4.6 / 4.7 generation: $5 / $25 / $0.50 / $6.25 per MTok
  "claude-opus-4-5": {
    inputMicroUsdPerMTok: 5_000_000,
    outputMicroUsdPerMTok: 25_000_000,
    cacheReadMicroUsdPerMTok: 500_000,
    cacheCreationMicroUsdPerMTok: 6_250_000,
  },
  "claude-opus-4-6": {
    inputMicroUsdPerMTok: 5_000_000,
    outputMicroUsdPerMTok: 25_000_000,
    cacheReadMicroUsdPerMTok: 500_000,
    cacheCreationMicroUsdPerMTok: 6_250_000,
  },
  "claude-opus-4-7": {
    inputMicroUsdPerMTok: 5_000_000,
    outputMicroUsdPerMTok: 25_000_000,
    cacheReadMicroUsdPerMTok: 500_000,
    cacheCreationMicroUsdPerMTok: 6_250_000,
  },
  // Sonnet 4.x family ($3 / $15 / $0.30 / $3.75 per MTok)
  "claude-sonnet-4-5": {
    inputMicroUsdPerMTok: 3_000_000,
    outputMicroUsdPerMTok: 15_000_000,
    cacheReadMicroUsdPerMTok: 300_000,
    cacheCreationMicroUsdPerMTok: 3_750_000,
  },
  "claude-sonnet-4-6": {
    inputMicroUsdPerMTok: 3_000_000,
    outputMicroUsdPerMTok: 15_000_000,
    cacheReadMicroUsdPerMTok: 300_000,
    cacheCreationMicroUsdPerMTok: 3_750_000,
  },
  // Haiku 4.5 ($1 / $5 / $0.10 / $1.25 per MTok)
  "claude-haiku-4-5": {
    inputMicroUsdPerMTok: 1_000_000,
    outputMicroUsdPerMTok: 5_000_000,
    cacheReadMicroUsdPerMTok: 100_000,
    cacheCreationMicroUsdPerMTok: 1_250_000,
  },
});

const ONE_MILLION = 1_000_000;
const ROUND_HALF_UP = ONE_MILLION / 2;

/**
 * Compute the integer μUSD cost of one Anthropic call. Accumulates in
 * μUSD-million-tokens space (no precision loss for sub-μUSD/tok rates
 * like $0.30/MTok cache-reads), then round-half-up to integer μUSD.
 *
 * The §15.A invariant holds because the final value is a non-negative
 * safe integer; intermediate fixed-point accumulation never leaves
 * integer arithmetic.
 *
 * Overflow safety: max plausible per-call sum is
 *   input_tokens (1e6) * input_rate (15e6 μUSD/MTok)
 *   = 1.5e13, well inside Number.MAX_SAFE_INTEGER (~9e15).
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
  const accum =
    rate.inputMicroUsdPerMTok * units.inputTokens +
    rate.outputMicroUsdPerMTok * units.outputTokens +
    rate.cacheReadMicroUsdPerMTok * units.cacheReadTokens +
    rate.cacheCreationMicroUsdPerMTok * units.cacheCreationTokens;
  const rounded = Math.floor((accum + ROUND_HALF_UP) / ONE_MILLION);
  return asMicroUsd(rounded);
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
 * Validate a pricing table at provider construction. Throws on any
 * invalid entry so the gateway never tries to bill a malformed rate.
 * See triangulation #2 finding B2-6.
 */
export function validateAnthropicPricingTable(table: AnthropicPricingTable): void {
  const models = Object.keys(table);
  if (models.length === 0) {
    throw new Error("AnthropicPricingTable must register at least one model");
  }
  for (const model of models) {
    if (model.length === 0) {
      throw new Error("AnthropicPricingTable model id must be a non-empty string");
    }
    const rate = table[model];
    if (rate === undefined) {
      // Shouldn't happen given Object.keys, but the guard keeps the
      // type narrowing honest for the loop body.
      throw new Error(`AnthropicPricingTable: missing rate for model ${JSON.stringify(model)}`);
    }
    const fields: Array<[string, number]> = [
      ["inputMicroUsdPerMTok", rate.inputMicroUsdPerMTok],
      ["outputMicroUsdPerMTok", rate.outputMicroUsdPerMTok],
      ["cacheReadMicroUsdPerMTok", rate.cacheReadMicroUsdPerMTok],
      ["cacheCreationMicroUsdPerMTok", rate.cacheCreationMicroUsdPerMTok],
    ];
    for (const [field, value] of fields) {
      if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
        throw new Error(
          `AnthropicPricingTable[${JSON.stringify(model)}].${field} must be a non-negative safe integer, got ${String(value)}`,
        );
      }
    }
  }
}

/**
 * The set of model IDs the default pricing table knows about. Used by
 * the provider to declare its `models[]` for gateway routing.
 */
export const DEFAULT_ANTHROPIC_MODELS: readonly string[] = Object.freeze(
  Object.keys(DEFAULT_ANTHROPIC_PRICING),
);
