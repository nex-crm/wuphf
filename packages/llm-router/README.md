# @wuphf/llm-router

WUPHF v1 AI gateway — single in-process cost chokepoint. Per RFC §7.5,
every agent runner proxies through one `Gateway.complete()` call. The
gateway:

- enforces the per-office daily token ceiling ($5/day default, reads
  `cost_by_agent` from `@wuphf/broker`'s cost ledger)
- enforces the per-agent wake cap (12/hr default, in-memory)
- deduplicates identical payloads within a 60s window (SHA-256 of
  canonical request bytes)
- runs a circuit breaker (2 escalations in 10 min → opens for a
  cool-down period)
- short-circuits when idle mode is active (5 min of zero activity)
- writes one `cost_event` row **before** the response returns; the
  return value carries `costEventLsn` so the caller has proof

Direct provider SDKs are loaded by the gateway, never by agent
runners — there is no parallel call path to an LLM in this codebase.

## Providers

- `stub-fixed-cost` / `stub-error` — deterministic test targets for the
  §10.4 nightly burn-down. Every call costs 10000 micro-USD (= $0.01)
  and returns a canned response (or throws, for `stub-error`).
- **`anthropic` — `@anthropic-ai/sdk` adapter (PR B.2).** Subpath import:
  `import { createAnthropicProvider } from "@wuphf/llm-router/anthropic"`.
  Covers `claude-opus-4-1`, `claude-opus-4-{5,6,7}`, `claude-sonnet-4-{5,6}`,
  `claude-haiku-4-5` with built-in integer-μUSD pricing; hosts may
  override the pricing table for negotiated rates. The SDK is an
  **optional peer dependency** — hosts using only the stub do not
  install it.
- **`openai` — `openai` SDK adapter (PR B.3).** Subpath import:
  `import { createOpenAIProvider } from "@wuphf/llm-router/openai"`.
  Covers GPT-5 family (`gpt-5`, `gpt-5-mini`, `gpt-5-nano`) and GPT-4.1
  family. Splits `prompt_tokens_details.cached_tokens` out of
  `prompt_tokens` so the discounted cached-input rate applies correctly.
  SDK is an **optional peer dependency** — only installed if the host
  wants it.
- **`ollama` — `ollama` SDK adapter (PR B.4).** Subpath import:
  `import { createOllamaProvider } from "@wuphf/llm-router/ollama"`.
  Covers `llama3.3`, `llama3.2`, `llama3.1`, `qwen2.5`, `gemma2`,
  `mistral-small3.1`. **All default pricing is zero** — Ollama runs
  locally on the host's hardware, so there is no per-token provider
  charge. The `cost_event` row is still written for accounting
  uniformity (Hard rule #1). Hosts that want to model GPU/electricity
  cost override the pricing table with non-zero rates. SDK is an
  **optional peer dependency**.
- **`opencode` + `opencodego` — agent-runtime adapters (PR B.5).** Subpath
  import: `import { createOpenCodeProvider, createOpenCodeGoProvider }
  from "@wuphf/llm-router/opencode"`. `opencode` (TypeScript) and
  `opencodego` (Go port) are agent runners that wrap an underlying LLM
  provider; the gateway treats them as providers in their own right so
  the cost row, cap, breaker, and dedupe all run. **Two factory variants
  surface as separate `ProviderKind` values** (`"opencode"` vs
  `"opencodego"`) so the cost ledger can distinguish them. **Mixed
  topology** — both factories accept a structural `OpenCodeClient`; the
  package ships a subprocess transport (CLI over stdio) and an HTTP
  transport (POST to `/chat`). **Default pricing is zero** because cost
  depends on the configured backing model; hosts override the table to
  reflect real upstream spend. No new peer dependency.

## Usage

### Stub (tests, §10.4 burn-down)

```ts
import { createGateway, createStubProvider } from "@wuphf/llm-router";
import { createCostLedger } from "@wuphf/broker/cost-ledger";

const gateway = createGateway({
  ledger,                             // from @wuphf/broker/cost-ledger
  providers: [createStubProvider()],
  nowMs: () => Date.now(),
});

const result = await gateway.complete(ctx, request);
// result.costEventLsn is the proof the cost row was written.
```

### Anthropic (production)

First install the SDK alongside the router (it's an optional peer dep):

```bash
bun add @anthropic-ai/sdk @wuphf/llm-router
```

Then wire it:

```ts
import { createGateway } from "@wuphf/llm-router";
import { createAnthropicProviderWithKey } from "@wuphf/llm-router/anthropic";

// Read the key from your secret store. The constructor rejects an
// empty/non-string value; do NOT rely on `!` non-null assertions —
// they don't run at runtime.
const apiKey = process.env.ANTHROPIC_API_KEY;
if (typeof apiKey !== "string" || apiKey.length === 0) {
  throw new Error("ANTHROPIC_API_KEY is required");
}

const gateway = createGateway({
  ledger,
  providers: [
    await createAnthropicProviderWithKey({
      apiKey,
      // Optional: override pricing for negotiated rates.
      // pricing: { ... }
    }),
  ],
  nowMs: () => Date.now(),
});

const result = await gateway.complete(
  { agentSlug: asAgentSlug("primary") },
  { model: "claude-opus-4-7", prompt: "go", maxOutputTokens: 1024 },
);
```

`createAnthropicProviderWithKey` is `async` because the SDK module is
loaded lazily via dynamic import — hosts that never call this function
never pay the SDK's parse/load cost.

For tests, inject a fake client matching the `AnthropicClient`
interface (no SDK install needed):

```ts
import { createAnthropicProvider } from "@wuphf/llm-router/anthropic";

const provider = createAnthropicProvider({
  client: {
    messages: {
      create: async (params, options) => ({ content: [...], usage: { ... } }),
    },
  },
});
```

### OpenAI (production)

Install the SDK alongside the router:

```bash
bun add openai @wuphf/llm-router
```

```ts
import { createGateway } from "@wuphf/llm-router";
import { createOpenAIProviderWithKey } from "@wuphf/llm-router/openai";

const apiKey = process.env.OPENAI_API_KEY;
if (typeof apiKey !== "string" || apiKey.length === 0) {
  throw new Error("OPENAI_API_KEY is required");
}

const gateway = createGateway({
  ledger,
  providers: [
    await createOpenAIProviderWithKey({ apiKey }),
  ],
  nowMs: () => Date.now(),
});

const result = await gateway.complete(
  { agentSlug: asAgentSlug("primary") },
  { model: "gpt-5", prompt: "go", maxOutputTokens: 1024 },
);
```

The adapter splits OpenAI's `prompt_tokens_details.cached_tokens` out
of `prompt_tokens` so the discounted cached-input rate applies to the
cached subset (typically 10% of input cost on GPT-5 family).

### Ollama (local execution)

Ollama runs models on the host's own hardware. There is no API key, no
remote endpoint, and **no per-token provider charge** — default
pricing is zero across the board. The gateway still writes one
`cost_event` per call (amount 0) so `cost_by_agent` and the §10.4
projection stay uniform across providers.

Install the SDK alongside the router (optional peer dep):

```bash
bun add ollama @wuphf/llm-router
```

```ts
import { createGateway } from "@wuphf/llm-router";
import { createOllamaProviderWithUrl } from "@wuphf/llm-router/ollama";

const gateway = createGateway({
  ledger,
  providers: [
    // baseUrl defaults to http://localhost:11434.
    await createOllamaProviderWithUrl(),
  ],
  nowMs: () => Date.now(),
});

const result = await gateway.complete(
  { agentSlug: asAgentSlug("primary") },
  { model: "llama3.3", prompt: "go", maxOutputTokens: 1024 },
);
// result.costMicroUsd === 0 (local execution), but the cost_event row
// was written and `result.costEventLsn` is its LSN.
```

For a remote Ollama (LAN, SSH tunnel) pass an explicit URL:

```ts
await createOllamaProviderWithUrl({ baseUrl: "http://10.0.0.5:11434" });
```

Hosts that want to model local GPU / electricity cost can override the
pricing table; the integer-μUSD/MTok shape is identical to the other
adapters:

```ts
import { createOllamaProviderWithUrl } from "@wuphf/llm-router/ollama";

await createOllamaProviderWithUrl({
  pricing: {
    "llama3.3": {
      inputMicroUsdPerMTok: 100_000,  // $0.10/MTok GPU cost
      outputMicroUsdPerMTok: 200_000, // $0.20/MTok
      cacheReadMicroUsdPerMTok: 0,
      cacheCreationMicroUsdPerMTok: 0,
    },
  },
});
```

For tests, inject a fake client matching the `OllamaClient` interface
(no SDK install needed):

```ts
import { createOllamaProvider } from "@wuphf/llm-router/ollama";

const provider = createOllamaProvider({
  client: {
    chat: async (request) => ({
      model: "llama3.3",
      message: { role: "assistant", content: "ok" },
      done: true,
      prompt_eval_count: 100,
      eval_count: 50,
    }),
  },
});
```

Unlike the Anthropic/OpenAI adapters, the Ollama adapter does **not**
mint an idempotency key. Ollama is a local HTTP server with no
documented server-side dedupe contract, and a "retry against the same
local process" doesn't incur double billing because billing is $0.
The gateway's content-hash dedupe (60s sliding window) still applies
upstream.

### OpenCode + OpenCodeGo (mixed local-CLI / remote-HTTP topology)

`opencode` (TypeScript) and `opencodego` (Go) are agent runners.
Some hosts spawn them as **local CLI subprocesses** and talk over
stdio; others hit a **hosted HTTP endpoint**. The adapter accepts a
structural client so either transport works, and exposes two factory
variants so the cost ledger records each runner under its own
`ProviderKind`.

Subprocess transport (local CLI):

```ts
import { createGateway } from "@wuphf/llm-router";
import {
  createOpenCodeProvider,
  createOpenCodeSubprocessClient,
} from "@wuphf/llm-router/opencode";

const client = await createOpenCodeSubprocessClient({
  binary: "opencode", // or absolute path; defaults to PATH lookup
  // Optional: args, env override
});

const gateway = createGateway({
  ledger,
  providers: [
    createOpenCodeProvider({
      client,
      // Required if the host wants real cost accounting — defaults to
      // zero across the board.
      pricing: {
        "opencode-sonnet": {
          inputMicroUsdPerMTok: 3_000_000,   // $3/MTok
          outputMicroUsdPerMTok: 15_000_000, // $15/MTok
          cachedInputMicroUsdPerMTok: 300_000,
        },
      },
    }),
  ],
  nowMs: () => Date.now(),
});

const result = await gateway.complete(
  { agentSlug: asAgentSlug("primary") },
  { model: "opencode-sonnet", prompt: "go", maxOutputTokens: 1024 },
);
```

HTTP transport (hosted endpoint):

```ts
import {
  createOpenCodeGoProvider,
  createOpenCodeHttpClient,
} from "@wuphf/llm-router/opencode";

const client = createOpenCodeHttpClient({
  baseUrl: "http://opencodego.internal:9100",
  headers: { authorization: "Bearer <token>" },
});

const provider = createOpenCodeGoProvider({ client });
// Audit row will carry providerKind: "opencodego".
```

For tests, inject a fake client matching the `OpenCodeClient`
interface (no transport setup needed):

```ts
import { createOpenCodeProvider } from "@wuphf/llm-router/opencode";

const provider = createOpenCodeProvider({
  client: {
    chat: async (req, options) => ({
      text: "ok",
      usage: { inputTokens: 100, outputTokens: 50 },
      finishReason: "stop",
    }),
  },
});
```

The adapter **does** mint an `Idempotency-Key` header (subprocess
transports ignore headers; HTTP transports may use them for
server-side dedupe). The key is derived from `ProviderRequest.requestKey`
when present, otherwise from a sha256 of the canonical request bytes.

## §10.4 nightly burn-down

```bash
bun run burn:nightly
```

Runs 10 wakes/minute × 60 minutes against `stub-fixed-cost`. Asserts:

- Final spend is **$5.00 ± $0.05** (read-only ceiling held)
- Per-agent wake cap throttles **before** the office-daily cap
- Circuit breaker opens after **2 errors within a 10-minute window** (per the gateway's default `breakerErrorThreshold`)

Per RFC §10.4 these assertions are CI-blocking.

## Validation

```bash
bun run typecheck
bun run test
bun run check
```
