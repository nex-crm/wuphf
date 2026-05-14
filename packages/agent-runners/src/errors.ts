import type { RunnerId } from "@wuphf/protocol";

export type RunnerSpawnErrorCode =
  | "claude_cli_not_available"
  | "codex_cli_not_available"
  | "credential_ownership_mismatch"
  | "endpoint_not_allowed"
  | "provider_kind_mismatch"
  | "receipt_write_failed"
  | "runner_lifecycle_error"
  | "runner_options_required"
  | "runner_spawn_failed";

export interface RunnerSpawnError {
  readonly code: RunnerSpawnErrorCode;
  readonly httpStatus: 400 | 403 | 500;
}

export class AgentRunnerError extends Error {
  constructor(
    message: string,
    readonly code: RunnerSpawnErrorCode,
    readonly httpStatus: 400 | 403 | 500 = 500,
    options?: ErrorOptions,
  ) {
    super(message, options);
    this.name = "AgentRunnerError";
  }
}

export class ClaudeCliNotAvailable extends AgentRunnerError {
  override readonly name = "ClaudeCliNotAvailable";

  constructor(message = "Claude CLI is not available", options?: ErrorOptions) {
    super(message, "claude_cli_not_available", 500, options);
  }
}

export class CodexCliNotAvailable extends AgentRunnerError {
  override readonly name = "CodexCliNotAvailable";

  constructor(message = "Codex CLI is not available", options?: ErrorOptions) {
    super(message, "codex_cli_not_available", 500, options);
  }
}

export class RunnerLifecycleError extends AgentRunnerError {
  override readonly name = "RunnerLifecycleError";

  constructor(message: string, options?: ErrorOptions) {
    super(message, "runner_lifecycle_error", 500, options);
  }
}

export class ReceiptWriteFailed extends AgentRunnerError {
  override readonly name = "ReceiptWriteFailed";

  constructor(runnerId: RunnerId, message: string, options?: ErrorOptions) {
    super(
      `runner ${runnerId}: receipt write failed: ${message}`,
      "receipt_write_failed",
      500,
      options,
    );
  }
}

export class RunnerSpawnFailed extends AgentRunnerError {
  override readonly name = "RunnerSpawnFailed";

  constructor(message: string, options?: ErrorOptions) {
    super(message, "runner_spawn_failed", 500, options);
  }
}

export class ProviderKindMismatch extends AgentRunnerError {
  override readonly name = "ProviderKindMismatch";

  constructor(message = "provider kind does not match credential scope", options?: ErrorOptions) {
    super(message, "provider_kind_mismatch", 400, options);
  }
}

export class RunnerOptionsRequired extends AgentRunnerError {
  override readonly name = "RunnerOptionsRequired";

  constructor(message = "runner options are required", options?: ErrorOptions) {
    super(message, "runner_options_required", 400, options);
  }
}

export class EndpointNotAllowed extends AgentRunnerError {
  override readonly name = "EndpointNotAllowed";

  constructor(
    readonly endpoint: string,
    readonly allowedOrigins: readonly string[],
    options?: ErrorOptions,
  ) {
    super(`endpoint is not allowed: ${endpoint}`, "endpoint_not_allowed", 403, options);
  }
}

export function isRunnerSpawnError(error: unknown): error is Error & RunnerSpawnError {
  if (!(error instanceof Error)) return false;
  const maybe = error as { readonly code?: unknown; readonly httpStatus?: unknown };
  return (
    typeof maybe.code === "string" &&
    (maybe.httpStatus === 400 || maybe.httpStatus === 403 || maybe.httpStatus === 500)
  );
}
