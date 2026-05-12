// OpenAI SDK adapter for `Gateway.complete()`.
//
// Subpath import: hosts use `import { createOpenAIProvider } from
// "@wuphf/llm-router/openai"`. `openai` is a peer dependency — hosts
// that only use the stub or Anthropic do not install it. The
// convenience constructor `createOpenAIProviderWithKey` uses a dynamic
// import so the SDK module loads only when explicitly requested.
//
// Mirrors the AnthropicProvider design (post-triangulation-#2):
//
//   1. Provider routing: `models[]` is bound to the pricing-table keys,
//      so the gateway's exact-match registration puts `gpt-*` requests
//      on this provider.
//
//   2. Cost estimation: integer-μUSD pricing (`openai-pricing.ts`).
//      OpenAI's `prompt_tokens` already INCLUDES `cached_tokens`; the
//      adapter splits them so the estimator can apply the discounted
//      cached-input rate to the cached subset and the full input rate
//      to the remainder. Same §15.A integer-math invariant.
//
//   3. Request translation: `ProviderRequest` carries a single string
//      prompt → one user message in the chat-completions payload;
//      `maxOutputTokens` → `max_completion_tokens` (GPT-5 family) or
//      `max_tokens` (GPT-4.x). For PR B.3 we pass both fields so the
//      SDK accepts either generation.
//
//   4. Error mapping: 4xx caller-input errors → `BadRequestError`
//      (NOT a breaker strike — triangulation B2-7); auth/rate-limit/
//      5xx/network → `ProviderError` with structured metadata
//      (status, requestId, errorType, retryAfterMs) — same shape as
//      the Anthropic adapter so PR C's wire mapping stays uniform.
//
//   5. Idempotency-key threading: deterministic SHA-256 key from
//      canonical request bytes, passed via SDK request options.

import {
  asProviderKind,
  type CostUnits,
  canonicalJSON,
  type ProviderKind,
  sha256Hex,
} from "@wuphf/protocol";

import { BadRequestError, ProviderError, UnknownModelError } from "../errors.ts";
import type { CostEstimator, Provider, ProviderRequest, ProviderResponse } from "../types.ts";
import {
  createOpenAICostEstimator,
  DEFAULT_OPENAI_MODELS,
  DEFAULT_OPENAI_PRICING,
  type OpenAIPricingTable,
  validateOpenAIPricingTable,
} from "./openai-pricing.ts";

const OPENAI_PROVIDER_KIND: ProviderKind = asProviderKind("openai");

// HTTP statuses the gateway treats as caller-input (NOT breaker strikes).
const CALLER_INPUT_STATUSES = new Set<number>([400, 413, 422]);

interface SdkErrorLike {
  readonly status?: unknown;
  readonly headers?: unknown;
  readonly error?: unknown;
  readonly request_id?: unknown;
  readonly requestID?: unknown;
}

/**
 * Minimal slice of OpenAI's chat-completions surface. Tests inject a
 * fake; the real `OpenAI` client matches. Streaming is out of scope.
 *
 * Both `max_tokens` (legacy) and `max_completion_tokens` (GPT-5+) are
 * passed so the same call shape works across model generations.
 */
export interface OpenAIChatCompletionCreateParams {
  readonly model: string;
  readonly max_tokens?: number;
  readonly max_completion_tokens?: number;
  readonly messages: ReadonlyArray<{
    readonly role: "user" | "assistant" | "system";
    readonly content: string;
  }>;
}

export interface OpenAIRequestOptions {
  /**
   * Stable string the SDK forwards as the `idempotency-key` header.
   * OpenAI's server deduplicates same-key requests within a window.
   */
  readonly idempotencyKey?: string;
}

export interface OpenAIUsage {
  readonly prompt_tokens: number;
  readonly completion_tokens: number;
  readonly prompt_tokens_details?: { readonly cached_tokens?: number };
}

export interface OpenAIChatCompletion {
  readonly model: string;
  readonly choices: ReadonlyArray<{
    readonly message: { readonly content: string | null; readonly refusal: string | null };
    readonly finish_reason: string | null;
  }>;
  readonly usage: OpenAIUsage;
}

export interface OpenAIClient {
  readonly chat: {
    readonly completions: {
      create(
        params: OpenAIChatCompletionCreateParams,
        options?: OpenAIRequestOptions,
      ): Promise<OpenAIChatCompletion>;
    };
  };
}

export interface CreateOpenAIProviderArgs {
  readonly client: OpenAIClient;
  /**
   * Pricing table override. Defaults to `DEFAULT_OPENAI_PRICING`.
   * Validated at construction; throws on missing/invalid entries.
   */
  readonly pricing?: OpenAIPricingTable;
}

export function createOpenAIProvider(args: CreateOpenAIProviderArgs): Provider {
  const pricing = args.pricing ?? DEFAULT_OPENAI_PRICING;
  validateOpenAIPricingTable(pricing);
  const models: readonly string[] =
    args.pricing === undefined ? DEFAULT_OPENAI_MODELS : Object.keys(pricing);
  const modelSet = new Set<string>(models);
  const costEstimator: CostEstimator = createOpenAICostEstimator(pricing);

  return {
    kind: OPENAI_PROVIDER_KIND,
    models,
    costEstimator,
    async complete(req: ProviderRequest): Promise<ProviderResponse> {
      if (!modelSet.has(req.model)) {
        // Defensive: the gateway already routed by exact-match.
        throw new UnknownModelError(req.model);
      }
      if (!Number.isSafeInteger(req.maxOutputTokens) || req.maxOutputTokens <= 0) {
        throw new BadRequestError(OPENAI_PROVIDER_KIND, new Error("maxOutputTokens_invalid"));
      }
      const params: OpenAIChatCompletionCreateParams = {
        model: req.model,
        // Pass both: SDK accepts max_completion_tokens for GPT-5 family,
        // max_tokens for legacy models. OpenAI's API ignores the unused
        // one. Sending both is cheaper than sniffing model generation.
        max_tokens: req.maxOutputTokens,
        max_completion_tokens: req.maxOutputTokens,
        messages: [{ role: "user", content: req.prompt }],
      };
      const options: OpenAIRequestOptions = {
        idempotencyKey: deriveIdempotencyKey(req),
      };
      let raw: OpenAIChatCompletion;
      try {
        raw = await args.client.chat.completions.create(params, options);
      } catch (err) {
        throw classifySdkError(err);
      }
      return {
        text: extractText(raw),
        usage: usageToCostUnits(raw.usage),
      };
    },
  };
}

/**
 * Derive a stable idempotency key from the request payload. Same logical
 * request → same key → OpenAI dedupes server-side.
 */
function deriveIdempotencyKey(req: ProviderRequest): string {
  const projection = canonicalJSON({
    model: req.model,
    prompt: req.prompt,
    maxOutputTokens: req.maxOutputTokens,
  });
  return `wuphf-${sha256Hex(projection)}`;
}

function classifySdkError(err: unknown): BadRequestError | ProviderError {
  const meta = extractSdkErrorMetadata(err);
  if (meta.status !== undefined && CALLER_INPUT_STATUSES.has(meta.status)) {
    return new BadRequestError(OPENAI_PROVIDER_KIND, err, {
      ...(meta.status !== undefined ? { status: meta.status } : {}),
      ...(meta.requestId !== undefined ? { requestId: meta.requestId } : {}),
      ...(meta.errorType !== undefined ? { errorType: meta.errorType } : {}),
    });
  }
  return new ProviderError(OPENAI_PROVIDER_KIND, err, {
    ...(meta.status !== undefined ? { status: meta.status } : {}),
    ...(meta.requestId !== undefined ? { requestId: meta.requestId } : {}),
    ...(meta.errorType !== undefined ? { errorType: meta.errorType } : {}),
    ...(meta.retryAfterMs !== undefined ? { retryAfterMs: meta.retryAfterMs } : {}),
  });
}

interface ExtractedSdkMetadata {
  readonly status?: number;
  readonly requestId?: string;
  readonly errorType?: string;
  readonly retryAfterMs?: number;
}

function extractSdkErrorMetadata(err: unknown): ExtractedSdkMetadata {
  if (typeof err !== "object" || err === null) return {};
  const e = err as SdkErrorLike;
  const out: { -readonly [K in keyof ExtractedSdkMetadata]: ExtractedSdkMetadata[K] } = {};
  if (typeof e.status === "number") {
    out.status = e.status;
  }
  // OpenAI SDK uses both snake_case (request_id) and camelCase (requestID)
  // across versions; check both.
  if (typeof e.request_id === "string") {
    out.requestId = e.request_id;
  } else if (typeof e.requestID === "string") {
    out.requestId = e.requestID;
  }
  if (typeof e.error === "object" && e.error !== null) {
    const errBody = e.error as { readonly type?: unknown; readonly code?: unknown };
    if (typeof errBody.type === "string") {
      out.errorType = errBody.type;
    } else if (typeof errBody.code === "string") {
      out.errorType = errBody.code;
    }
  }
  if (typeof e.headers === "object" && e.headers !== null) {
    const retryAfter = readHeader(e.headers, "retry-after");
    if (retryAfter !== undefined) {
      const seconds = Number(retryAfter);
      if (Number.isFinite(seconds) && seconds >= 0) {
        out.retryAfterMs = Math.round(seconds * 1000);
      }
    }
  }
  return out;
}

function readHeader(headers: unknown, name: string): string | undefined {
  if (typeof headers === "object" && headers !== null && "get" in headers) {
    const getter = (headers as { readonly get?: (n: string) => string | null }).get;
    if (typeof getter === "function") {
      const v = getter.call(headers, name);
      if (typeof v === "string") return v;
    }
  }
  if (typeof headers === "object" && headers !== null) {
    const lower = name.toLowerCase();
    for (const [k, v] of Object.entries(headers as Record<string, unknown>)) {
      if (k.toLowerCase() === lower && typeof v === "string") {
        return v;
      }
    }
  }
  return undefined;
}

/**
 * Extract response text from the first choice. A refusal lands in a
 * separate `refusal` field (model declined to answer) — we surface
 * that as text too, so the caller can see WHAT the refusal said. Tool
 * calls / function calls are dropped for PR B.3 (no tool-use plumbing
 * yet in the gateway response shape).
 */
function extractText(raw: OpenAIChatCompletion): string {
  const first = raw.choices[0];
  if (first === undefined) return "";
  if (first.message.refusal !== null) {
    return first.message.refusal;
  }
  return first.message.content ?? "";
}

/**
 * Translate OpenAI's `CompletionUsage` to our `CostUnits` shape.
 *
 * Key subtlety: OpenAI's `prompt_tokens` INCLUDES `cached_tokens`. To
 * apply the discounted cached-input rate correctly, we split them:
 *
 *   inputTokens (fresh)  = prompt_tokens - cached_tokens
 *   cacheReadTokens      = cached_tokens
 *   cacheCreationTokens  = 0   (OpenAI caches automatically; no
 *                              separate creation line)
 *
 * If the SDK omits `prompt_tokens_details` (older models that don't
 * surface caching), `cached_tokens` defaults to 0 and all input bills
 * at the fresh rate.
 */
function usageToCostUnits(usage: OpenAIUsage): CostUnits {
  const cachedTokens = usage.prompt_tokens_details?.cached_tokens ?? 0;
  const freshInput = Math.max(0, usage.prompt_tokens - cachedTokens);
  return {
    inputTokens: freshInput,
    outputTokens: usage.completion_tokens,
    cacheReadTokens: cachedTokens,
    cacheCreationTokens: 0,
  };
}

export type { OpenAIModelPricing, OpenAIPricingTable } from "./openai-pricing.ts";
export {
  createOpenAICostEstimator,
  DEFAULT_OPENAI_MODELS,
  DEFAULT_OPENAI_PRICING,
  estimateOpenAICostMicroUsd,
  validateOpenAIPricingTable,
} from "./openai-pricing.ts";

/**
 * Convenience constructor for the real SDK client. The SDK is a peer
 * dependency; this function uses a dynamic import so a host that only
 * uses the structural-client path never loads the SDK module.
 *
 * Reads the key from the argument, NOT from process.env, so the secret
 * boundary stays at the host.
 */
export async function createOpenAIProviderWithKey(args: {
  readonly apiKey: string;
  readonly pricing?: OpenAIPricingTable;
}): Promise<Provider> {
  if (typeof args.apiKey !== "string" || args.apiKey.length === 0) {
    throw new Error(
      "createOpenAIProviderWithKey: apiKey must be a non-empty string (got " +
        typeof args.apiKey +
        ")",
    );
  }
  const sdk = await import("openai");
  const OpenAICtor = sdk.default;
  const client = new OpenAICtor({ apiKey: args.apiKey });
  return createOpenAIProvider({
    client: client as unknown as OpenAIClient,
    ...(args.pricing !== undefined ? { pricing: args.pricing } : {}),
  });
}
