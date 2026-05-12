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

/**
 * Optional structured metadata an adapter can attach when wrapping a
 * provider-side error. None are required — older adapters that don't
 * surface these stay backwards-compatible — but a real provider (e.g.
 * Anthropic) SHOULD populate them so on-call has parseable failure
 * info. See triangulation #2 finding B2-5.
 */
export interface ProviderErrorMetadata {
  /** HTTP status (or other transport status) when applicable. */
  readonly status?: number;
  /** Provider's request id, when the SDK exposes it. */
  readonly requestId?: string;
  /** Provider-specific error-type discriminator (e.g. "rate_limit_error"). */
  readonly errorType?: string;
  /** Milliseconds the caller should wait before retrying (e.g. 429 retry-after). */
  readonly retryAfterMs?: number;
}

export class ProviderError extends Error {
  readonly code = "provider_error";
  readonly providerKind: string;
  override readonly cause: unknown;
  readonly status?: number;
  readonly requestId?: string;
  readonly errorType?: string;
  readonly retryAfterMs?: number;
  constructor(providerKind: string, cause: unknown, metadata: ProviderErrorMetadata = {}) {
    const causeMsg = cause instanceof Error ? cause.message : String(cause);
    super(`provider_error: kind=${providerKind} cause=${causeMsg}`);
    this.name = "ProviderError";
    this.providerKind = providerKind;
    this.cause = cause;
    if (metadata.status !== undefined) this.status = metadata.status;
    if (metadata.requestId !== undefined) this.requestId = metadata.requestId;
    if (metadata.errorType !== undefined) this.errorType = metadata.errorType;
    if (metadata.retryAfterMs !== undefined) this.retryAfterMs = metadata.retryAfterMs;
  }
}

/**
 * Caller-input error: the provider rejected the request as malformed
 * (400, 413, 422 in HTTP terms). The gateway does NOT count this as a
 * breaker strike — bad input from one caller shouldn't open the
 * breaker for the whole agent. See triangulation #2 finding B2-7.
 */
export class BadRequestError extends Error {
  readonly code = "bad_request";
  readonly providerKind: string;
  override readonly cause: unknown;
  readonly status?: number;
  readonly requestId?: string;
  readonly errorType?: string;
  constructor(providerKind: string, cause: unknown, metadata: ProviderErrorMetadata = {}) {
    const causeMsg = cause instanceof Error ? cause.message : String(cause);
    super(`bad_request: kind=${providerKind} cause=${causeMsg}`);
    this.name = "BadRequestError";
    this.providerKind = providerKind;
    this.cause = cause;
    if (metadata.status !== undefined) this.status = metadata.status;
    if (metadata.requestId !== undefined) this.requestId = metadata.requestId;
    if (metadata.errorType !== undefined) this.errorType = metadata.errorType;
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
  | BadRequestError
  | CapExceededError
  | CircuitBreakerOpenError
  | IdleModeError
  | ProviderError
  | UnknownModelError;

export function isGatewayError(value: unknown): value is GatewayError {
  return (
    value instanceof BadRequestError ||
    value instanceof CapExceededError ||
    value instanceof CircuitBreakerOpenError ||
    value instanceof IdleModeError ||
    value instanceof ProviderError ||
    value instanceof UnknownModelError
  );
}
