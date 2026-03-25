/**
 * Plugin configuration — reads from environment variables,
 * with fallback to ~/.wuphf-mcp.json (shared with MCP server).
 */

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, dirname } from "node:path";
import { homedir } from "node:os";

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

/** Shared config file with MCP server — stores registration data. */
const MCP_CONFIG_PATH = join(homedir(), ".wuphf-mcp.json");

export { MCP_CONFIG_PATH };

interface McpConfig {
  api_key?: string;
  base_url?: string;
  workspace_id?: string;
  workspace_slug?: string;
}

/** Read ~/.wuphf-mcp.json (shared with MCP server registration). */
export function loadMcpConfig(): McpConfig {
  try {
    const raw = readFileSync(MCP_CONFIG_PATH, "utf-8");
    return JSON.parse(raw) as McpConfig;
  } catch {
    return {};
  }
}

/** Write registration data to ~/.wuphf-mcp.json. */
export function persistRegistration(data: Record<string, unknown>): void {
  const existing = loadMcpConfig() as Record<string, unknown>;
  if (typeof data.api_key === "string") existing.api_key = data.api_key;
  if (typeof data.workspace_id === "string" || typeof data.workspace_id === "number") {
    existing.workspace_id = String(data.workspace_id);
  }
  if (typeof data.workspace_slug === "string") existing.workspace_slug = data.workspace_slug;
  mkdirSync(dirname(MCP_CONFIG_PATH), { recursive: true });
  writeFileSync(MCP_CONFIG_PATH, JSON.stringify(existing, null, 2) + "\n", "utf-8");
}

/**
 * Load config from environment variables, with fallback to ~/.wuphf-mcp.json.
 *
 * Priority: WUPHF_API_KEY env > ~/.wuphf-mcp.json api_key
 * If neither is set, throws ConfigError with registration instructions.
 */
export function loadConfig(): NexConfig {
  let apiKey = process.env.WUPHF_API_KEY;

  if (!apiKey) {
    // Fallback to shared MCP config
    const mcpConfig = loadMcpConfig();
    apiKey = mcpConfig.api_key;
  }

  if (!apiKey) {
    throw new ConfigError(
      "No API key found. Set WUPHF_API_KEY or run /register to create an account."
    );
  }

  let baseUrl = process.env.WUPHF_API_BASE_URL ?? "https://app.nex.ai";
  // Strip trailing slash
  baseUrl = baseUrl.replace(/\/+$/, "");

  return { apiKey, baseUrl };
}

/**
 * Load base URL without requiring an API key.
 * Used for registration (which doesn't need auth).
 */
export function loadBaseUrl(): string {
  let baseUrl = process.env.WUPHF_API_BASE_URL ?? "https://app.nex.ai";
  return baseUrl.replace(/\/+$/, "");
}

// --- .wuphf.toml project config support ---

interface HookTomlConfig {
  enabled?: boolean;
  debounce_ms?: number;
  min_length?: number;
  max_length?: number;
}

interface ProjectTomlConfig {
  hooks?: {
    enabled?: boolean;
    recall?: HookTomlConfig;
    capture?: HookTomlConfig;
    session_start?: HookTomlConfig;
  };
}

function parseTomlValue(raw: string): unknown {
  if (raw === "true") return true;
  if (raw === "false") return false;
  if (/^-?\d+(\.\d+)?$/.test(raw)) return Number(raw);
  if ((raw.startsWith('"') && raw.endsWith('"')) || (raw.startsWith("'") && raw.endsWith("'"))) {
    return raw.slice(1, -1);
  }
  if (raw.startsWith("[") && raw.endsWith("]")) {
    const inner = raw.slice(1, -1).trim();
    if (!inner) return [];
    return inner.split(",").map((item) => parseTomlValue(item.trim()));
  }
  return raw;
}

function parseToml(content: string): Record<string, unknown> {
  const obj: Record<string, unknown> = {};
  const currentSection: string[] = [];

  for (const line of content.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;

    const sectionMatch = trimmed.match(/^\[([^\]]+)\]$/);
    if (sectionMatch) {
      currentSection.length = 0;
      currentSection.push(...sectionMatch[1].split("."));
      continue;
    }

    const kvMatch = trimmed.match(/^([a-zA-Z_][a-zA-Z0-9_.]*)\s*=\s*(.+)$/);
    if (!kvMatch) continue;

    const path = [...currentSection, ...kvMatch[1].trim().split(".")];
    const value = parseTomlValue(kvMatch[2].trim());

    let target: Record<string, unknown> = obj;
    for (let i = 0; i < path.length - 1; i++) {
      if (!(path[i] in target) || typeof target[path[i]] !== "object") {
        target[path[i]] = {};
      }
      target = target[path[i]] as Record<string, unknown>;
    }
    target[path[path.length - 1]] = value;
  }
  return obj;
}

/**
 * Check if a specific hook is enabled in .wuphf.toml.
 * Returns true by default (hooks are opt-out).
 */
export function isHookEnabled(hookName: "recall" | "capture" | "session_start"): boolean {
  try {
    const tomlPath = join(process.cwd(), ".wuphf.toml");
    const content = readFileSync(tomlPath, "utf-8");
    const config = parseToml(content) as ProjectTomlConfig;

    // Master kill switch
    if (config.hooks?.enabled === false) return false;

    // Per-hook setting
    const hookConfig = config.hooks?.[hookName];
    if (hookConfig?.enabled === false) return false;

    return true;
  } catch {
    return true; // No .wuphf.toml or read error → hooks enabled by default
  }
}

const DEFAULT_SCAN_EXTENSIONS = [".md", ".txt", ".csv", ".json", ".yaml", ".yml"];
const DEFAULT_IGNORE_DIRS = [
  "node_modules", ".git", "dist", "build", ".next", "__pycache__",
  "vendor", ".venv", ".claude", "coverage", ".turbo", ".cache",
];

/**
 * Load scan config from WUPHF_SCAN_* environment variables.
 * All fields have sensible defaults; WUPHF_SCAN_ENABLED=false is the kill switch.
 */
export function loadScanConfig(): ScanConfig {
  const enabled = (process.env.WUPHF_SCAN_ENABLED ?? "true").toLowerCase() !== "false";

  const extensions = process.env.WUPHF_SCAN_EXTENSIONS
    ? process.env.WUPHF_SCAN_EXTENSIONS.split(",").map((e) => e.trim())
    : DEFAULT_SCAN_EXTENSIONS;

  const maxFileSize = process.env.WUPHF_SCAN_MAX_FILE_SIZE
    ? parseInt(process.env.WUPHF_SCAN_MAX_FILE_SIZE, 10)
    : 100_000;

  const maxFilesPerScan = process.env.WUPHF_SCAN_MAX_FILES
    ? parseInt(process.env.WUPHF_SCAN_MAX_FILES, 10)
    : 5;

  const scanDepth = process.env.WUPHF_SCAN_DEPTH
    ? parseInt(process.env.WUPHF_SCAN_DEPTH, 10)
    : 2;

  const ignoreDirs = process.env.WUPHF_SCAN_IGNORE_DIRS
    ? process.env.WUPHF_SCAN_IGNORE_DIRS.split(",").map((d) => d.trim())
    : DEFAULT_IGNORE_DIRS;

  return { extensions, maxFileSize, maxFilesPerScan, scanDepth, ignoreDirs, enabled };
}
