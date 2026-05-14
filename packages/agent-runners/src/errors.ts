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
