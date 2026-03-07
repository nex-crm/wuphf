/**
 * Configuration resolution: CLI flags > env vars > config file.
 * Base URL is hardcoded to production (NEX_DEV_URL escape hatch for local dev).
 */

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, dirname } from "node:path";
import { homedir } from "node:os";

export const CONFIG_PATH = join(homedir(), ".nex", "config.json");
export const BASE_URL = process.env.NEX_DEV_URL ?? "https://app.nex.ai";
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
    const raw = readFileSync(CONFIG_PATH, "utf-8");
    return JSON.parse(raw) as NexConfig;
  } catch {
    return {};
  }
}

export function saveConfig(config: NexConfig): void {
  mkdirSync(dirname(CONFIG_PATH), { recursive: true });
  writeFileSync(CONFIG_PATH, JSON.stringify(config, null, 2) + "\n", "utf-8");
}

/**
 * Resolve API key from: flag > env > config file.
 */
export function resolveApiKey(flagValue?: string): string | undefined {
  return flagValue || process.env.NEX_API_KEY || loadConfig().api_key || undefined;
}

/**
 * Resolve output format from: flag > config file > default.
 */
export function resolveFormat(flagValue?: string): string {
  return flagValue || loadConfig().default_format || "json";
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
