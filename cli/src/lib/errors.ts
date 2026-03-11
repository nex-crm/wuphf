/**
 * Error types for Nex CLI.
 * Exit codes: 0 = success, 1 = general error, 2 = auth error.
 */

export class AuthError extends Error {
  public exitCode = 2;

  constructor(message = "No API key configured. Run 'nex setup' or set NEX_API_KEY.") {
    super(message);
    this.name = "AuthError";
  }
}

export class RateLimitError extends Error {
  public exitCode = 1;
  public retryAfterMs: number;

  constructor(retryAfterMs = 60_000) {
    super(`Rate limited — retry after ${Math.ceil(retryAfterMs / 1000)}s`);
    this.name = "RateLimitError";
    this.retryAfterMs = retryAfterMs;
  }
}

export class ServerError extends Error {
  public exitCode = 1;
  public status: number;

  constructor(status: number, body?: string) {
    super(`Nex API error ${status}${body ? `: ${body}` : ""}`);
    this.name = "ServerError";
    this.status = status;
  }
}
