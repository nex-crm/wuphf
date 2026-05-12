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
- `ollama` — future PR.

## Usage

### Stub (tests, §10.4 burn-down)

```ts
import { createGateway, createStubProvider } from "@wuphf/llm-router";
import { createCostLedger } from "@wuphf/broker";

const gateway = createGateway({
  ledger,                             // from @wuphf/broker
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

## §10.4 nightly burn-down

```bash
bun run burn:nightly
```

Runs 10 wakes/minute × 60 minutes against `stub-fixed-cost`. Asserts:

- Final spend is **$5.00 ± $0.05** (read-only ceiling held)
- Per-agent wake cap throttles **before** the office-daily cap
- Circuit breaker fires within **3 consecutive failures**

Per RFC §10.4 these assertions are CI-blocking.

## Validation

```bash
bun run typecheck
bun run test
bun run check
```
