// Ollama SDK adapter for `Gateway.complete()`.
//
// Subpath import: hosts use `import { createOllamaProvider } from
// "@wuphf/llm-router/ollama"`. `ollama` is an optional peer
// dependency — hosts that only use the stub or another provider do
// not install it. The convenience constructor
// `createOllamaProviderWithUrl` uses a dynamic import so the SDK
// module loads only when the host explicitly asks for it.
//
// Differences from the Anthropic / OpenAI adapters:
//
//   - **Zero-cost pricing by default.** Ollama runs locally on the
//     host's hardware — there is no per-token provider charge. The
//     pricing table holds all-zero rates so `cost_event.amountMicroUsd`
//     is 0. The `cost_event` row is still written (Hard rule #1: no row,
//     no response) for accounting uniformity. Hosts that want to model
//     GPU/electricity cost override the pricing table.
//
//   - **No idempotency-key.** Ollama is a local HTTP server with no
//     documented server-side request dedupe contract. There's no header
//     to forward, and a "retry against the same local process" doesn't
//     incur double-billing because billing is $0 to start with. The
//     gateway's content-hash dedupe (60s sliding window) still applies
//     upstream of this adapter.
//
//   - **No API-key constructor.** Local execution; no auth. We expose
//     `createOllamaProvider({ client, pricing? })` for full DI and
//     `createOllamaProviderWithUrl({ baseUrl?, pricing? })` as a
//     convenience that lazy-loads the SDK and constructs the client
//     with a base URL (default `http://localhost:11434`).
//
// Otherwise the wiring mirrors the existing adapters:
//
//   1. Provider routing: `models[]` is bound to the pricing-table keys
//      so the gateway's exact-match registration delivers e.g.
//      `llama3.3` requests here.
//
//   2. Cost estimation: integer-μUSD pricing (`ollama-pricing.ts`),
//      same fixed-point shape as the other adapters — preserves §15.A
//      integer invariant under host overrides.
//
//   3. Request translation: `ProviderRequest` carries one string
//      prompt → one user message; `maxOutputTokens` → `options.num_predict`
//      (Ollama's name for the generation cap). Streaming is disabled
//      (`stream: false`) so the SDK returns a single `ChatResponse`.
//
//   4. Error mapping: Ollama errors are typically network/connection
//      failures (server not running, model not pulled). We map all
//      thrown errors to `ProviderError`. There's no HTTP-status-based
//      caller-input class to peel off the way Anthropic/OpenAI do —
//      Ollama's local HTTP errors don't follow the 4xx-vs-5xx
//      convention reliably. We DO pre-validate `maxOutputTokens > 0`
//      locally as `BadRequestError`, same as the other adapters.

import { asProviderKind, type CostUnits, type ProviderKind } from "@wuphf/protocol";

import { BadRequestError, ProviderError, UnknownModelError } from "../errors.ts";
import type { CostEstimator, Provider, ProviderRequest, ProviderResponse } from "../types.ts";
import {
  createOllamaCostEstimator,
  DEFAULT_OLLAMA_MODELS,
  DEFAULT_OLLAMA_PRICING,
  type OllamaPricingTable,
  validateOllamaPricingTable,
} from "./ollama-pricing.ts";

const OLLAMA_PROVIDER_KIND: ProviderKind = asProviderKind("ollama");

const DEFAULT_OLLAMA_BASE_URL = "http://localhost:11434";

/**
 * Minimal slice of the Ollama SDK chat surface. Tests inject a fake;
 * the real `Ollama` client matches.
 *
 * The signature mirrors `client.chat(request)` for the non-streaming
 * (`stream: false`) variant. Streaming is out of scope for PR B.4;
 * the gateway's response shape is a single completion.
 *
 * `OllamaMessage` deliberately omits the SDK's `thinking`, `images`,
 * `tool_calls`, and `tool_name` fields — they're returned by the SDK
 * but we don't surface them through `ProviderResponse` yet.
 */
export interface OllamaMessage {
  readonly role: string;
  readonly content: string;
}

export interface OllamaChatRequest {
  readonly model: string;
  readonly messages: ReadonlyArray<OllamaMessage>;
  readonly stream: false;
  readonly options?: {
    /** Ollama's name for the generation token cap (≈ max_tokens). */
    readonly num_predict?: number;
  };
}

export interface OllamaChatResponse {
  readonly model: string;
  readonly message: OllamaMessage;
  readonly done: boolean;
  /** Tokens consumed parsing the prompt. Ollama-equivalent of input_tokens. */
  readonly prompt_eval_count: number;
  /** Tokens produced. Ollama-equivalent of output_tokens. */
  readonly eval_count: number;
}

export interface OllamaClient {
  chat(request: OllamaChatRequest): Promise<OllamaChatResponse>;
}

export interface CreateOllamaProviderArgs {
  /**
   * Ollama SDK client (production) or a fake matching `OllamaClient`
   * (tests). The structural-client path does NOT pull in the `ollama`
   * package.
   */
  readonly client: OllamaClient;
  /**
   * Pricing table override. Defaults to `DEFAULT_OLLAMA_PRICING` (all
   * zero rates). Validated at construction; throws on missing/invalid
   * entries.
   */
  readonly pricing?: OllamaPricingTable;
}

export function createOllamaProvider(args: CreateOllamaProviderArgs): Provider {
  const pricing = args.pricing ?? DEFAULT_OLLAMA_PRICING;
  // Same construction-time validation as the other adapters so a bad
  // config never reaches a billable call (even when "billable" means
  // "zero μUSD" — the validation catches NaN/negative/float rates that
  // would corrupt the §15.A invariant under a host override).
  validateOllamaPricingTable(pricing);
  const models: readonly string[] =
    args.pricing === undefined ? DEFAULT_OLLAMA_MODELS : Object.keys(pricing);
  const modelSet = new Set<string>(models);
  const costEstimator: CostEstimator = createOllamaCostEstimator(pricing);

  return {
    kind: OLLAMA_PROVIDER_KIND,
    models,
    costEstimator,
    async complete(req: ProviderRequest): Promise<ProviderResponse> {
      if (!modelSet.has(req.model)) {
        // Defensive: the gateway already routed by exact-match. If we
        // get here, the host built two providers claiming overlapping
        // models and the gateway delivered to the wrong one — surface
        // as UnknownModelError instead of dropping into the SDK.
        throw new UnknownModelError(req.model);
      }
      // Pre-validate caller input. num_predict ≤ 0 is meaningless to
      // Ollama (it would return nothing or treat 0 as "unbounded"
      // depending on version). Pre-rejecting locally avoids ambiguity
      // and matches the Anthropic/OpenAI contract for `maxOutputTokens`.
      if (!Number.isSafeInteger(req.maxOutputTokens) || req.maxOutputTokens <= 0) {
        throw new BadRequestError(OLLAMA_PROVIDER_KIND, new Error("maxOutputTokens_invalid"));
      }
      const request: OllamaChatRequest = {
        model: req.model,
        messages: [{ role: "user", content: req.prompt }],
        stream: false,
        options: { num_predict: req.maxOutputTokens },
      };
      let raw: OllamaChatResponse;
      try {
        raw = await args.client.chat(request);
      } catch (err) {
        // No status/headers/retry-after convention to extract from a
        // local Ollama error — surface as ProviderError with cause
        // attached. PR C's wire mapping treats this uniformly.
        throw new ProviderError(OLLAMA_PROVIDER_KIND, err);
      }
      return {
        text: raw.message?.content ?? "",
        usage: usageToCostUnits(raw),
      };
    },
  };
}

/**
 * Translate Ollama's response counters to our `CostUnits` shape.
 *
 *   inputTokens  = prompt_eval_count
 *   outputTokens = eval_count
 *   cacheRead/cacheCreation = 0   (Ollama does not surface a cache
 *                                  accounting split)
 *
 * Older or partial responses may omit these fields; coerce to 0 so the
 * protocol `CostUnits` invariant (non-negative integer) holds.
 */
function usageToCostUnits(raw: OllamaChatResponse): CostUnits {
  const promptEval = typeof raw.prompt_eval_count === "number" ? raw.prompt_eval_count : 0;
  const evalCount = typeof raw.eval_count === "number" ? raw.eval_count : 0;
  return {
    inputTokens: Math.max(0, promptEval),
    outputTokens: Math.max(0, evalCount),
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
  };
}

// Re-export pricing surface so hosts using the subpath get one import line:
//   import {
//     createOllamaProvider,
//     DEFAULT_OLLAMA_PRICING,
//     type OllamaPricingTable,
//   } from "@wuphf/llm-router/ollama";
export type { OllamaModelPricing, OllamaPricingTable } from "./ollama-pricing.ts";
export {
  createOllamaCostEstimator,
  DEFAULT_OLLAMA_MODELS,
  DEFAULT_OLLAMA_PRICING,
  estimateOllamaCostMicroUsd,
  validateOllamaPricingTable,
} from "./ollama-pricing.ts";

/**
 * Convenience constructor for the real SDK client. The `ollama`
 * package is an optional peer dependency; this function uses a
 * dynamic import so a host that only uses the structural-client path
 * (e.g. tests) never loads the SDK module.
 *
 * Unlike the Anthropic/OpenAI constructors there is no `apiKey` —
 * Ollama runs locally with no auth. `baseUrl` defaults to
 * `http://localhost:11434`, matching the SDK's own default; hosts
 * pointing at a remote Ollama (over SSH tunnel or LAN) pass an
 * explicit URL.
 *
 * Runtime validation: when `baseUrl` is provided it must be a
 * non-empty string. We don't try to validate URL well-formedness
 * here — the SDK will surface the failure at first call as a
 * `ProviderError`.
 */
export async function createOllamaProviderWithUrl(
  args: { readonly baseUrl?: string; readonly pricing?: OllamaPricingTable } = {},
): Promise<Provider> {
  if (args.baseUrl !== undefined) {
    if (typeof args.baseUrl !== "string" || args.baseUrl.length === 0) {
      throw new Error(
        "createOllamaProviderWithUrl: baseUrl must be a non-empty string (got " +
          typeof args.baseUrl +
          ")",
      );
    }
  }
  const host = args.baseUrl ?? DEFAULT_OLLAMA_BASE_URL;
  const sdk = await import("ollama");
  const OllamaCtor = sdk.Ollama;
  const client = new OllamaCtor({ host });
  // The SDK's `chat()` is overloaded on the `stream` discriminator; the
  // structural `OllamaClient` we expose only models the non-streaming
  // form, so we widen via `unknown` rather than collapse the SDK's
  // overload set on the public type.
  return createOllamaProvider({
    client: client as unknown as OllamaClient,
    ...(args.pricing !== undefined ? { pricing: args.pricing } : {}),
  });
}
