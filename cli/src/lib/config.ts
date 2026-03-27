/**
 * Configuration resolution: CLI flags > env vars > config file.
 * Base URL defaults to the Nex backend, with WUPHF_* env aliases first and legacy NEX_* fallbacks.
 */

import { existsSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, dirname } from "node:path";
import { homedir } from "node:os";

const NEW_CONFIG_PATH = join(homedir(), ".wuphf", "config.json");
const LEGACY_CONFIG_PATH = join(homedir(), ".nex", "config.json");

export const CONFIG_PATH = NEW_CONFIG_PATH;
export const BASE_URL = process.env.WUPHF_DEV_URL ?? process.env.NEX_DEV_URL ?? loadConfig().dev_url ?? "https://app.nex.ai";
export const API_BASE = `${BASE_URL}/api/developers`;
export const REGISTER_URL = `${BASE_URL}/api/v1/agents/register`;

export interface NexConfig {
  api_key?: string;
  email?: string;
  workspace_id?: string;
  workspace_slug?: string;
  default_format?: string;
  default_timeout?: number;
  [key: string]: unknown;
}

export function loadConfig(): NexConfig {
  try {
    const path = existsSync(NEW_CONFIG_PATH) ? NEW_CONFIG_PATH : LEGACY_CONFIG_PATH;
    const raw = readFileSync(path, "utf-8");
    return JSON.parse(raw) as NexConfig;
  } catch {
    return {};
  }
}

export function saveConfig(config: NexConfig): void {
  mkdirSync(dirname(NEW_CONFIG_PATH), { recursive: true });
  writeFileSync(NEW_CONFIG_PATH, JSON.stringify(config, null, 2) + "\n", "utf-8");
}

/**
 * Resolve API key from: flag > env > config file.
 */
export function resolveApiKey(flagValue?: string): string | undefined {
  return flagValue || process.env.WUPHF_API_KEY || process.env.NEX_API_KEY || loadConfig().api_key || undefined;
}

/**
 * Resolve output format from: flag > config file > default.
 */
export function resolveFormat(flagValue?: string): string {
  if (flagValue) return flagValue;
  const configured = loadConfig().default_format;
  if (configured) return configured;
  // Default to "text" for TTY (rich TUI output), "json" for piped/scripted usage
  return process.stdout.isTTY ? "text" : "json";
}

/**
 * Resolve timeout from: flag > config file > default.
 */
export function resolveTimeout(flagValue?: string): number {
  if (flagValue) return parseInt(flagValue, 10);
  return loadConfig().default_timeout ?? 120_000;
}

/**
 * Persist registration data to config file.
 */
export function persistRegistration(data: Record<string, unknown>): void {
  const existing = loadConfig();
  if (typeof data.api_key === "string") existing.api_key = data.api_key;
  if (typeof data.email === "string") existing.email = data.email;
  if (typeof data.workspace_id === "string" || typeof data.workspace_id === "number") {
    existing.workspace_id = String(data.workspace_id);
  }
  if (typeof data.workspace_slug === "string") existing.workspace_slug = data.workspace_slug;
  saveConfig(existing);
}
