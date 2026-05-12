// opencode + opencodego adapters.
//
// opencode (TypeScript) and opencodego (Go port) are agent runners
// that wrap one of several underlying LLM providers. From the
// gateway's perspective they ARE providers — they accept a prompt,
// return text + usage, and want the cost row written before the
// response returns. But they have two unusual properties:
//
//   1. **Mixed topology.** Some hosts run them as local CLI
//      subprocesses (spawn `opencode` and talk over stdin/stdout);
//      others hit a hosted HTTP endpoint. The adapter doesn't
//      pick — it accepts a structural `OpenCodeClient` and lets
//      the host inject either transport.
//
//   2. **Audit identity split.** opencode-ts and opencodego report
//      as different `ProviderKind` values (`"opencode"` vs
//      `"opencodego"`) so the cost ledger can distinguish them.
//      Same adapter source; two factory variants.
//
// Pricing defaults to zero because cost depends on the backing model
// the runner is configured to use. Hosts override the pricing table
// with rates that reflect their real upstream spend. See
// `opencode-pricing.ts` for the trade-off.
//
// Like Ollama: no idempotency-key forwarding by default. Local
// subprocesses have no server-side dedup contract. HTTP-backed hosts
// MAY plumb idempotency through their own client; the structural
// interface accepts `options.headers` for callers that want to set
// `Idempotency-Key` themselves.

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
  createOpenCodeCostEstimator,
  DEFAULT_OPENCODE_MODELS,
  DEFAULT_OPENCODE_PRICING,
  DEFAULT_OPENCODEGO_MODELS,
  DEFAULT_OPENCODEGO_PRICING,
  type OpenCodePricingTable,
  validateOpenCodePricingTable,
} from "./opencode-pricing.ts";

const OPENCODE_PROVIDER_KIND: ProviderKind = asProviderKind("opencode");
const OPENCODEGO_PROVIDER_KIND: ProviderKind = asProviderKind("opencodego");

/**
 * Discriminator for which runner the host is wiring in. Both share the
 * same adapter source; only the `kind` and the default pricing/model
 * registry differ.
 */
export type OpenCodeKind = "opencode" | "opencodego";

/**
 * Minimal transport interface — what an opencode-or-opencodego client
 * must expose. The host implements this with either a subprocess
 * runner or an HTTP client. Tests inject a fake.
 *
 * Request mirrors `ProviderRequest`; response carries the same
 * `usage` shape we already use for cost accounting.
 */
export interface OpenCodeChatRequest {
  readonly model: string;
  readonly prompt: string;
  readonly maxOutputTokens: number;
}

export interface OpenCodeUsage {
  readonly inputTokens: number;
  readonly outputTokens: number;
  readonly cachedInputTokens?: number;
}

export interface OpenCodeChatResponse {
  readonly text: string;
  readonly usage: OpenCodeUsage;
  /**
   * Served model id — typically the underlying upstream the runner is
   * configured to use (e.g. `claude-sonnet-4-6-20251015`). Surfaced
   * through `ProviderResponse.model` so the audit row pins the served
   * snapshot.  Optional — runners that don't expose it leave
   * this undefined.
   */
  readonly model?: string;
  /**
   * Surface a finish-reason if the runner reports one (e.g.
   * `"stop" | "length" | "tool_call"`). Optional — runners that don't
   * track it leave it undefined.
   */
  readonly finishReason?: string;
  /**
   * Surface a refusal string if the runner declined. The gateway
   * routes this into `ProviderResponse.refusal` so callers can
   * implement policy gates. Same contract as the other adapters.
   */
  readonly refusal?: string;
}

interface OpenCodeSubprocessResponseShape {
  readonly text?: unknown;
  readonly usage?: unknown;
  readonly finishReason?: unknown;
  readonly refusal?: unknown;
  readonly model?: unknown;
}

interface OpenCodeSubprocessUsageShape {
  readonly inputTokens?: unknown;
  readonly outputTokens?: unknown;
  readonly cachedInputTokens?: unknown;
}

export interface OpenCodeRequestOptions {
  readonly headers?: Readonly<Record<string, string>>;
}

/**
 * Structural transport interface. A subprocess-based client and an
 * HTTP-based client both satisfy this, so the adapter doesn't care
 * which the host wired in.
 */
export interface OpenCodeClient {
  chat(req: OpenCodeChatRequest, options?: OpenCodeRequestOptions): Promise<OpenCodeChatResponse>;
}

export interface CreateOpenCodeProviderArgs {
  readonly client: OpenCodeClient;
  /**
   * Pricing table override. Defaults to `DEFAULT_OPENCODE_PRICING`
   * (or `_OPENCODEGO_` based on `kind`). Validated at construction.
   */
  readonly pricing?: OpenCodePricingTable;
}

/**
 * Build an opencode provider (TypeScript runner). Records the audit
 * row under `providerKind: "opencode"`.
 */
export function createOpenCodeProvider(args: CreateOpenCodeProviderArgs): Provider {
  return buildProvider("opencode", args);
}

/**
 * Build an opencodego provider (Go port). Records the audit row
 * under `providerKind: "opencodego"` so cost reporting can
 * distinguish the two implementations.
 */
export function createOpenCodeGoProvider(args: CreateOpenCodeProviderArgs): Provider {
  return buildProvider("opencodego", args);
}

function buildProvider(kind: OpenCodeKind, args: CreateOpenCodeProviderArgs): Provider {
  const defaultPricing =
    kind === "opencode" ? DEFAULT_OPENCODE_PRICING : DEFAULT_OPENCODEGO_PRICING;
  const defaultModels = kind === "opencode" ? DEFAULT_OPENCODE_MODELS : DEFAULT_OPENCODEGO_MODELS;
  const pricing = args.pricing ?? defaultPricing;
  validateOpenCodePricingTable(pricing);
  const models: readonly string[] =
    args.pricing === undefined ? defaultModels : Object.keys(pricing);
  const modelSet = new Set<string>(models);
  const costEstimator: CostEstimator = createOpenCodeCostEstimator(pricing);
  const providerKind = kind === "opencode" ? OPENCODE_PROVIDER_KIND : OPENCODEGO_PROVIDER_KIND;

  return {
    kind: providerKind,
    models,
    costEstimator,
    async complete(req: ProviderRequest): Promise<ProviderResponse> {
      if (!modelSet.has(req.model)) {
        throw new UnknownModelError(req.model);
      }
      if (!Number.isSafeInteger(req.maxOutputTokens) || req.maxOutputTokens <= 0) {
        throw new BadRequestError(providerKind, new Error("maxOutputTokens_invalid"));
      }
      // Optional Idempotency-Key for HTTP-backed transports that
      // implement server-side dedup. Local subprocess transports
      // typically ignore headers — that's fine, the value is just
      // informational.
      const options: OpenCodeRequestOptions = {
        headers: { "Idempotency-Key": deriveIdempotencyKey(req) },
      };
      let raw: OpenCodeChatResponse;
      try {
        raw = await args.client.chat(
          {
            model: req.model,
            prompt: req.prompt,
            maxOutputTokens: req.maxOutputTokens,
          },
          options,
        );
      } catch (err) {
        // No HTTP status discrimination for the structural client —
        // hosts that want fine-grained 4xx/5xx mapping wrap their
        // transport before injecting it. We surface every transport
        // error as ProviderError so the breaker can react.
        throw new ProviderError(providerKind, err);
      }
      return buildProviderResponse(raw, providerKind);
    },
  };
}

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

function buildProviderResponse(
  raw: OpenCodeChatResponse,
  providerKind: ProviderKind,
): ProviderResponse {
  validateUsageCounters(raw.usage, providerKind);
  const cachedTokens = Math.min(
    Math.max(0, raw.usage.cachedInputTokens ?? 0),
    raw.usage.inputTokens,
  );
  const freshInput = Math.max(0, raw.usage.inputTokens - cachedTokens);
  const usage: CostUnits = {
    inputTokens: freshInput,
    outputTokens: raw.usage.outputTokens,
    cacheReadTokens: cachedTokens,
    cacheCreationTokens: 0,
  };
  // Refusal-routing mirrors the OpenAI/Anthropic adapters: refusal
  // prose goes into the separate `refusal` field, `text` is empty so
  // a caller that ignores `refusal` can't treat refusal prose as a
  // normal completion.
  const isRefusal = typeof raw.refusal === "string" && raw.refusal.length > 0;
  return {
    text: isRefusal ? "" : raw.text,
    usage,
    ...(raw.finishReason !== undefined ? { finishReason: raw.finishReason } : {}),
    ...(isRefusal ? { refusal: raw.refusal as string } : {}),
    // Surface served model id so the audit row pins the upstream
    // snapshot the runner actually used.
    ...(typeof raw.model === "string" && raw.model.length > 0 ? { model: raw.model } : {}),
  };
}

function validateUsageCounters(usage: OpenCodeUsage, providerKind: ProviderKind): void {
  const fields: ReadonlyArray<[string, number | undefined]> = [
    ["inputTokens", usage.inputTokens],
    ["outputTokens", usage.outputTokens],
    ["cachedInputTokens", usage.cachedInputTokens],
  ];
  for (const [name, value] of fields) {
    if (value === undefined) continue;
    if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
      throw new ProviderError(
        providerKind,
        new Error(`opencode_usage_${name}_invalid: ${String(value)}`),
      );
    }
  }
}

export type { OpenCodeModelPricing, OpenCodePricingTable } from "./opencode-pricing.ts";
export {
  createOpenCodeCostEstimator,
  DEFAULT_OPENCODE_MODELS,
  DEFAULT_OPENCODE_PRICING,
  DEFAULT_OPENCODEGO_MODELS,
  DEFAULT_OPENCODEGO_PRICING,
  estimateOpenCodeCostMicroUsd,
  validateOpenCodePricingTable,
} from "./opencode-pricing.ts";

// ─────────────────────────────────────────────────────────────────────
// Convenience transports
// ─────────────────────────────────────────────────────────────────────
//
// Two transports ship out of the box, both built on top of the
// structural `OpenCodeClient` interface:
//
//   - `createOpenCodeSubprocessClient` — spawns the runner as a CLI
//     subprocess (uses `node:child_process`). Default binary names:
//     `opencode` and `opencodego` in PATH; override via `binary`.
//
//   - `createOpenCodeHttpClient` — talks to a hosted HTTP endpoint
//     (uses `globalThis.fetch`). Hosts that pin a custom fetch can
//     inject it via `fetchFn`.
//
// Both are deliberately tiny — they own the transport, not the
// gateway-level concerns (caps, dedupe, idempotency-routing). Hosts
// that need richer transports (retries, custom auth, etc.) wrap them
// or replace them entirely.

/**
 * Wire protocol the subprocess speaks. the adapter picks JSON-lines over
 * stdio: one request JSON line in, one response JSON line out. The
 * `opencode` and `opencodego` runners expose this contract; if a
 * specific build does not, the host implements its own transport.
 */
interface OpenCodeSubprocessRequest {
  readonly version: 1;
  readonly model: string;
  readonly prompt: string;
  readonly maxOutputTokens: number;
}

/** Default timeout if the host doesn't set one — 60s is long enough for typical
 *  LLM calls but short enough that a hung runner releases the cap reservation
 *  rather than holding it indefinitely. */
const DEFAULT_SUBPROCESS_TIMEOUT_MS = 60_000;

export interface CreateOpenCodeSubprocessClientArgs {
  /**
   * Which runner is being wrapped. Picks the default binary name when
   * `binary` is not supplied: `"ts"` → `"opencode"`, `"go"` → `"opencodego"`.
   * The subprocess client is transport-only and has no other knowledge of
   * which provider variant will consume it, so a host wiring
   * `createOpenCodeGoProvider({ client: await createOpenCodeSubprocessClient() })`
   * MUST pass `kind: "go"` (or set `binary: "opencodego"` explicitly).
   * Default: `"ts"`.
   */
  readonly kind?: OpenCodeKind;
  /**
   * Path or name of the runner binary. Overrides the `kind`-derived
   * default. Pass an absolute path if the binary is outside PATH.
   */
  readonly binary?: string;
  /**
   * Extra command-line args forwarded to the runner before the
   * JSON-stdin handshake. Useful for `--config path/to/cfg.yaml`.
   */
  readonly args?: readonly string[];
  /**
   * Environment passed to the subprocess. The runner reads its API
   * keys from here, so the host owns the secret boundary.
   * Defaults to `process.env`.
   */
  readonly env?: NodeJS.ProcessEnv;
  /**
   * Hard timeout per chat call, in milliseconds. The subprocess is
   * killed and the promise rejects if the runner hangs reading stdin,
   * stalls between output and exit, or never writes to stdout. Without
   * this, a stuck runner pins the gateway's cap reservation
   * indefinitely. Default: 60_000 (60s).
   */
  readonly timeoutMs?: number;
}

export async function createOpenCodeSubprocessClient(
  args: CreateOpenCodeSubprocessClientArgs = {},
): Promise<OpenCodeClient> {
  const { spawn } = await import("node:child_process");
  const kind: OpenCodeKind = args.kind ?? "opencode";
  const defaultBinary = kind === "opencodego" ? "opencodego" : "opencode";
  const binary = args.binary ?? defaultBinary;
  const cliArgs = args.args ?? [];
  const env = args.env ?? process.env;
  const timeoutMs = args.timeoutMs ?? DEFAULT_SUBPROCESS_TIMEOUT_MS;
  return {
    async chat(req: OpenCodeChatRequest): Promise<OpenCodeChatResponse> {
      return await new Promise<OpenCodeChatResponse>((resolve, reject) => {
        const proc = spawn(binary, [...cliArgs], { env, stdio: ["pipe", "pipe", "pipe"] });
        const stdoutChunks: Buffer[] = [];
        const stderrChunks: Buffer[] = [];
        let timedOut = false;
        const timer = setTimeout(() => {
          timedOut = true;
          proc.kill("SIGKILL");
          reject(new Error(`opencode runner timed out after ${timeoutMs}ms`));
        }, timeoutMs);
        proc.stdout.on("data", (chunk: Buffer) => stdoutChunks.push(chunk));
        proc.stderr.on("data", (chunk: Buffer) => stderrChunks.push(chunk));
        proc.on("error", (err) => {
          clearTimeout(timer);
          if (!timedOut) reject(err);
        });
        proc.on("close", (code) => {
          clearTimeout(timer);
          if (timedOut) return;
          if (code !== 0) {
            const stderr = Buffer.concat(stderrChunks).toString("utf8");
            reject(new Error(`opencode runner exited ${code}: ${stderr.slice(0, 256)}`));
            return;
          }
          try {
            const parsed = JSON.parse(Buffer.concat(stdoutChunks).toString("utf8")) as unknown;
            resolve(parseSubprocessResponse(parsed));
          } catch (err) {
            reject(err instanceof Error ? err : new Error(String(err)));
          }
        });
        const wireRequest: OpenCodeSubprocessRequest = {
          version: 1,
          model: req.model,
          prompt: req.prompt,
          maxOutputTokens: req.maxOutputTokens,
        };
        proc.stdin.end(`${JSON.stringify(wireRequest)}\n`);
      });
    },
  };
}

function parseSubprocessResponse(raw: unknown): OpenCodeChatResponse {
  if (typeof raw !== "object" || raw === null) {
    throw new Error("opencode subprocess: response is not an object");
  }
  const obj = raw as OpenCodeSubprocessResponseShape;
  const text = typeof obj.text === "string" ? obj.text : "";
  const usageRaw = obj.usage;
  if (typeof usageRaw !== "object" || usageRaw === null) {
    throw new Error("opencode subprocess: response missing usage");
  }
  const usage = usageRaw as OpenCodeSubprocessUsageShape;
  const inputTokens = typeof usage.inputTokens === "number" ? usage.inputTokens : 0;
  const outputTokens = typeof usage.outputTokens === "number" ? usage.outputTokens : 0;
  const cachedInputTokens =
    typeof usage.cachedInputTokens === "number" ? usage.cachedInputTokens : undefined;
  const finishReason = typeof obj.finishReason === "string" ? obj.finishReason : undefined;
  const refusal = typeof obj.refusal === "string" ? obj.refusal : undefined;
  const model = typeof obj.model === "string" ? obj.model : undefined;
  return {
    text,
    usage: {
      inputTokens,
      outputTokens,
      ...(cachedInputTokens !== undefined ? { cachedInputTokens } : {}),
    },
    ...(model !== undefined ? { model } : {}),
    ...(finishReason !== undefined ? { finishReason } : {}),
    ...(refusal !== undefined ? { refusal } : {}),
  };
}

export interface CreateOpenCodeHttpClientArgs {
  /**
   * Base URL for the hosted runner. Must include scheme. The adapter
   * POSTs JSON requests to `${baseUrl}/chat` (path is fixed; if a
   * specific deployment uses a different path, the host wraps fetch
   * to rewrite it).
   */
  readonly baseUrl: string;
  /**
   * Optional headers (auth tokens etc.) merged into every request.
   */
  readonly headers?: Readonly<Record<string, string>>;
  /**
   * Inject a custom fetch (e.g. for retries, tracing). Defaults to
   * `globalThis.fetch`.
   */
  readonly fetchFn?: typeof globalThis.fetch;
}

export function createOpenCodeHttpClient(args: CreateOpenCodeHttpClientArgs): OpenCodeClient {
  if (typeof args.baseUrl !== "string" || args.baseUrl.length === 0) {
    throw new Error("createOpenCodeHttpClient: baseUrl required");
  }
  const fetchFn = args.fetchFn ?? globalThis.fetch;
  const baseHeaders = args.headers ?? {};
  return {
    async chat(req: OpenCodeChatRequest, options?: OpenCodeRequestOptions) {
      const res = await fetchFn(`${args.baseUrl.replace(/\/$/, "")}/chat`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...baseHeaders,
          ...(options?.headers ?? {}),
        },
        body: JSON.stringify({
          version: 1,
          model: req.model,
          prompt: req.prompt,
          maxOutputTokens: req.maxOutputTokens,
        }),
      });
      if (!res.ok) {
        const body = await res.text().catch(() => "");
        const err = new Error(`opencode http ${res.status}: ${body.slice(0, 256)}`);
        // Attach status so the gateway's classifySdkError-equivalent
        // can route 4xx to BadRequestError if the host adds that
        // mapping later.
        (err as { status?: number }).status = res.status;
        throw err;
      }
      const parsed = (await res.json()) as unknown;
      return parseSubprocessResponse(parsed);
    },
  };
}
