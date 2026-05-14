import type { RunnerId } from "@wuphf/protocol";

export class AgentRunnerError extends Error {
  constructor(
    message: string,
    readonly code: string,
    options?: ErrorOptions,
  ) {
    super(message, options);
    this.name = "AgentRunnerError";
  }
}

export class ClaudeCliNotAvailable extends AgentRunnerError {
  override readonly name = "ClaudeCliNotAvailable";

  constructor(message = "Claude CLI is not available", options?: ErrorOptions) {
    super(message, "claude_cli_not_available", options);
  }
}

export class CodexCliNotAvailable extends AgentRunnerError {
  override readonly name = "CodexCliNotAvailable";

  constructor(message = "Codex CLI is not available", options?: ErrorOptions) {
    super(message, "codex_cli_not_available", options);
  }
}

export class RunnerLifecycleError extends AgentRunnerError {
  override readonly name = "RunnerLifecycleError";

  constructor(message: string, options?: ErrorOptions) {
    super(message, "runner_lifecycle_error", options);
  }
}

export class ReceiptWriteFailed extends AgentRunnerError {
  override readonly name = "ReceiptWriteFailed";

  constructor(runnerId: RunnerId, message: string, options?: ErrorOptions) {
    super(`runner ${runnerId}: receipt write failed: ${message}`, "receipt_write_failed", options);
  }
}

export class RunnerSpawnFailed extends AgentRunnerError {
  override readonly name = "RunnerSpawnFailed";

  constructor(message: string, options?: ErrorOptions) {
    super(message, "runner_spawn_failed", options);
  }
}

export class RunnerOptionsRequired extends AgentRunnerError {
  override readonly name = "RunnerOptionsRequired";

  constructor(message = "runner options are required", options?: ErrorOptions) {
    super(message, "runner_options_required", options);
  }
}

export class EndpointNotAllowed extends AgentRunnerError {
  override readonly name = "EndpointNotAllowed";

  constructor(
    readonly endpoint: string,
    readonly allowedOrigins: readonly string[],
    options?: ErrorOptions,
  ) {
    super(`endpoint is not allowed: ${endpoint}`, "endpoint_not_allowed", options);
  }
}
