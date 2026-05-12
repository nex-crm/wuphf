// OpenAI per-model pricing, stored as integer micro-USD per million
// tokens (μUSD/MTok). Same fixed-point design as
// `anthropic-pricing.ts`; see that file's header for the rationale.
//
// Quick-reference:
//
//   public price → integer rate
//   ─────────────────────────────
//   $10.00/MTok  → 10_000_000
//   $5.00/MTok   →  5_000_000
//   $1.25/MTok   →  1_250_000
//   $0.625/MTok  →    625_000
//   $0.30/MTok   →    300_000
//   $0.25/MTok   →    250_000
//   $0.10/MTok   →    100_000
//   $0.05/MTok   →     50_000
//
// OpenAI's `CompletionUsage` exposes three fields we care about:
//
//   prompt_tokens                                — fresh input tokens
//   completion_tokens                            — output tokens
//   prompt_tokens_details.cached_tokens?         — cached input subset
//
// Note: OpenAI's cached-input billing is a DISCOUNT — `prompt_tokens`
// already includes the cached subset; `cached_tokens` is the portion of
// that count that bills at the cached-input rate (typically 50% of base
// input). So when computing cost we split prompt_tokens into:
//
//   fresh_input = prompt_tokens - cached_tokens
//   cached_input = cached_tokens
//
// The adapter does this split before calling the estimator, so this
// file's `cachedInputMicroUsdPerMTok` is the price for ALREADY-cached
// input. There is no "cache creation" line for OpenAI — caching is
// automatic and not separately billed.
//
// Pricing source: https://openai.com/api/pricing (verified 2026-05-12
// against the public table). Hosts override any rate via the
// `OpenAIPricingTable` config injected at construction time.
//
// Out of scope (deferred):
//   - Audio tokens (prompt_tokens_details.audio_tokens) — separate rate
//   - Reasoning tokens (completion_tokens_details.reasoning_tokens) —
//     billed at the same rate as output_tokens, so already covered
//   - Predicted-output tokens — separate billable line

import { asMicroUsd, type CostUnits, type MicroUsd } from "@wuphf/protocol";

import { UnknownModelError } from "../errors.ts";
import type { CostEstimator } from "../types.ts";

/**
 * Per-model rates in integer micro-USD per million tokens (μUSD/MTok).
 * All three fields MUST be non-negative safe integers; validation is
 * enforced at provider construction (`createOpenAIProvider`).
 *
 * Unlike Anthropic, OpenAI's caching is automatic; there is no
 * "cache_creation" line. Cached input tokens billed at the cached
 * rate are a SUBSET of `prompt_tokens` already counted, not an
 * additional charge.
 */
export interface OpenAIModelPricing {
  /** Rate for fresh (non-cached) prompt tokens. */
  readonly inputMicroUsdPerMTok: number;
  /** Rate for output (completion) tokens. */
  readonly outputMicroUsdPerMTok: number;
  /**
   * Rate for cached prompt tokens (subset of prompt_tokens that hit
   * the cache). Typically 50% of `inputMicroUsdPerMTok`.
   */
  readonly cachedInputMicroUsdPerMTok: number;
}

export type OpenAIPricingTable = Readonly<Record<string, OpenAIModelPricing>>;

/**
 * Built-in pricing for the GPT-5.x and GPT-4.1 families, current as
 * of 2026-05.
 *
 *   GPT-5:           $1.25 input / $10  output / $0.125 cached
 *   GPT-5-mini:      $0.25 input / $2   output / $0.025 cached
 *   GPT-5-nano:      $0.05 input / $0.4 output / $0.005 cached
 *   GPT-4.1:         $2    input / $8   output / $0.50  cached
 *   GPT-4.1-mini:    $0.40 input / $1.6 output / $0.10  cached
 *   GPT-4.1-nano:    $0.10 input / $0.4 output / $0.025 cached
 *
 * Cached input is consistently 10% of input rate across the GPT-5
 * family and 25% across GPT-4.1; OpenAI's pricing page is the source
 * of truth for any future model.
 *
 * Hosts that want negotiated rates pass a full `OpenAIPricingTable`
 * to `createOpenAIProvider`. The model registry derives from the
 * passed table's keys.
 */
// Deep-freeze each nested rate object — same rationale as DEFAULT_ANTHROPIC_PRICING:
// `readonly` is compile-time only, so without per-entry Object.freeze a runtime
// assignment could silently corrupt billing for every consumer of this table.
export const DEFAULT_OPENAI_PRICING: OpenAIPricingTable = Object.freeze({
  // GPT-5 family ($1.25 / $10 / $0.125 per MTok)
  "gpt-5": Object.freeze({
    inputMicroUsdPerMTok: 1_250_000,
    outputMicroUsdPerMTok: 10_000_000,
    cachedInputMicroUsdPerMTok: 125_000,
  }),
  "gpt-5-mini": Object.freeze({
    inputMicroUsdPerMTok: 250_000,
    outputMicroUsdPerMTok: 2_000_000,
    cachedInputMicroUsdPerMTok: 25_000,
  }),
  "gpt-5-nano": Object.freeze({
    inputMicroUsdPerMTok: 50_000,
    outputMicroUsdPerMTok: 400_000,
    cachedInputMicroUsdPerMTok: 5_000,
  }),
  // GPT-4.1 family ($2 / $8 / $0.50 per MTok)
  "gpt-4.1": Object.freeze({
    inputMicroUsdPerMTok: 2_000_000,
    outputMicroUsdPerMTok: 8_000_000,
    cachedInputMicroUsdPerMTok: 500_000,
  }),
  "gpt-4.1-mini": Object.freeze({
    inputMicroUsdPerMTok: 400_000,
    outputMicroUsdPerMTok: 1_600_000,
    cachedInputMicroUsdPerMTok: 100_000,
  }),
  "gpt-4.1-nano": Object.freeze({
    inputMicroUsdPerMTok: 100_000,
    outputMicroUsdPerMTok: 400_000,
    cachedInputMicroUsdPerMTok: 25_000,
  }),
});

const ONE_MILLION = 1_000_000;
const ROUND_HALF_UP = ONE_MILLION / 2;

/**
 * Compute the integer μUSD cost of one OpenAI call. Accumulates in
 * μUSD-million-tokens space (no precision loss for sub-μUSD/tok rates
 * like $0.05/MTok inputs), then round-half-up to integer μUSD.
 *
 * `units.inputTokens` is the FRESH input (prompt_tokens − cached_tokens);
 * `units.cacheReadTokens` is the cached subset. The adapter must do
 * this split before calling the estimator; passing the raw OpenAI
 * `prompt_tokens` here would double-count cached tokens at the fresh
 * rate. `units.cacheCreationTokens` is unused for OpenAI (automatic
 * caching, no separate creation line).
 *
 * Overflow safety: max plausible per-call sum is
 *   prompt_tokens (1e6) * input_rate (10e6 μUSD/MTok) = 1e13,
 * well inside Number.MAX_SAFE_INTEGER (~9e15).
 */
export function estimateOpenAICostMicroUsd(
  pricing: OpenAIPricingTable,
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
    rate.cachedInputMicroUsdPerMTok * units.cacheReadTokens;
  const rounded = Math.floor((accum + ROUND_HALF_UP) / ONE_MILLION);
  return asMicroUsd(rounded);
}

/**
 * Build a `CostEstimator` bound to a specific pricing table. Used by
 * `OpenAIProvider` and overridable by hosts that need a different
 * price book.
 */
export function createOpenAICostEstimator(pricing: OpenAIPricingTable): CostEstimator {
  return {
    estimate(model: string, units: CostUnits): MicroUsd {
      return estimateOpenAICostMicroUsd(pricing, model, units);
    },
  };
}

/**
 * Validate a pricing table at provider construction. Throws on any
 * invalid entry so the gateway never tries to bill a malformed rate.
 * Same contract as `validateAnthropicPricingTable`.
 */
export function validateOpenAIPricingTable(table: OpenAIPricingTable): void {
  const models = Object.keys(table);
  if (models.length === 0) {
    throw new Error("OpenAIPricingTable must register at least one model");
  }
  for (const model of models) {
    if (model.length === 0) {
      throw new Error("OpenAIPricingTable model id must be a non-empty string");
    }
    const rate = table[model];
    if (rate === undefined) {
      throw new Error(`OpenAIPricingTable: missing rate for model ${JSON.stringify(model)}`);
    }
    const fields: Array<[string, number]> = [
      ["inputMicroUsdPerMTok", rate.inputMicroUsdPerMTok],
      ["outputMicroUsdPerMTok", rate.outputMicroUsdPerMTok],
      ["cachedInputMicroUsdPerMTok", rate.cachedInputMicroUsdPerMTok],
    ];
    for (const [field, value] of fields) {
      if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
        throw new Error(
          `OpenAIPricingTable[${JSON.stringify(model)}].${field} must be a non-negative safe integer, got ${String(value)}`,
        );
      }
    }
  }
}

/**
 * Model IDs the default table knows about. Used by the provider to
 * declare its `models[]` for gateway routing.
 */
export const DEFAULT_OPENAI_MODELS: readonly string[] = Object.freeze(
  Object.keys(DEFAULT_OPENAI_PRICING),
);
