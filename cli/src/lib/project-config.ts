/**
 * .wuphf.toml project config reader/writer.
 *
 * Resolution: CLI flags > .wuphf.toml > env vars > ~/.wuphf/config.json > defaults
 */

import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { join } from "node:path";

// --- Minimal TOML parser (read-only, no dependency) ---
// Handles flat keys, dotted keys, strings, numbers, booleans, arrays.
// Good enough for .wuphf.toml — not a full TOML spec parser.

function parseLine(
  line: string,
  obj: Record<string, unknown>,
  currentSection: string[],
): void {
  const trimmed = line.trim();
  if (!trimmed || trimmed.startsWith("#")) return;

  // Section header: [hooks.recall]
  const sectionMatch = trimmed.match(/^\[([^\]]+)\]$/);
  if (sectionMatch) {
    currentSection.length = 0;
    currentSection.push(...sectionMatch[1].split("."));
    return;
  }

  // Key = value
  const kvMatch = trimmed.match(/^([a-zA-Z_][a-zA-Z0-9_.]*)\s*=\s*(.+)$/);
  if (!kvMatch) return;

  const key = kvMatch[1].trim();
  const rawValue = kvMatch[2].trim();
  const value = parseValue(rawValue);

  // Build nested path
  const path = [...currentSection, ...key.split(".")];
  let target: Record<string, unknown> = obj;
  for (let i = 0; i < path.length - 1; i++) {
    if (!(path[i] in target) || typeof target[path[i]] !== "object") {
      target[path[i]] = {};
    }
    target = target[path[i]] as Record<string, unknown>;
  }
  target[path[path.length - 1]] = value;
}

function parseValue(raw: string): unknown {
  // Boolean
  if (raw === "true") return true;
  if (raw === "false") return false;

  // Number
  if (/^-?\d+(\.\d+)?$/.test(raw)) return Number(raw);

  // String (quoted)
  if ((raw.startsWith('"') && raw.endsWith('"')) || (raw.startsWith("'") && raw.endsWith("'"))) {
    return raw.slice(1, -1);
  }

  // Array
  if (raw.startsWith("[") && raw.endsWith("]")) {
    const inner = raw.slice(1, -1).trim();
    if (!inner) return [];
    return inner.split(",").map((item) => parseValue(item.trim()));
  }

  // Bare string fallback
  return raw;
}

export function parseToml(content: string): Record<string, unknown> {
  const obj: Record<string, unknown> = {};
  const currentSection: string[] = [];
  for (const line of content.split("\n")) {
    parseLine(line, obj, currentSection);
  }
  return obj;
}

// --- Project config interface ---

export interface HookConfig {
  enabled?: boolean;
  debounce_ms?: number;
  min_length?: number;
  max_length?: number;
}

export interface ProjectConfig {
  auth?: { api_key?: string };
  hooks?: {
    enabled?: boolean;
    recall?: HookConfig;
    capture?: HookConfig;
    session_start?: HookConfig;
  };
  scan?: {
    enabled?: boolean;
    extensions?: string[];
    ignore_dirs?: string[];
    max_files?: number;
    max_file_size?: number;
    depth?: number;
  };
  mcp?: { enabled?: boolean };
  output?: { format?: string; timeout?: number };
}

/**
 * Find and parse .wuphf.toml from the given directory (defaults to cwd).
 * Returns undefined if no .wuphf.toml is found.
 */
export function loadProjectConfig(dir?: string): ProjectConfig | undefined {
  const configPath = join(dir ?? process.cwd(), ".wuphf.toml");
  if (!existsSync(configPath)) return undefined;

  try {
    const content = readFileSync(configPath, "utf-8");
    return parseToml(content) as ProjectConfig;
  } catch {
    return undefined;
  }
}

/**
 * Check if a specific hook is enabled in .wuphf.toml.
 * Returns true by default (hooks are opt-out, not opt-in).
 */
export function isHookEnabled(
  hookName: "recall" | "capture" | "session_start",
  dir?: string,
): boolean {
  const config = loadProjectConfig(dir);
  if (!config) return true;

  // Master kill switch
  if (config.hooks?.enabled === false) return false;

  // Per-hook setting
  const hookConfig = config.hooks?.[hookName];
  if (hookConfig?.enabled === false) return false;

  return true;
}

const DEFAULT_TOML = `# .wuphf.toml — WUPHF project configuration
# All settings are optional. Defaults shown in comments.

# [auth]
# api_key = "sk-..."          # Prefer WUPHF_API_KEY env var or ~/.wuphf/config.json

# [hooks]
# enabled = true              # Master kill switch for all hooks

# [hooks.recall]
# enabled = true              # Auto-recall context on each prompt
# debounce_ms = 30000

# [hooks.capture]
# enabled = true              # Auto-capture on conversation stop
# min_length = 20
# max_length = 50000

# [hooks.session_start]
# enabled = true              # Load context on session start

# [scan]
# enabled = true
# extensions = [".md", ".txt", ".csv", ".json", ".yaml", ".yml"]
# ignore_dirs = ["node_modules", ".git", "dist", "build", "__pycache__"]
# max_files = 5
# max_file_size = 100000
# depth = 2

# [mcp]
# enabled = true              # MCP installed by default; use --no-mcp to skip

# [output]
# format = "text"             # "text" | "json"
# timeout = 120000
`;

/**
 * Write a default .wuphf.toml to the given directory.
 * Returns false if the file already exists.
 */
export function writeDefaultProjectConfig(dir?: string): boolean {
  const configPath = join(dir ?? process.cwd(), ".wuphf.toml");
  if (existsSync(configPath)) return false;

  writeFileSync(configPath, DEFAULT_TOML, "utf-8");
  return true;
}
