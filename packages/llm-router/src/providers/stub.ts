// Stub provider for the §10.4 burn-down (`ci:burn:nightly`).
//
// Determinism is the contract: every call costs exactly
// STUB_FIXED_COST_MICRO_USD with zero variance, returns canned text, and
// reports fixed token usage. The burn-down asserts the per-office daily
// cap (default $5 = 5_000_000 micro-USD) holds at $5.00 ± $0.05 across 60
// minutes of throttled calls; that contract requires exact-amount
// predictability.
//
// Two model names are exposed:
//
//   stub-fixed-cost   — the §10.4 target. 10000 micro-USD/call.
//   stub-error        — always throws. Used for breaker tests.
//
// Adding a third "random cost" variant is tempting for testing daily-cap
// edge cases but would break §10.4's invariance — keep them in test files
// with their own provider.

import {
  asMicroUsd,
  asProviderKind,
  type CostUnits,
  type MicroUsd,
  type ProviderKind,
} from "@wuphf/protocol";

import { ProviderError, UnknownModelError } from "../errors.ts";
import type { CostEstimator, Provider, ProviderRequest, ProviderResponse } from "../types.ts";

const STUB_PROVIDER_KIND: ProviderKind = asProviderKind("openai-compat");

/**
 * $0.01 per call. Picked so the §10.4 burn-down can run 500 calls
 * inside the $5 cap and have ~$0 headroom — a tighter cap surface for
 * the test, while still cheap enough that the wake cap throttles before
 * the daily cap (12/hr × 60min = 720 wakes; daily cap stops at 500).
 */
export const STUB_FIXED_COST_MICRO_USD = 10_000;

const STUB_FIXED_RESPONSE_TEXT = "ack";

// Identical usage for every call; cache fields are zero (stub does not
// model prompt caching). Holding usage constant makes the §10.4 ledger
// math byte-for-byte reproducible.
const STUB_FIXED_USAGE: CostUnits = Object.freeze({
  inputTokens: 100,
  outputTokens: 50,
  cacheReadTokens: 0,
  cacheCreationTokens: 0,
});

export const STUB_MODEL_FIXED_COST = "stub-fixed-cost";
export const STUB_MODEL_ERROR = "stub-error";

class StubCostEstimator implements CostEstimator {
  estimate(model: string, _usage: CostUnits): MicroUsd {
    if (model === STUB_MODEL_FIXED_COST) {
      return asMicroUsd(STUB_FIXED_COST_MICRO_USD);
    }
    if (model === STUB_MODEL_ERROR) {
      // stub-error never produces a successful response, so this branch is
      // unreachable in practice; the throw makes the dead branch loud if a
      // future refactor accidentally takes it.
      throw new UnknownModelError(model);
    }
    throw new UnknownModelError(model);
  }
}

/**
 * `Provider` implementation for the stub models. Constructed once and
 * registered alongside real providers on `Gateway` startup; the gateway
 * routes requests to a provider by model-name prefix matching.
 */
export class StubProvider implements Provider {
  // ProviderKind doesn't have "stub"; "openai-compat" is the catch-all
  // for audit-event attribution (see protocol PROVIDER_KIND_VALUES).
  // The gateway routes by `models`, NOT by `kind`, so this stub can
  // share `kind` with a future real openai-compat provider without
  // colliding on model registration.
  readonly kind: ProviderKind = STUB_PROVIDER_KIND;
  readonly models: readonly string[] = [STUB_MODEL_FIXED_COST, STUB_MODEL_ERROR];
  readonly costEstimator: CostEstimator = new StubCostEstimator();

  async complete(req: ProviderRequest): Promise<ProviderResponse> {
    if (req.model === STUB_MODEL_FIXED_COST) {
      return Promise.resolve({
        text: STUB_FIXED_RESPONSE_TEXT,
        usage: STUB_FIXED_USAGE,
      });
    }
    if (req.model === STUB_MODEL_ERROR) {
      throw new ProviderError("openai-compat", new Error("stub-error: forced failure"));
    }
    throw new UnknownModelError(req.model);
  }
}

export function createStubProvider(): StubProvider {
  return new StubProvider();
}

export function isStubModel(model: string): boolean {
  return model === STUB_MODEL_FIXED_COST || model === STUB_MODEL_ERROR;
}
