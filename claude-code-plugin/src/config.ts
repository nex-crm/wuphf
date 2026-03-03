/**
 * Plugin configuration — reads from environment variables.
 */

export interface NexConfig {
  apiKey: string;
  baseUrl: string;
}

export class ConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ConfigError";
  }
}

/**
 * Load config from environment variables.
 * - NEX_API_KEY: required
 * - NEX_API_BASE_URL: optional (default: https://api.nex-crm.com)
 */
export function loadConfig(): NexConfig {
  const apiKey = process.env.NEX_API_KEY;
  if (!apiKey) {
    throw new ConfigError(
      "NEX_API_KEY environment variable is required. Export it before using the Nex memory plugin."
    );
  }

  let baseUrl = process.env.NEX_API_BASE_URL ?? "https://api.nex.ai";
  // Strip trailing slash
  baseUrl = baseUrl.replace(/\/+$/, "");

  return { apiKey, baseUrl };
}
