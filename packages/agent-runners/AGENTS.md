# @wuphf/agent-runners — Agent Guidelines

This package owns subprocess-backed agent execution for WUPHF v1. It is allowed
to spawn local CLIs and stream their output, but it must not own credential
authority.

## Hard rules

1. **Runners never hold a `BrokerIdentity`.** The broker resolves the
   `CredentialHandle` and injects a `secretReader` closure. Runners call that
   closure exactly once at spawn time and never import `@wuphf/credentials`.
2. **Receipt write failure fails the runner.** A missing or failed receipt is a
   failed task, not a best-effort log. Emit a `failed` `RunnerEvent` and do not
   emit `finished`.
3. **Subprocesses must exit cleanly on `terminate()`.** Send a graceful signal,
   wait for actual exit, hard-kill after the grace period, drain final events,
   and close streams. No zombies and no leaked file descriptors.
4. **`RunnerEvent` is the wire surface.** Any change to runner request/event
   shape belongs in `@wuphf/protocol`, must reject unknown keys, and needs
   golden vectors in `packages/protocol/testdata/runner-vectors.json`.
5. **Lifecycle state has one writer.** The v0 race at
   `internal/agent/service.go:192-199` was a status-write/event-drain race. v1
   prevents it with one owned `LifecycleStateMachine` per runner:
   `Pending -> Running -> Stopping -> Stopped`. Consumers read the
   `ReadableStream`; they never write state. `terminate()` resolves only after
   the subprocess has actually exited.

## Validation

```bash
bunx tsc --noEmit
bunx vitest run
bunx biome check src/ tests/
```
