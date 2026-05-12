// Ollama per-model pricing — zero across the board.
//
// Ollama runs locally on the host's hardware; there is no provider-side
// charge per token. The pricing table is still wired so the gateway's
// `Gateway.complete()` writes a `cost_event` for every successful call
// (Hard rule #1: no row, no response). The amount is just `0 μUSD`.
//
// Why keep the table at all then?
//
//   1. Accounting consistency: every Gateway.complete() emits exactly
//      one cost_event row, regardless of provider. Downstream consumers
//      (cost tile, §10.4 burn-down projection, cost_by_agent) get a
//      uniform shape.
//   2. Host overridability: a host that wants to model GPU/electricity
//      cost can pass a custom `OllamaPricingTable` with non-zero rates.
//      The integer-μUSD/MTok shape is preserved so the §15.A sum
//      invariant continues to hold under host overrides.
//   3. Discoverability: the table acts as the registered model list,
//      mirroring the Anthropic/OpenAI adapter pattern (`models[]`
//      derives from the pricing keys).
//
// The model set mirrors what the public `ollama` SDK type defs
// reference and what `ollama.com/library` ships as common defaults:
// llama 3.1 / 3.2 / 3.3, qwen2.5, gemma2, and mistral-small3.1.
// Adding a model is one config entry — the host passes
// `{ "model-name": { ... } }` to `createOllamaProvider`.

import { asMicroUsd, type CostUnits, type MicroUsd } from "@wuphf/protocol";

import { UnknownModelError } from "../errors.ts";
import type { CostEstimator } from "../types.ts";

/**
 * Per-model rates in integer micro-USD per million tokens (μUSD/MTok).
 * Same shape and unit as Anthropic's table so a host that wants to
 * model GPU cost can populate the fields with non-zero rates and the
 * estimator math works unchanged.
 *
 * Cache fields exist for shape uniformity. Ollama itself does not
 * surface a cache-read/cache-creation accounting split in its
 * ChatResponse, so the adapter zeroes them when translating usage.
 */
export interface OllamaModelPricing {
  readonly inputMicroUsdPerMTok: number;
  readonly outputMicroUsdPerMTok: number;
  readonly cacheReadMicroUsdPerMTok: number;
  readonly cacheCreationMicroUsdPerMTok: number;
}

export type OllamaPricingTable = Readonly<Record<string, OllamaModelPricing>>;

const ZERO_RATES: OllamaModelPricing = Object.freeze({
  inputMicroUsdPerMTok: 0,
  outputMicroUsdPerMTok: 0,
  cacheReadMicroUsdPerMTok: 0,
  cacheCreationMicroUsdPerMTok: 0,
});

/**
 * Built-in pricing for common Ollama models. All rates are zero because
 * Ollama is a local model runner — the host pays no per-token cost.
 *
 * Hosts that want to track GPU/electricity cost pass a custom table to
 * `createOllamaProvider`. The model registry derives from the passed
 * table's keys, so adding a model is one config change.
 */
export const DEFAULT_OLLAMA_PRICING: OllamaPricingTable = Object.freeze({
  "llama3.3": ZERO_RATES,
  "llama3.2": ZERO_RATES,
  "llama3.1": ZERO_RATES,
  "qwen2.5": ZERO_RATES,
  gemma2: ZERO_RATES,
  "mistral-small3.1": ZERO_RATES,
});

const ONE_MILLION = 1_000_000;
const ROUND_HALF_UP = ONE_MILLION / 2;

/**
 * Compute the integer μUSD cost of one Ollama call. Same fixed-point
 * shape as the Anthropic/OpenAI estimators so a host that overrides
 * the table with non-zero GPU rates gets identical math behavior.
 *
 * With the default table (all zeros), the result is always 0 — but the
 * function still goes through the same arithmetic so the §15.A integer
 * invariant holds uniformly.
 *
 * Overflow safety: max plausible per-call sum is identical to the other
 * estimators (1e6 tokens × 1e7 μUSD/MTok = 1e13), well inside
 * `Number.MAX_SAFE_INTEGER`.
 */
export function estimateOllamaCostMicroUsd(
  pricing: OllamaPricingTable,
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
 * `OllamaProvider` and overridable by hosts that need a different
 * price book (e.g. to model local GPU cost or to add a new model).
 */
export function createOllamaCostEstimator(pricing: OllamaPricingTable): CostEstimator {
  return {
    estimate(model: string, units: CostUnits): MicroUsd {
      return estimateOllamaCostMicroUsd(pricing, model, units);
    },
  };
}

/**
 * Validate a pricing table at provider construction. Same contract as
 * `validateAnthropicPricingTable` / `validateOpenAIPricingTable`: all
 * rates MUST be non-negative safe integers. Zero is explicitly allowed
 * — that's the default — but `NaN`, negatives, and floats throw.
 */
export function validateOllamaPricingTable(table: OllamaPricingTable): void {
  const models = Object.keys(table);
  if (models.length === 0) {
    throw new Error("OllamaPricingTable must register at least one model");
  }
  for (const model of models) {
    if (model.length === 0) {
      throw new Error("OllamaPricingTable model id must be a non-empty string");
    }
    const rate = table[model];
    if (rate === undefined) {
      throw new Error(`OllamaPricingTable: missing rate for model ${JSON.stringify(model)}`);
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
          `OllamaPricingTable[${JSON.stringify(model)}].${field} must be a non-negative safe integer, got ${String(value)}`,
        );
      }
    }
  }
}

/**
 * Model IDs the default table knows about. Used by the provider to
 * declare its `models[]` for gateway routing.
 */
export const DEFAULT_OLLAMA_MODELS: readonly string[] = Object.freeze(
  Object.keys(DEFAULT_OLLAMA_PRICING),
);
