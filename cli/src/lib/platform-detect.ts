/**
 * Detect installed AI coding platforms and check WUPHF installation status.
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
  supportsRules: boolean;
  supportsHooks: boolean;
  supportsCustomTools: boolean;
  supportsCustomAgents: boolean;
  supportsWorkflows: boolean;
  hookConfigPath: string | null;
  rulesPath: string | null;
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

function hasNexMcpEntry(configPath: string, key = "wuphf"): boolean {
  try {
    const raw = readFileSync(configPath, "utf-8");
    const config = JSON.parse(raw);
    // Check mcpServers.wuphf or context_servers.wuphf
    return !!(config?.mcpServers?.[key] || config?.context_servers?.[key]);
  } catch {
    return false;
  }
}

function hasNexRules(rulesPath: string | null): boolean {
  if (!rulesPath) return false;
  try {
    const content = readFileSync(rulesPath, "utf-8");
    return content.includes("WUPHF") && (content.includes("nex_ask") || content.includes("WUPHF Context"));
  } catch {
    return false;
  }
}

function hasClaudeCodePlugin(): boolean {
  const settingsPath = join(home, ".claude", "settings.json");
  try {
    const raw = readFileSync(settingsPath, "utf-8");
    return raw.includes("wuphf") && raw.includes("auto-recall");
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

function openclawConfigPath(): string {
  return join(home, ".openclaw", "openclaw.json");
}

function hasOpenClawNexPlugin(): boolean {
  try {
    const raw = readFileSync(openclawConfigPath(), "utf-8");
    const config = JSON.parse(raw);
    return !!(config?.plugins?.entries?.wuphf);
  } catch {
    return false;
  }
}

export function detectPlatforms(): Platform[] {
  const cwd = process.cwd();

  // Rules paths (project-local)
  const cursorRulesPath = join(cwd, ".cursor", "rules", "wuphf.md");
  const vscodeRulesPath = join(cwd, ".github", "instructions", "wuphf.instructions.md");
  const windsurfRulesPath = join(cwd, ".windsurf", "rules", "wuphf.md");
  const clineRulesPath = join(cwd, ".clinerules", "wuphf.md");
  const continueBase = exists(join(cwd, ".continue")) ? join(cwd, ".continue") : join(home, ".continue");
  const continueRulesPath = join(continueBase, "rules", "wuphf.md");
  const zedRulesPath = join(cwd, ".rules");
  const kilocodeRulesPath = join(cwd, ".kilocode", "rules", "wuphf.md");
  const opencodeRulesPath = join(cwd, "AGENTS.md");

  // Hook config paths
  const cursorHookConfigPath = join(home, ".cursor", "hooks.json");
  const windsurfHookConfigPath = join(cwd, ".windsurf", "hooks.json");
  const clineHookDir = join(cwd, ".clinerules", "hooks");

  const platforms: Platform[] = [
    {
      id: "claude-code",
      displayName: "Claude Code",
      detected: exists(join(home, ".claude")),
      nexInstalled: hasClaudeCodePlugin(),
      pluginSupport: true,
      supportsRules: false,
      supportsHooks: true,
      supportsCustomTools: false,
      supportsCustomAgents: false,
      supportsWorkflows: true, // slash commands
      hookConfigPath: join(home, ".claude", "settings.json"),
      rulesPath: null,
      configPath: join(home, ".claude", "settings.json"),
      configFormat: "standard",
    },
    {
      id: "claude-desktop",
      displayName: "Claude Desktop",
      detected: exists(claudeDesktopConfigPath()),
      nexInstalled: hasNexMcpEntry(claudeDesktopConfigPath()),
      pluginSupport: false,
      supportsRules: false,
      supportsHooks: false,
      supportsCustomTools: false,
      supportsCustomAgents: false,
      supportsWorkflows: false,
      hookConfigPath: null,
      rulesPath: null,
      configPath: claudeDesktopConfigPath(),
      configFormat: "standard",
    },
    {
      id: "cursor",
      displayName: "Cursor",
      detected: exists(join(home, ".cursor")),
      nexInstalled: hasNexMcpEntry(join(home, ".cursor", "mcp.json")) || hasNexRules(cursorRulesPath),
      pluginSupport: false,
      supportsRules: true,
      supportsHooks: true,
      supportsCustomTools: false,
      supportsCustomAgents: false,
      supportsWorkflows: false,
      hookConfigPath: cursorHookConfigPath,
      rulesPath: cursorRulesPath,
      configPath: join(home, ".cursor", "mcp.json"),
      configFormat: "standard",
    },
    {
      id: "vscode",
      displayName: "VS Code",
      detected: whichExists("code") || exists(join(cwd, ".vscode")),
      nexInstalled: hasNexMcpEntry(join(cwd, ".vscode", "mcp.json")) || hasNexRules(vscodeRulesPath),
      pluginSupport: false,
      supportsRules: true,
      supportsHooks: false,
      supportsCustomTools: false,
      supportsCustomAgents: true,
      supportsWorkflows: false,
      hookConfigPath: null,
      rulesPath: vscodeRulesPath,
      configPath: join(cwd, ".vscode", "mcp.json"),
      configFormat: "standard",
    },
    {
      id: "windsurf",
      displayName: "Windsurf",
      detected: exists(join(home, ".codeium", "windsurf")),
      nexInstalled: hasNexMcpEntry(join(home, ".codeium", "windsurf", "mcp_config.json")) || hasNexRules(windsurfRulesPath),
      pluginSupport: false,
      supportsRules: true,
      supportsHooks: true,
      supportsCustomTools: false,
      supportsCustomAgents: false,
      supportsWorkflows: true,
      hookConfigPath: windsurfHookConfigPath,
      rulesPath: windsurfRulesPath,
      configPath: join(home, ".codeium", "windsurf", "mcp_config.json"),
      configFormat: "standard",
    },
    {
      id: "cline",
      displayName: "Cline",
      detected: hasClineExtension(),
      nexInstalled: hasNexMcpEntry(clineConfigPath()) || hasNexRules(clineRulesPath),
      pluginSupport: false,
      supportsRules: true,
      supportsHooks: true,
      supportsCustomTools: false,
      supportsCustomAgents: false,
      supportsWorkflows: false,
      hookConfigPath: clineHookDir,
      rulesPath: clineRulesPath,
      configPath: clineConfigPath(),
      configFormat: "standard",
    },
    {
      id: "zed",
      displayName: "Zed",
      detected: exists(join(home, ".config", "zed")),
      nexInstalled: hasNexMcpEntry(join(home, ".config", "zed", "settings.json"), "wuphf") || hasNexRules(zedRulesPath),
      pluginSupport: false,
      supportsRules: true,
      supportsHooks: false,
      supportsCustomTools: false,
      supportsCustomAgents: false,
      supportsWorkflows: false,
      hookConfigPath: null,
      rulesPath: zedRulesPath,
      configPath: join(home, ".config", "zed", "settings.json"),
      configFormat: "zed",
    },
    {
      id: "continue",
      displayName: "Continue.dev",
      detected: exists(join(cwd, ".continue")) || exists(join(home, ".continue")),
      nexInstalled: hasNexRules(continueRulesPath),
      pluginSupport: false,
      supportsRules: true,
      supportsHooks: false,
      supportsCustomTools: false,
      supportsCustomAgents: false,
      supportsWorkflows: false,
      hookConfigPath: null,
      rulesPath: continueRulesPath,
      configPath: exists(join(cwd, ".continue"))
        ? join(cwd, ".continue", "config.yaml")
        : join(home, ".continue", "config.yaml"),
      configFormat: "continue",
    },
    {
      id: "kilocode",
      displayName: "Kilo Code",
      detected: exists(join(cwd, ".kilocode")),
      nexInstalled: hasNexMcpEntry(join(cwd, ".kilocode", "mcp.json")) || hasNexRules(kilocodeRulesPath),
      pluginSupport: false,
      supportsRules: true,
      supportsHooks: false,
      supportsCustomTools: false,
      supportsCustomAgents: true,
      supportsWorkflows: false,
      hookConfigPath: null,
      rulesPath: kilocodeRulesPath,
      configPath: join(cwd, ".kilocode", "mcp.json"),
      configFormat: "standard",
    },
    {
      id: "opencode",
      displayName: "OpenCode",
      detected: exists(join(home, ".config", "opencode")),
      nexInstalled: hasNexMcpEntry(join(home, ".config", "opencode", "opencode.json")) || hasNexRules(opencodeRulesPath),
      pluginSupport: false,
      supportsRules: true,
      supportsHooks: false,
      supportsCustomTools: true,
      supportsCustomAgents: false,
      supportsWorkflows: false,
      hookConfigPath: null,
      rulesPath: opencodeRulesPath,
      configPath: join(home, ".config", "opencode", "opencode.json"),
      configFormat: "standard",
    },
    {
      id: "openclaw",
      displayName: "OpenClaw",
      detected: whichExists("openclaw") || exists(join(home, ".openclaw")),
      nexInstalled: hasOpenClawNexPlugin(),
      pluginSupport: true,
      supportsRules: false,
      supportsHooks: false, // Hooks handled by the plugin internally
      supportsCustomTools: false,
      supportsCustomAgents: false,
      supportsWorkflows: false,
      hookConfigPath: null,
      rulesPath: null,
      configPath: openclawConfigPath(),
      configFormat: "standard",
    },
    {
      id: "aider",
      displayName: "Aider",
      detected: whichExists("aider") || exists(join(cwd, "CONVENTIONS.md")),
      nexInstalled: hasNexRules(join(cwd, "CONVENTIONS.md")),
      pluginSupport: false,
      supportsRules: true,
      supportsHooks: false,
      supportsCustomTools: false,
      supportsCustomAgents: false,
      supportsWorkflows: false,
      hookConfigPath: null,
      rulesPath: join(cwd, "CONVENTIONS.md"),
      configPath: "", // Aider has no MCP support
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
  "openclaw",
  "aider",
];
