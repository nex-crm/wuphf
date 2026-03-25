/**
 * Plugin configuration parsing and validation.
 * Resolves API key from config, env var, or ${VAR} interpolation.
 */

export interface NexPluginConfig {
  apiKey: string;
  baseUrl: string;
  autoRecall: boolean;
  autoCapture: boolean;
  captureMode: "last_turn" | "full_session";
  maxRecallResults: number;
  sessionTracking: boolean;
  recallTimeoutMs: number;
  debug: boolean;
}

const DEFAULTS: Omit<NexPluginConfig, "apiKey"> = {
  baseUrl: "https://app.nex.ai",
  autoRecall: true,
  autoCapture: true,
  captureMode: "last_turn",
  maxRecallResults: 5,
  sessionTracking: true,
  recallTimeoutMs: 1500,
  debug: false,
};

/** Resolve ${VAR_NAME} patterns in a string value. */
function resolveEnvVars(value: string): string {
  return value.replace(/\$\{([^}]+)\}/g, (_, varName: string) => {
    return process.env[varName.trim()] ?? "";
  });
}

export class ConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ConfigError";
  }
}

/**
 * Parse raw plugin config into a validated NexPluginConfig.
 * Falls back to process.env.WUPHF_API_KEY if no apiKey in config.
 */
export function parseConfig(raw?: Record<string, unknown>): NexPluginConfig {
  const cfg = raw ?? {};

  // Resolve API key: config → env var interpolation → WUPHF_API_KEY env
  let apiKey = typeof cfg.apiKey === "string" ? resolveEnvVars(cfg.apiKey) : undefined;
  if (!apiKey) {
    apiKey = process.env.WUPHF_API_KEY;
  }
  if (!apiKey) {
    throw new ConfigError(
      "No API key configured. Set 'apiKey' in plugin config or export WUPHF_API_KEY environment variable."
    );
  }

  let baseUrl = process.env.WUPHF_DEV_URL
    ?? (typeof cfg.baseUrl === "string" ? resolveEnvVars(cfg.baseUrl).replace(/\/+$/, "") : undefined)
    ?? DEFAULTS.baseUrl;

  const captureMode = cfg.captureMode as string | undefined;
  if (captureMode !== undefined && captureMode !== "last_turn" && captureMode !== "full_session") {
    throw new ConfigError(`Invalid captureMode: "${captureMode}". Must be "last_turn" or "full_session".`);
  }

  const maxRecallResults = typeof cfg.maxRecallResults === "number" ? cfg.maxRecallResults : DEFAULTS.maxRecallResults;
  if (maxRecallResults < 1 || maxRecallResults > 20) {
    throw new ConfigError(`maxRecallResults must be between 1 and 20, got ${maxRecallResults}.`);
  }

  const recallTimeoutMs = typeof cfg.recallTimeoutMs === "number" ? cfg.recallTimeoutMs : DEFAULTS.recallTimeoutMs;
  if (recallTimeoutMs < 500 || recallTimeoutMs > 10000) {
    throw new ConfigError(`recallTimeoutMs must be between 500 and 10000, got ${recallTimeoutMs}.`);
  }

  return {
    apiKey,
    baseUrl,
    autoRecall: typeof cfg.autoRecall === "boolean" ? cfg.autoRecall : DEFAULTS.autoRecall,
    autoCapture: typeof cfg.autoCapture === "boolean" ? cfg.autoCapture : DEFAULTS.autoCapture,
    captureMode: (captureMode as NexPluginConfig["captureMode"]) ?? DEFAULTS.captureMode,
    maxRecallResults,
    sessionTracking: typeof cfg.sessionTracking === "boolean" ? cfg.sessionTracking : DEFAULTS.sessionTracking,
    recallTimeoutMs,
    debug: typeof cfg.debug === "boolean" ? cfg.debug : DEFAULTS.debug,
  };
}
