// Anthropic SDK adapter for `Gateway.complete()`.
//
// Subpath import: hosts use `import { createAnthropicProvider } from
// "@wuphf/llm-router/anthropic"`. The root `@wuphf/llm-router` does NOT
// pull `@anthropic-ai/sdk` into the import graph, mirroring the
// `@wuphf/broker/sqlite` precedent. Hosts that only use the stub
// (e.g. tests, §10.4 burn-down) pay zero install cost for the SDK.
//
// The adapter wires four things:
//
//   1. Provider routing: `models[]` is bound to the pricing-table model
//      IDs, so the gateway's exact-match registration (post-H4 fix) puts
//      `claude-*` requests on this provider. A host that adds a new
//      pricing entry MUST also expand `models[]` — `createAnthropicProvider`
//      handles this automatically when given the pricing table.
//
//   2. Cost estimation: integer-μUSD pricing (`anthropic-pricing.ts`). The
//      §15.A invariant is preserved because we never leave integer math
//      between provider response and `appendCostEvent`.
//
//   3. Request translation: `ProviderRequest` carries a single string
//      prompt today (the simplest shape the gateway needs); we translate
//      to a single user message and lift `maxOutputTokens` directly into
//      Anthropic's `max_tokens` field.
//
//   4. Error mapping: every SDK error class collapses to `ProviderError`
//      with the original error attached as `cause`. The breaker sees one
//      consistent failure surface regardless of HTTP status. We do NOT
//      try to distinguish 429 / 5xx here — the breaker treats every
//      failure as a strike, and the in-flight reservation issue (#819)
//      is where rate-limit-specific behavior belongs.

import Anthropic from "@anthropic-ai/sdk";
import { asProviderKind, type CostUnits, type ProviderKind } from "@wuphf/protocol";

import { ProviderError, UnknownModelError } from "../errors.ts";
import type { CostEstimator, Provider, ProviderRequest, ProviderResponse } from "../types.ts";
import {
  type AnthropicPricingTable,
  createAnthropicCostEstimator,
  DEFAULT_ANTHROPIC_MODELS,
  DEFAULT_ANTHROPIC_PRICING,
} from "./anthropic-pricing.ts";

const ANTHROPIC_PROVIDER_KIND: ProviderKind = asProviderKind("anthropic");

/**
 * Minimal slice of the Anthropic SDK surface we depend on, so tests can
 * inject a fake client without pulling the whole SDK type tree.
 *
 * The shape mirrors `client.messages.create(MessageCreateParamsNonStreaming,
 * options) → APIPromise<Message>`. Streaming is out of scope for PR B.2 —
 * see "out of scope" in the package README.
 */
export interface AnthropicMessageCreateParams {
  readonly model: string;
  readonly max_tokens: number;
  readonly messages: ReadonlyArray<{
    readonly role: "user" | "assistant";
    readonly content: string;
  }>;
}

export interface AnthropicMessageUsage {
  readonly input_tokens: number;
  readonly output_tokens: number;
  readonly cache_read_input_tokens: number | null;
  readonly cache_creation_input_tokens: number | null;
}

export interface AnthropicMessage {
  readonly content: ReadonlyArray<{ readonly type: string; readonly text?: string }>;
  readonly usage: AnthropicMessageUsage;
}

/**
 * Anything that exposes `messages.create(params) → Promise<AnthropicMessage>`
 * satisfies this. The real `Anthropic` client matches; tests pass a fake.
 */
export interface AnthropicClient {
  readonly messages: {
    create(params: AnthropicMessageCreateParams): Promise<AnthropicMessage>;
  };
}

export interface CreateAnthropicProviderArgs {
  /**
   * Anthropic SDK client. Production: `new Anthropic({ apiKey: process.env.ANTHROPIC_API_KEY })`.
   * Tests: inject a fake matching `AnthropicClient`.
   *
   * Optional in this signature so a host that only wants pricing/model
   * registration (e.g. a smoke test that doesn't actually call the API)
   * can pass `undefined` — but `complete()` will throw if called.
   */
  readonly client: AnthropicClient;
  /**
   * Pricing table override. Defaults to `DEFAULT_ANTHROPIC_PRICING`.
   * The provider's `models[]` is derived from the table's keys, so
   * overriding extends model registration in one place.
   */
  readonly pricing?: AnthropicPricingTable;
}

export function createAnthropicProvider(args: CreateAnthropicProviderArgs): Provider {
  const pricing = args.pricing ?? DEFAULT_ANTHROPIC_PRICING;
  const models: readonly string[] =
    args.pricing === undefined ? DEFAULT_ANTHROPIC_MODELS : Object.keys(pricing);
  const costEstimator: CostEstimator = createAnthropicCostEstimator(pricing);

  return {
    kind: ANTHROPIC_PROVIDER_KIND,
    models,
    costEstimator,
    async complete(req: ProviderRequest): Promise<ProviderResponse> {
      if (!models.includes(req.model)) {
        // Defensive: the gateway already routed by exact-match, so this
        // shouldn't fire in practice. If it does, the host built two
        // providers claiming overlapping models and the gateway delivered
        // to the wrong one — surface as UnknownModelError so the caller
        // gets a stable error type instead of an SDK-level 4xx.
        throw new UnknownModelError(req.model);
      }
      let raw: AnthropicMessage;
      try {
        raw = await args.client.messages.create({
          model: req.model,
          max_tokens: req.maxOutputTokens,
          messages: [{ role: "user", content: req.prompt }],
        });
      } catch (err) {
        // Every SDK error (auth, rate-limit, 5xx, network, abort) collapses
        // to ProviderError. The breaker treats them all as strikes; the
        // in-flight reservation work (issue #819) is where rate-limit-
        // specific retry / cool-down semantics belong.
        throw new ProviderError(ANTHROPIC_PROVIDER_KIND, err);
      }
      return {
        text: extractText(raw.content),
        usage: usageToCostUnits(raw.usage),
      };
    },
  };
}

/**
 * Flatten Anthropic's content-block array to a single text response.
 * For PR B.2 we only handle `type === "text"` blocks (the standard
 * non-streaming, non-tool-use path). Tool-use blocks and thinking blocks
 * are ignored on text extraction; their token usage is still in `usage`
 * so the cost line is correct. PR B.3+ will plumb tool_use through to
 * the gateway response shape.
 */
function extractText(content: AnthropicMessage["content"]): string {
  let out = "";
  for (const block of content) {
    if (block.type === "text" && typeof block.text === "string") {
      out += block.text;
    }
  }
  return out;
}

/**
 * Translate Anthropic's Usage to our CostUnits shape. SDK can return
 * `null` for cache fields (when the request didn't use prompt caching) —
 * coerce to 0 so the protocol CostUnits invariant (non-negative integer)
 * holds.
 */
function usageToCostUnits(usage: AnthropicMessageUsage): CostUnits {
  return {
    inputTokens: usage.input_tokens,
    outputTokens: usage.output_tokens,
    cacheReadTokens: usage.cache_read_input_tokens ?? 0,
    cacheCreationTokens: usage.cache_creation_input_tokens ?? 0,
  };
}

// Re-export pricing surface so hosts using the subpath get one import line:
//   import {
//     createAnthropicProvider,
//     DEFAULT_ANTHROPIC_PRICING,
//     type AnthropicPricingTable,
//   } from "@wuphf/llm-router/anthropic";
export type { AnthropicModelPricing, AnthropicPricingTable } from "./anthropic-pricing.ts";
export {
  createAnthropicCostEstimator,
  DEFAULT_ANTHROPIC_MODELS,
  DEFAULT_ANTHROPIC_PRICING,
  estimateAnthropicCostMicroUsd,
} from "./anthropic-pricing.ts";

// Convenience constructor for the real SDK client. Hosts that don't want
// to import `@anthropic-ai/sdk` directly can call this with their API key
// and get a ready-to-use Provider.
//
// Reads the key from the argument, NOT from process.env, so the secret
// boundary stays at the host — per protocol AGENTS.md, "Always source
// credentials from .env files. Never pass API tokens inline on the
// command line."
export function createAnthropicProviderWithKey(args: {
  readonly apiKey: string;
  readonly pricing?: AnthropicPricingTable;
}): Provider {
  const client = new Anthropic({ apiKey: args.apiKey });
  return createAnthropicProvider({
    client: client as unknown as AnthropicClient,
    ...(args.pricing !== undefined ? { pricing: args.pricing } : {}),
  });
}
