# @wuphf/llm-router — Agent Guidelines

Branch-7 slice: the single in-process AI gateway. All agent runners go
through `Gateway.complete()` — there is no other call path to a provider
SDK in the broker process. Branch-7 PR B (`feat/cost-ceiling-supervisor-core`)
is where the gateway, cost-event emission, cap enforcement, and the §10.4
burn-down land.

## Hard rules

1. **Every successful `Gateway.complete()` writes one `cost_event` BEFORE
   returning.** No row, no response. The return value carries
   `costEventLsn: EventLsn` so the caller can prove the write happened.
   If `ledger.appendCostEvent` throws, the response is discarded.
2. **All amounts are integer `MicroUsd`.** Per `@wuphf/protocol/cost.ts`
   the §15.A sum invariant is only decidable on integers. Pricing
   tables are `Record<string, MicroUsdPerUnit>` lookups. Never use
   float dollars.
3. **The daily cap reads from `cost_by_agent`, not from in-memory
   counters.** The projection is the source of truth; in-memory state
   would diverge after a broker restart. Wake caps and the breaker may
   stay process-local (they reset on restart by design).
4. **No `Date.now()` for ordering or identity.** Per protocol AGENTS.md
   rule 14. Wake-cap timestamps and breaker state may use the wall
   clock for "did this happen in the last 10 minutes," but the
   cost_event LSN comes from the ledger and `crossedAt` is the
   triggering event's `occurredAt`.
5. **Identical-payload dedupe uses a content hash, not a sequence
   number.** SHA-256 of the canonical request bytes. The 60s window is
   a sliding window: an entry expires when the wall clock crosses its
   stored deadline.
6. **The stub provider is deterministic.** §10.4 (`ci:burn:nightly`)
   asserts $5.00 ± $0.05 across 60 minutes; that contract requires
   exact-amount predictability. `stub-fixed-cost` returns 10000
   micro-USD/call with zero variance.
7. **No `electron`, no renderer code.** The gateway is pure Node.
   Renderer surfaces (cost tile, throttle banners) live in PR C
   `feat/cost-ceiling-supervisor-ui`.
8. **Type-system enforcement of "row before response."** The gateway's
   public return type couples to the ledger write: `GatewayCompletionResult`
   carries `costEventLsn`, and the only code that mints one is the
   gateway implementation calling `ledger.appendCostEvent`. Do not
   widen the return type to make tests pass.

## Cap classes and error mapping

| Cap | Throws | HTTP equivalent (PR C wire) |
|---|---|---|
| Per-office daily cap | `CapExceededError("daily")` | 429 Retry-After: tomorrow 00:00 UTC |
| Per-agent wake cap | `CapExceededError("wake")` | 429 Retry-After: window-end |
| Circuit breaker open | `CircuitBreakerOpenError` | 503 Retry-After: breaker-cooldown-end |
| Idle mode | `IdleModeError` | 423 Locked |
| Provider error | `ProviderError` | 502 Bad Gateway |
| Dedupe replay | (no throw — returns cached `GatewayCompletionResult`) | 200 with `X-Dedupe-Replay: true` |

## Adding a new provider

1. Implement `Provider` in `src/providers/<name>.ts`.
2. Register a `CostEstimator` for every model the provider exposes; pricing
   tables MUST be integer micro-USD per token.
3. Add a unit test that asserts deterministic cost given fixed usage.
4. Add the `ProviderKind` literal to `@wuphf/protocol`'s `PROVIDER_KIND_VALUES`
   if it isn't already there (closed enum — see protocol AGENTS.md).

## Validation

```bash
bun run typecheck
bun run test
bun run check
```

Burn-down (slow, separate command):

```bash
bun run burn:nightly
```
