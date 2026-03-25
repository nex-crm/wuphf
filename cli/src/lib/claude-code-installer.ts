/**
 * Claude Code plugin installer.
 * Merges WUPHF hooks into Claude Code's ~/.claude/settings.json.
 */

import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { resolve, dirname, join } from "node:path";
import { homedir } from "node:os";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

export function installClaudeCodePlugin(): { success: boolean; message: string } {
  // Resolve plugin directory: src/lib/ or dist/lib/ → up 3 levels → claude-code-plugin/
  const pluginDir = resolve(__dirname, "..", "..", "..", "claude-code-plugin");
  const pluginSettingsPath = join(pluginDir, "settings.json");
  const claudeSettingsPath = join(homedir(), ".claude", "settings.json");

  // 1. Check plugin directory exists
  if (!existsSync(pluginDir) || !existsSync(pluginSettingsPath)) {
    return {
      success: false,
      message: `Claude Code plugin not found at ${pluginDir}. Ensure the wuphf repo is intact.`,
    };
  }

  // 2. Read plugin settings.json
  let pluginSettingsRaw: string;
  try {
    pluginSettingsRaw = readFileSync(pluginSettingsPath, "utf-8");
  } catch (err) {
    return {
      success: false,
      message: `Failed to read plugin settings: ${err instanceof Error ? err.message : String(err)}`,
    };
  }

  // 3. Resolve <path-to> placeholders with actual plugin path
  const resolvedRaw = pluginSettingsRaw.replace(/<path-to>/g, pluginDir);
  let pluginSettings: Record<string, unknown>;
  try {
    pluginSettings = JSON.parse(resolvedRaw);
  } catch {
    return {
      success: false,
      message: "Failed to parse plugin settings.json.",
    };
  }

  // 4. Read existing Claude settings (or start fresh)
  let claudeSettings: Record<string, unknown> = {};
  try {
    claudeSettings = JSON.parse(readFileSync(claudeSettingsPath, "utf-8"));
  } catch {
    // File doesn't exist yet
  }

  // 5. Merge hooks from plugin into Claude settings
  const pluginHooks = pluginSettings.hooks as Record<string, unknown> | undefined;
  if (pluginHooks) {
    const existingHooks = (claudeSettings.hooks as Record<string, unknown>) ?? {};
    claudeSettings.hooks = { ...existingHooks, ...pluginHooks };
  }

  // 6. Write back
  try {
    mkdirSync(dirname(claudeSettingsPath), { recursive: true });
    writeFileSync(claudeSettingsPath, JSON.stringify(claudeSettings, null, 2) + "\n", "utf-8");
  } catch (err) {
    return {
      success: false,
      message: `Failed to write Claude settings: ${err instanceof Error ? err.message : String(err)}`,
    };
  }

  return {
    success: true,
    message: "WUPHF plugin installed into Claude Code. Open a new Claude Code session — WUPHF context will load automatically.",
  };
}
