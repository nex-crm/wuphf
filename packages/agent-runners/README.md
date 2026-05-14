# @wuphf/agent-runners

Agent runners for WUPHF v1.

The package freezes the `AgentRunner` interface and ships the first concrete
adapter, Claude CLI in `--print` streaming JSON mode. Codex CLI and
OpenAI-compatible adapters plug into the same `SpawnAgentRunner` function.

## Threat Model

Runners are untrusted execution clients. They receive a pre-resolved
`CredentialHandle` plus a broker-injected `secretReader` closure; they never
hold a `BrokerIdentity` and never import `@wuphf/credentials`. The adapter reads
the secret once at spawn time, injects it as `ANTHROPIC_API_KEY`, and then only
emits `RunnerEvent` values from `@wuphf/protocol`.

Receipt writes are authoritative. If `receiptStore.put` throws or reports that
the receipt was not stored, the run emits `failed` and does not emit `finished`.
This is the v1 guard against the v0 `appendTaskLogEntry` best-effort receipt
anti-pattern.

Lifecycle state is owned by the runner fiber. Output consumers subscribe to a
`ReadableStream<RunnerEvent>` but cannot mutate state, and `terminate()` waits
for the subprocess to actually exit before resolving. This structurally avoids
the v0 status-write/event-drain race.

## Claude CLI Adapter

`createClaudeCliRunner()` resolves the absolute Claude CLI path at adapter
construction, rejects group/world-writable binaries, and spawns directly with
`node:child_process.spawn`. Tests use the injectable `Spawner` seam and do not
require a real Claude installation.

Claude JSON lines are parsed as they stream. Text deltas emit `stdout` events.
Usage objects emit `cost` events as they arrive; Claude reports usage at the end
of each message, so cost placement follows those message boundaries. The event
queue is capped at 1000 retained events for replay to late subscribers; slow
live consumers rely on `ReadableStream` backpressure.

Real CLI smoke coverage is deferred until a gated
`WUPHF_REAL_CLAUDE_CLI=1` test lands in a follow-up.

## OpenAI-Compatible HTTP Adapter

`createOpenAICompatRunner()` drives chat-completions endpoints that implement
OpenAI-style streaming SSE. It does not spawn a subprocess; the runner is the
in-flight HTTP request, and `terminate()` aborts through `AbortController`.

The wire-frozen `RunnerSpawnRequest` has no provider-options field, so this
adapter exposes a typed local extension:

```ts
import {
  createOpenAICompatRunner,
  type OpenAICompatRunnerSpawnRequest,
} from "@wuphf/agent-runners";

const request: OpenAICompatRunnerSpawnRequest = {
  kind: "openai-compat",
  agentId,
  credential,
  prompt: "Summarize the change",
  model: "gpt-5-mini",
  options: {
    endpoint: "https://api.openai.com/v1/chat/completions",
    headers: { "OpenAI-Organization": "org_..." },
    timeoutMs: 60_000,
  },
};
```

The broker-provided secret is read once at spawn time. Scope `openai` and
`openai-compat` use `Authorization: Bearer <secret>`; scope `anthropic` uses
`x-api-key: <secret>`. Other scopes fall back to bearer auth and emit a
structured `stderr` event documenting the assumption.

SSE `delta.content` chunks become `stdout` events. A provider-reported `usage`
object becomes a cost ledger entry and `cost` event; if the provider omits
usage, the adapter records a zero-cost entry and marks the emitted cost event
with `note: "provider_did_not_report_usage"` for operational visibility.

Retries are intentionally not implemented in the adapter. TODO(#NEW): add a
cost-ledger-aware, idempotency-aware retry middleware above concrete adapters.
