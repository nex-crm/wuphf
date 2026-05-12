// Typed errors thrown by the gateway. Every caller-visible failure has
// a concrete subclass with a stable `code` so the PR C wire layer can
// map them to HTTP without sniffing message strings.

export type CapKind = "daily" | "wake";

export class CapExceededError extends Error {
  readonly code = "cap_exceeded";
  constructor(
    readonly cap: CapKind,
    readonly observedMicroUsd: number | null,
    readonly limitMicroUsd: number | null,
    readonly retryAfterMs: number,
  ) {
    super(
      `cap_exceeded: ${cap}` +
        (observedMicroUsd !== null && limitMicroUsd !== null
          ? ` (observed=${observedMicroUsd}, limit=${limitMicroUsd})`
          : "") +
        `, retry_after_ms=${retryAfterMs}`,
    );
    this.name = "CapExceededError";
  }
}

export class CircuitBreakerOpenError extends Error {
  readonly code = "breaker_open";
  constructor(readonly cooldownEndsMs: number) {
    super(`breaker_open: cooldown_ends_ms=${cooldownEndsMs}`);
    this.name = "CircuitBreakerOpenError";
  }
}

export class IdleModeError extends Error {
  readonly code = "idle_mode";
  constructor(readonly idleSinceMs: number) {
    super(`idle_mode: idle_since_ms=${idleSinceMs}`);
    this.name = "IdleModeError";
  }
}

export class ProviderError extends Error {
  readonly code = "provider_error";
  readonly providerKind: string;
  override readonly cause: unknown;
  constructor(providerKind: string, cause: unknown) {
    const causeMsg = cause instanceof Error ? cause.message : String(cause);
    super(`provider_error: kind=${providerKind} cause=${causeMsg}`);
    this.name = "ProviderError";
    this.providerKind = providerKind;
    this.cause = cause;
  }
}

export class UnknownModelError extends Error {
  readonly code = "unknown_model";
  constructor(readonly model: string) {
    super(`unknown_model: ${JSON.stringify(model)}`);
    this.name = "UnknownModelError";
  }
}

/**
 * Type guard for the gateway's own typed errors. Callers can switch on
 * `.code` to decide retry behavior without `instanceof` chains.
 */
export type GatewayError =
  | CapExceededError
  | CircuitBreakerOpenError
  | IdleModeError
  | ProviderError
  | UnknownModelError;

export function isGatewayError(value: unknown): value is GatewayError {
  return (
    value instanceof CapExceededError ||
    value instanceof CircuitBreakerOpenError ||
    value instanceof IdleModeError ||
    value instanceof ProviderError ||
    value instanceof UnknownModelError
  );
}
