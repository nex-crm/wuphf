// opencode / opencodego pricing.
//
// opencode (TypeScript) and opencodego (Go port) are agent runners,
// not LLM providers themselves — they wrap an underlying model (often
// Anthropic, OpenAI, or a local Ollama). The actual provider cost
// depends on whatever backing model the host has wired into the
// opencode CLI / HTTP service.
//
// For PR B.5 we ship a zero-cost default table: the cost ledger still
// writes one `cost_event` per call (Hard rule #1: no row, no
// response) for accounting symmetry, with `amountMicroUsd = 0`. Hosts
// override the table to model the real backing-model cost — same
// pattern as `ollama-pricing.ts`. Future PRs may add "pass-through"
// pricing where opencode self-reports the underlying model's spend.
//
// Same fixed-point design as the other adapters (per-MTok integer
// μUSD); validation enforced at provider construction.

import { asMicroUsd, type CostUnits, type MicroUsd } from "@wuphf/protocol";

import { UnknownModelError } from "../errors.ts";
import type { CostEstimator } from "../types.ts";

export interface OpenCodeModelPricing {
  readonly inputMicroUsdPerMTok: number;
  readonly outputMicroUsdPerMTok: number;
  /**
   * Cached prompt rate (subset of input tokens). Set to `undefined`
   * for runners that don't expose cache accounting; the estimator
   * treats absent rates as zero.
   */
  readonly cachedInputMicroUsdPerMTok?: number;
}

export type OpenCodePricingTable = Readonly<Record<string, OpenCodeModelPricing>>;

/**
 * Default zero-cost table. Hosts override with rates that reflect the
 * backing model opencode is configured to use. The model IDs are
 * placeholders documenting the most common opencode profiles; rename
 * via host override to match a different upstream config.
 */
export const DEFAULT_OPENCODE_PRICING: OpenCodePricingTable = Object.freeze({
  "opencode-default": {
    inputMicroUsdPerMTok: 0,
    outputMicroUsdPerMTok: 0,
    cachedInputMicroUsdPerMTok: 0,
  },
  // Common opencode profiles (placeholders — host overrides with real
  // upstream rates).
  "opencode-anthropic-sonnet": {
    inputMicroUsdPerMTok: 0,
    outputMicroUsdPerMTok: 0,
    cachedInputMicroUsdPerMTok: 0,
  },
  "opencode-openai-gpt5": {
    inputMicroUsdPerMTok: 0,
    outputMicroUsdPerMTok: 0,
    cachedInputMicroUsdPerMTok: 0,
  },
});

/**
 * Default for the Go port. Same shape, separate default keys so the
 * audit row can distinguish opencode-ts traffic from opencodego.
 */
export const DEFAULT_OPENCODEGO_PRICING: OpenCodePricingTable = Object.freeze({
  "opencodego-default": {
    inputMicroUsdPerMTok: 0,
    outputMicroUsdPerMTok: 0,
    cachedInputMicroUsdPerMTok: 0,
  },
  "opencodego-anthropic-sonnet": {
    inputMicroUsdPerMTok: 0,
    outputMicroUsdPerMTok: 0,
    cachedInputMicroUsdPerMTok: 0,
  },
});

const ONE_MILLION = 1_000_000;
const ROUND_HALF_UP = ONE_MILLION / 2;

export function estimateOpenCodeCostMicroUsd(
  pricing: OpenCodePricingTable,
  model: string,
  units: CostUnits,
): MicroUsd {
  const rate = pricing[model];
  if (rate === undefined) {
    throw new UnknownModelError(model);
  }
  const cachedRate = rate.cachedInputMicroUsdPerMTok ?? 0;
  const accum =
    rate.inputMicroUsdPerMTok * units.inputTokens +
    rate.outputMicroUsdPerMTok * units.outputTokens +
    cachedRate * units.cacheReadTokens;
  const rounded = Math.floor((accum + ROUND_HALF_UP) / ONE_MILLION);
  return asMicroUsd(rounded);
}

export function createOpenCodeCostEstimator(pricing: OpenCodePricingTable): CostEstimator {
  return {
    estimate(model: string, units: CostUnits): MicroUsd {
      return estimateOpenCodeCostMicroUsd(pricing, model, units);
    },
  };
}

export function validateOpenCodePricingTable(table: OpenCodePricingTable): void {
  const models = Object.keys(table);
  if (models.length === 0) {
    throw new Error("OpenCodePricingTable must register at least one model");
  }
  for (const model of models) {
    if (model.length === 0) {
      throw new Error("OpenCodePricingTable model id must be a non-empty string");
    }
    const rate = table[model];
    if (rate === undefined) {
      throw new Error(`OpenCodePricingTable: missing rate for ${JSON.stringify(model)}`);
    }
    const fields: ReadonlyArray<[string, number | undefined]> = [
      ["inputMicroUsdPerMTok", rate.inputMicroUsdPerMTok],
      ["outputMicroUsdPerMTok", rate.outputMicroUsdPerMTok],
      ["cachedInputMicroUsdPerMTok", rate.cachedInputMicroUsdPerMTok],
    ];
    for (const [field, value] of fields) {
      if (value === undefined) continue; // optional
      if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
        throw new Error(
          `OpenCodePricingTable[${JSON.stringify(model)}].${field} must be a non-negative safe integer, got ${String(value)}`,
        );
      }
    }
  }
}

export const DEFAULT_OPENCODE_MODELS: readonly string[] = Object.freeze(
  Object.keys(DEFAULT_OPENCODE_PRICING),
);

export const DEFAULT_OPENCODEGO_MODELS: readonly string[] = Object.freeze(
  Object.keys(DEFAULT_OPENCODEGO_PRICING),
);
