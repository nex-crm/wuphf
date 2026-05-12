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

/**
 * SDK request options we use. We pass `headers` directly (NOT the
 * `idempotencyKey` shorthand) because the SDK only forwards
 * `options.idempotencyKey` when its internal `idempotencyHeader`
 * field is configured, and the default `OpenAI` client leaves it
 * undefined — so the shorthand is a silent no-op.
 * See triangulation #3 finding B3-1 (5-lens BLOCK/HIGH).
 */
export interface OpenAIRequestOptions {
  readonly headers?: Readonly<Record<string, string>>;
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
  // SDK marks `usage` optional. Adapter validates presence before
  // billing rather than throwing an unclassified TypeError.
  // See triangulation #3 finding B3-5.
  readonly usage?: OpenAIUsage;
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
      // B3-4: model-aware token field. GPT-5 and reasoning models
      // (o1, o3, o4) deprecated `max_tokens` and require
      // `max_completion_tokens`. Legacy GPT-4.1 and earlier use
      // `max_tokens`. Sending both can yield a 400 on reasoning models.
      const params: OpenAIChatCompletionCreateParams = isReasoningOrGpt5(req.model)
        ? {
            model: req.model,
            max_completion_tokens: req.maxOutputTokens,
            messages: [{ role: "user", content: req.prompt }],
          }
        : {
            model: req.model,
            max_tokens: req.maxOutputTokens,
            messages: [{ role: "user", content: req.prompt }],
          };
      // B3-1: pass an explicit Idempotency-Key header, NOT
      // `options.idempotencyKey`. The SDK's `idempotencyKey` shorthand
      // is only forwarded when its internal `idempotencyHeader` field
      // is set, which the default client leaves undefined.
      // B3-2: derive the key from req.requestKey (gateway-computed,
      // includes ctx) so two agents with the same prompt don't share
      // a server-side dedup window.
      const options: OpenAIRequestOptions = {
        headers: { "Idempotency-Key": deriveIdempotencyKey(req) },
      };
      let raw: OpenAIChatCompletion;
      try {
        raw = await args.client.chat.completions.create(params, options);
      } catch (err) {
        throw classifySdkError(err);
      }
      return buildProviderResponse(raw);
    },
  };
}

/**
 * Derive a stable idempotency key. Preference order:
 *
 *   1. `req.requestKey` — gateway-computed `hashRequest(ctx, req)`.
 *      This is context-scoped so two different agents sending the
 *      same prompt do NOT collide on the server-side dedup window.
 *      See triangulation #3 finding B3-2.
 *   2. Content-only fallback for callers that build a request outside
 *      the gateway path (direct tests, manual smoke calls). NOT used
 *      in production.
 *
 * Either way the key is prefixed with `wuphf-` so the request is
 * identifiable in Anthropic / OpenAI-side support traces.
 */
function deriveIdempotencyKey(req: ProviderRequest): string {
  if (typeof req.requestKey === "string" && req.requestKey.length > 0) {
    return `wuphf-${req.requestKey}`;
  }
  const projection = canonicalJSON({
    model: req.model,
    prompt: req.prompt,
    maxOutputTokens: req.maxOutputTokens,
  });
  return `wuphf-${sha256Hex(projection)}`;
}

/**
 * GPT-5 family and reasoning models (`o1`, `o3`, `o4`) deprecated
 * `max_tokens` and require `max_completion_tokens`. Sending the
 * legacy field can yield 400. Other models accept `max_tokens`.
 */
function isReasoningOrGpt5(model: string): boolean {
  return model.startsWith("gpt-5") || /^o[1-9]/.test(model);
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
    // B3-7: prefer `retry-after-ms` (millisecond precision) over
    // `retry-after` (seconds). The OpenAI SDK reads both for its
    // internal retry logic; we mirror that order.
    const retryAfterMs = readHeader(e.headers, "retry-after-ms");
    if (retryAfterMs !== undefined) {
      const ms = Number(retryAfterMs);
      if (Number.isFinite(ms) && ms >= 0) {
        out.retryAfterMs = Math.round(ms);
      }
    } else {
      const retryAfter = readHeader(e.headers, "retry-after");
      if (retryAfter !== undefined) {
        const seconds = Number(retryAfter);
        if (Number.isFinite(seconds) && seconds >= 0) {
          out.retryAfterMs = Math.round(seconds * 1000);
        }
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
 * Build the `ProviderResponse` from an OpenAI chat completion.
 *
 * Surfaces:
 *   - `text`: `message.content` for normal completions, `""` when
 *     content is null and there's no refusal. Refusals are NOT folded
 *     into `text` — they go into the separate `refusal` field so a
 *     caller can implement policy gates without parsing prose. See
 *     triangulation #3 finding B3-3.
 *   - `refusal`: `message.refusal` when present.
 *   - `finishReason`: passed through from the SDK so callers can
 *     distinguish `stop` from `length` (truncation), `content_filter`,
 *     `tool_calls`, etc.
 *   - `usage`: validated for presence and non-negative-integer shape;
 *     `cached_tokens` is clamped to `prompt_tokens` so a malformed
 *     provider response can't overstate cached billing.
 *     See triangulation #3 findings B3-5, B3-6.
 *
 * Throws `ProviderError` if usage is missing — that's a provider
 * post-condition violation and the caller deserves a typed error,
 * not an unclassified `TypeError`.
 */
function buildProviderResponse(raw: OpenAIChatCompletion): ProviderResponse {
  const first = raw.choices[0];
  if (raw.usage === undefined) {
    throw new ProviderError(OPENAI_PROVIDER_KIND, new Error("openai_usage_missing"));
  }
  validateUsageCounters(raw.usage);

  const refusal = first?.message.refusal ?? null;
  const content = first?.message.content ?? "";
  const finishReason = first?.finish_reason ?? null;

  // Compute cost units: prompt_tokens INCLUDES cached_tokens; the
  // discounted cached-input rate applies to the cached subset, the
  // full rate to the remainder. Clamp cached to prompt so a malformed
  // response can't double-bill.
  const cachedTokensRaw = raw.usage.prompt_tokens_details?.cached_tokens ?? 0;
  const cachedTokens = Math.min(Math.max(0, cachedTokensRaw), raw.usage.prompt_tokens);
  const freshInput = Math.max(0, raw.usage.prompt_tokens - cachedTokens);
  const usage: CostUnits = {
    inputTokens: freshInput,
    outputTokens: raw.usage.completion_tokens,
    cacheReadTokens: cachedTokens,
    cacheCreationTokens: 0,
  };

  return {
    text: refusal === null ? content : "",
    usage,
    ...(finishReason !== null ? { finishReason } : {}),
    ...(refusal !== null ? { refusal } : {}),
    // #827: surface served snapshot id (e.g. gpt-5-2025-08-07) so the
    // audit row records the actual served model.
    ...(typeof raw.model === "string" && raw.model.length > 0 ? { model: raw.model } : {}),
  };
}

function validateUsageCounters(usage: OpenAIUsage): void {
  const fields: ReadonlyArray<[string, number | undefined]> = [
    ["prompt_tokens", usage.prompt_tokens],
    ["completion_tokens", usage.completion_tokens],
    ["prompt_tokens_details.cached_tokens", usage.prompt_tokens_details?.cached_tokens],
  ];
  for (const [name, value] of fields) {
    if (value === undefined) continue; // optional
    if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
      throw new ProviderError(
        OPENAI_PROVIDER_KIND,
        new Error(`openai_usage_${name}_invalid: ${String(value)}`),
      );
    }
  }
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
