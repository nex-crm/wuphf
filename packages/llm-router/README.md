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

- `stub-fixed-cost` — deterministic test target for the §10.4 nightly
  burn-down. Every call costs 10000 micro-USD (= $0.01) and returns a
  canned response.
- `anthropic` (PR B.2 follow-up) — `@anthropic-ai/sdk` adapter.
- `openai` (PR B.3 follow-up) — `openai` SDK adapter.
- `ollama` (PR B.3 follow-up) — local model adapter.

## Usage

```ts
import { createGateway } from "@wuphf/llm-router";
import { createCostLedger, createCommandIdempotencyStore } from "@wuphf/broker";

const gateway = createGateway({
  ledger,                             // from @wuphf/broker
  providers: [createStubProvider()],
  caps: {
    dailyMicroUsd: 5_000_000,         // $5/day per RFC §8
    wakeCapPerHour: 12,
    breakerErrorThreshold: 2,
    breakerWindowMs: 10 * 60 * 1000,
    breakerCooldownMs: 5 * 60 * 1000,
    idleThresholdMs: 5 * 60 * 1000,
    dedupeWindowMs: 60 * 1000,
  },
  nowMs: () => Date.now(),
});

const result = await gateway.complete(ctx, request);
// result.costEventLsn is the proof the cost row was written.
```

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
