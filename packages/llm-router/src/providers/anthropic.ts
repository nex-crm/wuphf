// Anthropic SDK adapter for `Gateway.complete()`.
//
// Subpath import: hosts use `import { createAnthropicProvider } from
// "@wuphf/llm-router/anthropic"`. `@anthropic-ai/sdk` is a peer
// dependency — hosts that only use the stub do not install it. The
// convenience constructor `createAnthropicProviderWithKey` uses a
// dynamic import so the SDK module is loaded only when the host
// explicitly asks for it; the structural-client path
// (`createAnthropicProvider`) never touches the SDK.
//
// The adapter wires five things:
//
//   1. Provider routing: `models[]` is bound to the pricing-table model
//      IDs, so the gateway's exact-match registration (post-H4 fix) puts
//      `claude-*` requests on this provider. A host that adds a new
//      pricing entry MUST also expand `models[]` —
//      `createAnthropicProvider` handles this automatically when given
//      the pricing table.
//
//   2. Cost estimation: integer-μUSD pricing (`anthropic-pricing.ts`).
//      The §15.A invariant is preserved because we never leave integer
//      math between provider response and `appendCostEvent`.
//
//   3. Request translation: `ProviderRequest` carries a single string
//      prompt today (the simplest shape the gateway needs); we translate
//      to a single user message and lift `maxOutputTokens` directly into
//      Anthropic's `max_tokens` field.
//
//   4. Error mapping: structured triage of SDK errors. 400/413/422
//      (caller-input errors) → `BadRequestError`, which the gateway does
//      NOT count as a breaker strike (triangulation B2-7). 401/403/429/
//      5xx/network → `ProviderError` with structured metadata (status,
//      requestId, errorType, retryAfterMs) preserved for on-call
//      (triangulation B2-5).
//
//   5. Idempotency-key threading: every call mints a deterministic key
//      from the request bytes and passes it to `messages.create` so SDK
//      retries (which are on by default) don't double-charge after a
//      lost response (triangulation B2-4). The key is content-derived,
//      so a logical retry from the same caller reuses it.

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
  type AnthropicPricingTable,
  createAnthropicCostEstimator,
  DEFAULT_ANTHROPIC_MODELS,
  DEFAULT_ANTHROPIC_PRICING,
  validateAnthropicPricingTable,
} from "./anthropic-pricing.ts";

const ANTHROPIC_PROVIDER_KIND: ProviderKind = asProviderKind("anthropic");

// HTTP status set the gateway treats as caller-input (NOT breaker strikes).
// 400 = bad request, 413 = payload too large, 422 = unprocessable entity.
// 401 (auth), 403 (permission), 429 (rate limit), and 5xx stay as
// ProviderError so the breaker can react to real provider failures.
const CALLER_INPUT_STATUSES = new Set<number>([400, 413, 422]);

// Loose-typed accessor for SDK error metadata. The SDK's `APIError`
// class declares `status`, `headers`, and `error`; we read defensively
// so a future SDK version that renames a field degrades gracefully
// rather than throwing inside the catch handler.
interface SdkErrorLike {
  readonly status?: unknown;
  readonly headers?: unknown;
  readonly error?: unknown;
  readonly requestID?: unknown;
}

/**
 * Minimal slice of the Anthropic SDK surface we depend on. Tests inject
 * a fake; the real `Anthropic` client matches.
 *
 * We pass `headers` directly (NOT `options.idempotencyKey`) — the SDK
 * only forwards the shorthand when its internal `idempotencyHeader`
 * is configured, which the default client leaves undefined. Same bug
 * the OpenAI adapter had; same fix. See triangulation #3 finding B3-1
 * (5-lens BLOCK/HIGH).
 */
export interface AnthropicMessageCreateParams {
  readonly model: string;
  readonly max_tokens: number;
  readonly messages: ReadonlyArray<{
    readonly role: "user" | "assistant";
    readonly content: string;
  }>;
}

export interface AnthropicRequestOptions {
  readonly headers?: Readonly<Record<string, string>>;
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
  /**
   * Why the model stopped. `end_turn` is a normal completion;
   * `max_tokens` is truncation; `stop_sequence` matched a configured
   * stop; `tool_use` paused for a tool call; `refusal` is a policy
   * decline; `pause_turn` is a long-running pause to be resumed.
   * Surfaced through `ProviderResponse.finishReason`. See triangulation
   * #3 finding B3-3 (parallel to the OpenAI refusal/finish-reason fix).
   */
  readonly stop_reason?: string | null;
}

export interface AnthropicClient {
  readonly messages: {
    create(
      params: AnthropicMessageCreateParams,
      options?: AnthropicRequestOptions,
    ): Promise<AnthropicMessage>;
  };
}

export interface CreateAnthropicProviderArgs {
  /**
   * Anthropic SDK client (production) or a fake matching `AnthropicClient`
   * (tests). The structural-client path does NOT pull in `@anthropic-ai/sdk`.
   */
  readonly client: AnthropicClient;
  /**
   * Pricing table override. Defaults to `DEFAULT_ANTHROPIC_PRICING`.
   * Validated at construction; throws on missing/invalid entries.
   */
  readonly pricing?: AnthropicPricingTable;
}

export function createAnthropicProvider(args: CreateAnthropicProviderArgs): Provider {
  const pricing = args.pricing ?? DEFAULT_ANTHROPIC_PRICING;
  // B2-6: validate pricing at construction so a bad config never reaches
  // a billable call. The default table is also validated — cheap and
  // catches future drift if someone edits a rate to NaN.
  validateAnthropicPricingTable(pricing);
  const models: readonly string[] =
    args.pricing === undefined ? DEFAULT_ANTHROPIC_MODELS : Object.keys(pricing);
  const modelSet = new Set<string>(models);
  const costEstimator: CostEstimator = createAnthropicCostEstimator(pricing);

  return {
    kind: ANTHROPIC_PROVIDER_KIND,
    models,
    costEstimator,
    async complete(req: ProviderRequest): Promise<ProviderResponse> {
      if (!modelSet.has(req.model)) {
        // Defensive: the gateway already routed by exact-match, so this
        // shouldn't fire in practice. If it does, the host built two
        // providers claiming overlapping models and the gateway delivered
        // to the wrong one — surface as UnknownModelError so the caller
        // gets a stable error type instead of an SDK-level 4xx.
        throw new UnknownModelError(req.model);
      }
      // B2-7: pre-validate caller input. max_tokens must be > 0; the
      // server rejects 0 with 400 anyway, but pre-rejecting locally
      // means we don't burn a network round-trip OR send an idempotency
      // key that the server would reuse for a real retry.
      if (!Number.isSafeInteger(req.maxOutputTokens) || req.maxOutputTokens <= 0) {
        throw new BadRequestError(ANTHROPIC_PROVIDER_KIND, new Error("maxOutputTokens_invalid"));
      }
      const params: AnthropicMessageCreateParams = {
        model: req.model,
        max_tokens: req.maxOutputTokens,
        messages: [{ role: "user", content: req.prompt }],
      };
      // B3-1: explicit Idempotency-Key header. The SDK's
      // `options.idempotencyKey` shorthand is a no-op unless the
      // client's internal `idempotencyHeader` is configured.
      // B3-2: derive from req.requestKey (gateway-computed,
      // includes ctx).
      const options: AnthropicRequestOptions = {
        headers: { "Idempotency-Key": deriveIdempotencyKey(req) },
      };
      let raw: AnthropicMessage;
      try {
        raw = await args.client.messages.create(params, options);
      } catch (err) {
        throw classifySdkError(err);
      }
      // B3-3: surface stop_reason as finishReason and treat
      // `stop_reason === "refusal"` as a refusal so callers can
      // implement policy gates without parsing prose. The text-block
      // content for a refusal is the prose; we route it into `refusal`
      // and leave `text` empty so a caller that ignores `refusal`
      // doesn't accidentally treat the prose as a normal completion.
      const finishReason = raw.stop_reason ?? null;
      const isRefusal = finishReason === "refusal";
      const extractedText = extractText(raw.content);
      return {
        text: isRefusal ? "" : extractedText,
        usage: usageToCostUnits(raw.usage),
        ...(finishReason !== null ? { finishReason } : {}),
        ...(isRefusal ? { refusal: extractedText } : {}),
      };
    },
  };
}

/**
 * Derive a stable idempotency key from the request payload. Same logical
 * request → same key → Anthropic dedupes server-side. Different agents
 * issuing the same prompt get DIFFERENT keys upstream, because the
 * gateway's dedupe already short-circuits same-(ctx, request) repeats
 * before they reach the provider. (See B3 fix in PR B; dedupe key is
 * (agentSlug, taskId, receiptId, request).)
 *
 * The key prefix `wuphf-` makes it identifiable in Anthropic-side logs
 * if support needs to trace one.
 */
function deriveIdempotencyKey(req: ProviderRequest): string {
  // Prefer the gateway-computed `requestKey` (context-scoped) so two
  // agents with the same prompt don't share a server-side dedup
  // window. Fall back to content-only when constructed outside the
  // gateway path. See triangulation #3 finding B3-2.
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
 * Map an SDK error to the right typed gateway error. Caller-input
 * statuses (400/413/422) → `BadRequestError` (NOT a breaker strike);
 * everything else → `ProviderError` with structured metadata so on-call
 * sees status/requestId/retry-after instead of opaque "provider_error".
 */
function classifySdkError(err: unknown): BadRequestError | ProviderError {
  const meta = extractSdkErrorMetadata(err);
  if (meta.status !== undefined && CALLER_INPUT_STATUSES.has(meta.status)) {
    return new BadRequestError(ANTHROPIC_PROVIDER_KIND, err, {
      ...(meta.status !== undefined ? { status: meta.status } : {}),
      ...(meta.requestId !== undefined ? { requestId: meta.requestId } : {}),
      ...(meta.errorType !== undefined ? { errorType: meta.errorType } : {}),
    });
  }
  return new ProviderError(ANTHROPIC_PROVIDER_KIND, err, {
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
  if (typeof e.requestID === "string") {
    out.requestId = e.requestID;
  }
  if (typeof e.error === "object" && e.error !== null) {
    const errBody = e.error as { readonly error?: { readonly type?: unknown } };
    if (typeof errBody.error?.type === "string") {
      out.errorType = errBody.error.type;
    }
  }
  if (typeof e.headers === "object" && e.headers !== null) {
    // B3-7: prefer `retry-after-ms` (millisecond precision) over
    // `retry-after` (seconds). SDK's `headers` is a Headers-like.
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
  // Headers instance
  if (typeof headers === "object" && headers !== null && "get" in headers) {
    const getter = (headers as { readonly get?: (n: string) => string | null }).get;
    if (typeof getter === "function") {
      const v = getter.call(headers, name);
      if (typeof v === "string") return v;
    }
  }
  // Plain object indexer
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
 * Flatten Anthropic's content-block array to a single text response.
 * For PR B.2 we only handle `type === "text"` blocks (the standard
 * non-streaming, non-tool-use path). Tool-use blocks and thinking blocks
 * are ignored on text extraction; their token usage is still in `usage`
 * so the cost line is correct. PR B.3+ will plumb tool_use through to
 * the gateway response shape.
 *
 * Uses array-join instead of `+=` so allocation is O(total text) rather
 * than O(blocks²) for large multi-block responses.
 */
function extractText(content: AnthropicMessage["content"]): string {
  const parts: string[] = [];
  for (const block of content) {
    if (block.type === "text" && typeof block.text === "string") {
      parts.push(block.text);
    }
  }
  return parts.join("");
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
  validateAnthropicPricingTable,
} from "./anthropic-pricing.ts";

/**
 * Convenience constructor for the real SDK client. The SDK is a peer
 * dependency; this function uses a dynamic import so a host that only
 * uses the structural-client path (e.g. tests) never loads the SDK
 * module. Triangulation #2 finding perf-1 / B2-3.
 *
 * Reads the key from the argument, NOT from process.env, so the secret
 * boundary stays at the host — per protocol AGENTS.md, "Always source
 * credentials from .env files. Never pass API tokens inline on the
 * command line."
 *
 * Runtime validation: `apiKey` must be a non-empty string. The README's
 * `process.env.ANTHROPIC_API_KEY!` non-null assertion can forge "key
 * exists" from JS or misconfigured TS; this rejects it explicitly.
 */
export async function createAnthropicProviderWithKey(args: {
  readonly apiKey: string;
  readonly pricing?: AnthropicPricingTable;
}): Promise<Provider> {
  if (typeof args.apiKey !== "string" || args.apiKey.length === 0) {
    throw new Error(
      "createAnthropicProviderWithKey: apiKey must be a non-empty string (got " +
        typeof args.apiKey +
        ")",
    );
  }
  const sdk = await import("@anthropic-ai/sdk");
  const AnthropicCtor = sdk.default;
  const client = new AnthropicCtor({ apiKey: args.apiKey });
  return createAnthropicProvider({
    client: client as unknown as AnthropicClient,
    ...(args.pricing !== undefined ? { pricing: args.pricing } : {}),
  });
}
