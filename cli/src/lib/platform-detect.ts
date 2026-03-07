/**
 * Detect installed AI coding platforms and check Nex installation status.
 */

import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import { execFileSync } from "node:child_process";

export interface Platform {
  id: string;
  displayName: string;
  detected: boolean;
  nexInstalled: boolean;
  pluginSupport: boolean;
  configPath: string;
  configFormat: "standard" | "zed" | "continue";
}

const home = homedir();

function exists(path: string): boolean {
  return existsSync(path);
}

function whichExists(cmd: string): boolean {
  try {
    execFileSync("which", [cmd], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

function hasNexMcpEntry(configPath: string, key = "nex"): boolean {
  try {
    const raw = readFileSync(configPath, "utf-8");
    const config = JSON.parse(raw);
    // Check mcpServers.nex or context_servers.nex
    return !!(config?.mcpServers?.[key] || config?.context_servers?.[key]);
  } catch {
    return false;
  }
}

function hasClaudeCodePlugin(): boolean {
  const settingsPath = join(home, ".claude", "settings.json");
  try {
    const raw = readFileSync(settingsPath, "utf-8");
    return raw.includes("nex") && raw.includes("auto-recall");
  } catch {
    return false;
  }
}

function claudeDesktopConfigPath(): string {
  if (process.platform === "darwin") {
    return join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json");
  }
  if (process.platform === "win32") {
    return join(process.env.APPDATA ?? join(home, "AppData", "Roaming"), "Claude", "claude_desktop_config.json");
  }
  // Linux
  return join(home, ".config", "Claude", "claude_desktop_config.json");
}

function hasClineExtension(): boolean {
  const extensionsDir = join(home, ".vscode", "extensions");
  try {
    const entries = readdirSync(extensionsDir);
    return entries.some((e) => e.startsWith("saoudrizwan.claude-dev-"));
  } catch {
    return false;
  }
}

function clineConfigPath(): string {
  // Cline stores MCP config in VS Code globalStorage
  if (process.platform === "darwin") {
    return join(home, "Library", "Application Support", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json");
  }
  if (process.platform === "win32") {
    return join(process.env.APPDATA ?? join(home, "AppData", "Roaming"), "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json");
  }
  return join(home, ".config", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json");
}

export function detectPlatforms(): Platform[] {
  const cwd = process.cwd();

  const platforms: Platform[] = [
    {
      id: "claude-code",
      displayName: "Claude Code",
      detected: exists(join(home, ".claude")),
      nexInstalled: hasClaudeCodePlugin(),
      pluginSupport: true,
      configPath: join(home, ".claude", "settings.json"),
      configFormat: "standard",
    },
    {
      id: "claude-desktop",
      displayName: "Claude Desktop",
      detected: exists(claudeDesktopConfigPath()),
      nexInstalled: hasNexMcpEntry(claudeDesktopConfigPath()),
      pluginSupport: false,
      configPath: claudeDesktopConfigPath(),
      configFormat: "standard",
    },
    {
      id: "cursor",
      displayName: "Cursor",
      detected: exists(join(home, ".cursor")),
      nexInstalled: hasNexMcpEntry(join(home, ".cursor", "mcp.json")),
      pluginSupport: false,
      configPath: join(home, ".cursor", "mcp.json"),
      configFormat: "standard",
    },
    {
      id: "vscode",
      displayName: "VS Code",
      detected: whichExists("code") || exists(join(cwd, ".vscode")),
      nexInstalled: hasNexMcpEntry(join(cwd, ".vscode", "mcp.json")),
      pluginSupport: false,
      configPath: join(cwd, ".vscode", "mcp.json"),
      configFormat: "standard",
    },
    {
      id: "windsurf",
      displayName: "Windsurf",
      detected: exists(join(home, ".codeium", "windsurf")),
      nexInstalled: hasNexMcpEntry(join(home, ".codeium", "windsurf", "mcp_config.json")),
      pluginSupport: false,
      configPath: join(home, ".codeium", "windsurf", "mcp_config.json"),
      configFormat: "standard",
    },
    {
      id: "cline",
      displayName: "Cline",
      detected: hasClineExtension(),
      nexInstalled: hasNexMcpEntry(clineConfigPath()),
      pluginSupport: false,
      configPath: clineConfigPath(),
      configFormat: "standard",
    },
    {
      id: "zed",
      displayName: "Zed",
      detected: exists(join(home, ".config", "zed")),
      nexInstalled: hasNexMcpEntry(join(home, ".config", "zed", "settings.json"), "nex"),
      pluginSupport: false,
      configPath: join(home, ".config", "zed", "settings.json"),
      configFormat: "zed",
    },
    {
      id: "continue",
      displayName: "Continue.dev",
      detected: exists(join(cwd, ".continue")) || exists(join(home, ".continue")),
      nexInstalled: false, // YAML check would be complex — just report not installed
      pluginSupport: false,
      configPath: exists(join(cwd, ".continue"))
        ? join(cwd, ".continue", "config.yaml")
        : join(home, ".continue", "config.yaml"),
      configFormat: "continue",
    },
    {
      id: "kilocode",
      displayName: "Kilo Code",
      detected: exists(join(cwd, ".kilocode")),
      nexInstalled: hasNexMcpEntry(join(cwd, ".kilocode", "mcp.json")),
      pluginSupport: false,
      configPath: join(cwd, ".kilocode", "mcp.json"),
      configFormat: "standard",
    },
    {
      id: "opencode",
      displayName: "OpenCode",
      detected: exists(join(home, ".config", "opencode")),
      nexInstalled: hasNexMcpEntry(join(home, ".config", "opencode", "opencode.json")),
      pluginSupport: false,
      configPath: join(home, ".config", "opencode", "opencode.json"),
      configFormat: "standard",
    },
  ];

  return platforms;
}

export function getDetectedPlatforms(): Platform[] {
  return detectPlatforms().filter((p) => p.detected);
}

export function getPlatformById(id: string): Platform | undefined {
  return detectPlatforms().find((p) => p.id === id);
}

export const VALID_PLATFORM_IDS = [
  "claude-code",
  "claude-desktop",
  "cursor",
  "vscode",
  "windsurf",
  "cline",
  "continue",
  "zed",
  "kilocode",
  "opencode",
];
