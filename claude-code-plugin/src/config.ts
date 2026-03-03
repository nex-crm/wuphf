/**
 * Plugin configuration — reads from environment variables.
 */

export interface NexConfig {
  apiKey: string;
  baseUrl: string;
}

export interface ScanConfig {
  extensions: string[];
  maxFileSize: number;
  maxFilesPerScan: number;
  scanDepth: number;
  ignoreDirs: string[];
  enabled: boolean;
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

  let baseUrl = process.env.NEX_API_BASE_URL ?? "https://app.nex.ai";
  // Strip trailing slash
  baseUrl = baseUrl.replace(/\/+$/, "");

  return { apiKey, baseUrl };
}

const DEFAULT_SCAN_EXTENSIONS = [".md", ".txt", ".csv", ".json", ".yaml", ".yml"];
const DEFAULT_IGNORE_DIRS = [
  "node_modules", ".git", "dist", "build", ".next", "__pycache__",
  "vendor", ".venv", ".claude", "coverage", ".turbo", ".cache",
];

/**
 * Load scan config from NEX_SCAN_* environment variables.
 * All fields have sensible defaults; NEX_SCAN_ENABLED=false is the kill switch.
 */
export function loadScanConfig(): ScanConfig {
  const enabled = (process.env.NEX_SCAN_ENABLED ?? "true").toLowerCase() !== "false";

  const extensions = process.env.NEX_SCAN_EXTENSIONS
    ? process.env.NEX_SCAN_EXTENSIONS.split(",").map((e) => e.trim())
    : DEFAULT_SCAN_EXTENSIONS;

  const maxFileSize = process.env.NEX_SCAN_MAX_FILE_SIZE
    ? parseInt(process.env.NEX_SCAN_MAX_FILE_SIZE, 10)
    : 100_000;

  const maxFilesPerScan = process.env.NEX_SCAN_MAX_FILES
    ? parseInt(process.env.NEX_SCAN_MAX_FILES, 10)
    : 5;

  const scanDepth = process.env.NEX_SCAN_DEPTH
    ? parseInt(process.env.NEX_SCAN_DEPTH, 10)
    : 2;

  const ignoreDirs = process.env.NEX_SCAN_IGNORE_DIRS
    ? process.env.NEX_SCAN_IGNORE_DIRS.split(",").map((d) => d.trim())
    : DEFAULT_IGNORE_DIRS;

  return { extensions, maxFileSize, maxFilesPerScan, scanDepth, ignoreDirs, enabled };
}
