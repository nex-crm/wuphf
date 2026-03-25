/**
 * Platform-specific installers for WUPHF MCP server, hooks, plugins, agents, and workflows.
 *
 * Installation hierarchy (per platform):
 *   1. Hooks (event-driven scripts)
 *   2. Custom tools/plugins (OpenCode plugin, OpenClaw plugin)
 *   3. Custom agents/modes (VS Code agent, Kilo Code mode)
 *   4. Workflows/slash commands (Windsurf workflows)
 *   5. Rules (instruction files)
 *   6. MCP (tool protocol)
 */

import { readFileSync, writeFileSync, mkdirSync, existsSync, readdirSync, unlinkSync, copyFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { homedir } from "node:os";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";
import type { Platform } from "./platform-detect.js";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const MCP_SERVER_ENTRY = {
  command: "npx",
  args: ["-y", "@wuphf-crm/mcp-server"],
  env: {} as Record<string, string>,
};

// ── Shared helpers ──────────────────────────────────────────────────────

function readJsonFile(path: string): Record<string, unknown> {
  try {
    const raw = readFileSync(path, "utf-8");
    return JSON.parse(raw) as Record<string, unknown>;
  } catch {
    return {};
  }
}

function writeJsonFile(path: string, data: Record<string, unknown>): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify(data, null, 2) + "\n", "utf-8");
}

/**
 * Resolve bundled plugin paths.
 * From dist/lib/installers.js:
 *   __dirname = <pkg>/dist/lib/
 *   dist/plugin/ = __dirname/../plugin/
 *   plugin-commands/ = __dirname/../../plugin-commands/
 *   platform-plugins/ = __dirname/../../platform-plugins/
 *   platform-rules/ = __dirname/../../platform-rules/
 */
function getPluginDistDir(): string {
  return join(__dirname, "..", "plugin");
}

function getPluginCommandsDir(): string {
  return join(__dirname, "..", "..", "plugin-commands");
}

function getPluginRulesDir(): string {
  return join(__dirname, "..", "..", "platform-rules");
}

function getPlatformPluginsDir(): string {
  return join(__dirname, "..", "..", "platform-plugins");
}

// ── 1. Generic MCP Installer ───────────────────────────────────────────

export function installMcpServer(
  platform: Platform,
  apiKey: string,
): { installed: boolean; configPath: string } {
  if (!platform.configPath) {
    return { installed: false, configPath: "" };
  }

  const entry = {
    ...MCP_SERVER_ENTRY,
    env: { WUPHF_API_KEY: apiKey },
  };

  if (platform.configFormat === "zed") {
    return installZedMcp(platform.configPath, entry);
  }

  if (platform.configFormat === "continue") {
    return installContinueMcp(platform.configPath, apiKey);
  }

  // Standard JSON format (Cursor, Claude Desktop, VS Code, Windsurf, Cline, Kilo Code, OpenCode)
  const config = readJsonFile(platform.configPath);

  if (!config.mcpServers || typeof config.mcpServers !== "object") {
    config.mcpServers = {};
  }
  (config.mcpServers as Record<string, unknown>).wuphf = entry;

  writeJsonFile(platform.configPath, config);
  return { installed: true, configPath: platform.configPath };
}

function installZedMcp(
  configPath: string,
  entry: typeof MCP_SERVER_ENTRY,
): { installed: boolean; configPath: string } {
  const config = readJsonFile(configPath);

  if (!config.context_servers || typeof config.context_servers !== "object") {
    config.context_servers = {};
  }
  (config.context_servers as Record<string, unknown>).wuphf = {
    command: { path: entry.command, args: entry.args, env: entry.env },
  };

  writeJsonFile(configPath, config);
  return { installed: true, configPath };
}

function installContinueMcp(
  configPath: string,
  apiKey: string,
): { installed: boolean; configPath: string } {
  const mcpPath = configPath.replace("config.yaml", "mcp.json");
  const config = readJsonFile(mcpPath);

  if (!config.mcpServers || typeof config.mcpServers !== "object") {
    config.mcpServers = {};
  }
  (config.mcpServers as Record<string, unknown>).wuphf = {
    ...MCP_SERVER_ENTRY,
    env: { WUPHF_API_KEY: apiKey },
  };

  writeJsonFile(mcpPath, config);
  return { installed: true, configPath: mcpPath };
}

// ── 2. Claude Code Plugin Installer ────────────────────────────────────

interface HookEntry {
  type: string;
  command: string;
  timeout: number;
  statusMessage?: string;
  async?: boolean;
}

interface HookGroup {
  matcher: string;
  hooks: HookEntry[];
}

interface SettingsJson {
  hooks?: Record<string, HookGroup[]>;
  [key: string]: unknown;
}

export function installClaudeCodePlugin(): {
  installed: boolean;
  hooksAdded: string[];
  commandsCopied: string[];
} {
  const home = homedir();
  const claudeDir = join(home, ".claude");
  const settingsPath = join(claudeDir, "settings.json");

  const distDir = getPluginDistDir();

  // Verify bundled plugin exists
  if (!existsSync(join(distDir, "auto-recall.js"))) {
    return { installed: false, hooksAdded: [], commandsCopied: [] };
  }

  // 1. Read existing settings.json
  let settings: SettingsJson = {};
  try {
    const raw = readFileSync(settingsPath, "utf-8");
    settings = JSON.parse(raw) as SettingsJson;
  } catch {
    // Start fresh
  }

  if (!settings.hooks) {
    settings.hooks = {};
  }

  const hooksAdded: string[] = [];

  // 2. Add hooks (idempotent — remove stale wuphf hooks, then add fresh ones)
  const hookDefs: Array<{
    event: string;
    script: string;
    timeout: number;
    statusMessage?: string;
    async?: boolean;
  }> = [
    {
      event: "SessionStart",
      script: join(distDir, "auto-session-start.js"),
      timeout: 120000,
      statusMessage: "Loading knowledge context...",
    },
    {
      event: "UserPromptSubmit",
      script: join(distDir, "auto-recall.js"),
      timeout: 10000,
      statusMessage: "Recalling relevant memories...",
    },
    {
      event: "Stop",
      script: join(distDir, "auto-capture.js"),
      timeout: 10000,
      async: true,
    },
  ];

  for (const def of hookDefs) {
    if (!settings.hooks![def.event]) {
      settings.hooks![def.event] = [];
    }

    const groups = settings.hooks![def.event];

    // Remove any existing wuphf hook entries (handles path updates on upgrade)
    const filtered = groups.filter((g) =>
      !g.hooks.some((h) => h.command.includes("auto-recall") || h.command.includes("auto-capture") || h.command.includes("auto-session-start"))
    );

    const hookEntry: HookEntry = {
      type: "command",
      command: `node ${def.script}`,
      timeout: def.timeout,
    };
    if (def.statusMessage) hookEntry.statusMessage = def.statusMessage;
    if (def.async) hookEntry.async = true;

    filtered.push({ matcher: "", hooks: [hookEntry] });
    settings.hooks![def.event] = filtered;
    hooksAdded.push(def.event);
  }

  // 3. Write settings.json
  mkdirSync(claudeDir, { recursive: true });
  writeFileSync(settingsPath, JSON.stringify(settings, null, 2) + "\n", "utf-8");

  // 4. Copy slash commands (copy, not symlink — survives npm updates)
  const commandsCopied: string[] = [];
  const commandsDir = join(claudeDir, "commands");
  const sourceCommandsDir = getPluginCommandsDir();

  if (existsSync(sourceCommandsDir)) {
    mkdirSync(commandsDir, { recursive: true });

    try {
      const entries = readdirSync(sourceCommandsDir);
      for (const entry of entries) {
        if (!entry.endsWith(".md")) continue;
        const target = join(commandsDir, entry);
        const source = join(sourceCommandsDir, entry);

        try { unlinkSync(target); } catch { /* File didn't exist */ }

        try {
          copyFileSync(source, target);
          commandsCopied.push(entry);
        } catch { /* Copy failed — non-critical */ }
      }
    } catch { /* Commands dir read failed — non-critical */ }
  }

  return { installed: true, hooksAdded, commandsCopied };
}

// ── 3. Hook Installers (Cursor, Windsurf, Cline) ──────────────────────

/**
 * Install hook scripts for platforms that support event-driven hooks.
 * Each platform has its own adapter scripts in dist/plugin/adapters/.
 */
export function installHooks(
  platform: Platform,
): { installed: boolean; hooksAdded: string[] } {
  // Claude Code handled separately via installClaudeCodePlugin
  if (platform.id === "claude-code") {
    return { installed: false, hooksAdded: [] };
  }

  const adapterDir = join(getPluginDistDir(), "adapters");
  if (!existsSync(adapterDir)) {
    return { installed: false, hooksAdded: [] };
  }

  if (platform.id === "cursor") return installCursorHooks(adapterDir, platform);
  if (platform.id === "windsurf") return installWindsurfHooks(adapterDir, platform);
  if (platform.id === "cline") return installClineHooks(adapterDir, platform);

  return { installed: false, hooksAdded: [] };
}

function installCursorHooks(
  adapterDir: string,
  platform: Platform,
): { installed: boolean; hooksAdded: string[] } {
  const hookConfigPath = platform.hookConfigPath;
  if (!hookConfigPath) return { installed: false, hooksAdded: [] };

  const config = readJsonFile(hookConfigPath);
  if (!config.hooks || typeof config.hooks !== "object") {
    config.hooks = {};
  }

  const hooks = config.hooks as Record<string, unknown[]>;
  const hooksAdded: string[] = [];

  const cursorHookDefs = [
    { event: "sessionStart", script: "cursor-session-start.js", timeout: 120000 },
    { event: "userPromptSubmit", script: "cursor-recall.js", timeout: 10000 },
    { event: "stop", script: "cursor-stop.js", timeout: 10000 },
  ];

  for (const def of cursorHookDefs) {
    const scriptPath = join(adapterDir, def.script);
    if (!existsSync(scriptPath)) continue;

    if (!hooks[def.event]) hooks[def.event] = [];

    // Remove existing wuphf hooks
    hooks[def.event] = (hooks[def.event] as Array<Record<string, unknown>>).filter(
      (h) => !String(h.command ?? "").includes("wuphf")
    );

    (hooks[def.event] as unknown[]).push({
      type: "command",
      command: `node ${scriptPath}`,
      timeout: def.timeout,
    });
    hooksAdded.push(def.event);
  }

  writeJsonFile(hookConfigPath, config);
  return { installed: true, hooksAdded };
}

function installWindsurfHooks(
  adapterDir: string,
  platform: Platform,
): { installed: boolean; hooksAdded: string[] } {
  const hookConfigPath = platform.hookConfigPath;
  if (!hookConfigPath) return { installed: false, hooksAdded: [] };

  const config = readJsonFile(hookConfigPath);
  if (!config.hooks || typeof config.hooks !== "object") {
    config.hooks = {};
  }

  const hooks = config.hooks as Record<string, unknown[]>;
  const hooksAdded: string[] = [];

  const windsurfHookDefs = [
    { event: "pre_user_prompt", script: "windsurf-recall.js", timeout: 10000 },
    { event: "post_cascade_response", script: "windsurf-capture.js", timeout: 10000 },
  ];

  for (const def of windsurfHookDefs) {
    const scriptPath = join(adapterDir, def.script);
    if (!existsSync(scriptPath)) continue;

    if (!hooks[def.event]) hooks[def.event] = [];

    hooks[def.event] = (hooks[def.event] as Array<Record<string, unknown>>).filter(
      (h) => !String(h.command ?? "").includes("wuphf")
    );

    (hooks[def.event] as unknown[]).push({
      type: "command",
      command: `node ${scriptPath}`,
      timeout: def.timeout,
    });
    hooksAdded.push(def.event);
  }

  writeJsonFile(hookConfigPath, config);
  return { installed: true, hooksAdded };
}

function installClineHooks(
  adapterDir: string,
  platform: Platform,
): { installed: boolean; hooksAdded: string[] } {
  // Cline uses executable files in .clinerules/hooks/
  const hookDir = platform.hookConfigPath;
  if (!hookDir) return { installed: false, hooksAdded: [] };

  mkdirSync(hookDir, { recursive: true });
  const hooksAdded: string[] = [];

  const clineHookDefs = [
    { event: "UserPromptSubmit", script: "cline-recall.js" },
    { event: "TaskStart", script: "cline-task-start.js" },
    { event: "TaskComplete", script: "cline-capture.js" },
  ];

  for (const def of clineHookDefs) {
    const scriptPath = join(adapterDir, def.script);
    if (!existsSync(scriptPath)) continue;

    // Write a shell wrapper that invokes node with the adapter script
    const wrapperPath = join(hookDir, `wuphf-${def.event.toLowerCase()}`);
    const wrapper = `#!/usr/bin/env sh\nexec node "${scriptPath}" "$@"\n`;
    writeFileSync(wrapperPath, wrapper, { mode: 0o755 });
    hooksAdded.push(def.event);
  }

  return { installed: true, hooksAdded };
}

// ── 4. Custom Tool/Plugin Installers ───────────────────────────────────

/**
 * Install OpenCode plugin template to .opencode/plugins/wuphf.ts.
 */
export function installOpenCodePlugin(): {
  installed: boolean;
  pluginPath: string;
} {
  const templatePath = join(getPlatformPluginsDir(), "opencode-plugin.ts");
  if (!existsSync(templatePath)) {
    return { installed: false, pluginPath: "" };
  }

  const targetDir = join(process.cwd(), ".opencode", "plugins");
  const targetPath = join(targetDir, "wuphf.ts");

  mkdirSync(targetDir, { recursive: true });
  copyFileSync(templatePath, targetPath);

  return { installed: true, pluginPath: targetPath };
}

/**
 * Install OpenClaw plugin via CLI.
 */
export function installOpenClawPlugin(
  apiKey: string,
): { installed: boolean; message: string } {
  // Check if openclaw CLI is available
  let hasOpenClaw = false;
  try {
    execFileSync("which", ["openclaw"], { stdio: "ignore" });
    hasOpenClaw = true;
  } catch { /* not installed */ }

  if (!hasOpenClaw) {
    return {
      installed: false,
      message: "Install OpenClaw to enable: https://docs.openclaw.ai/install",
    };
  }

  // Check if plugin already installed
  const configPath = join(homedir(), ".openclaw", "openclaw.json");
  const config = readJsonFile(configPath);
  const plugins = (config.plugins ?? {}) as Record<string, unknown>;
  const entries = (plugins.entries ?? {}) as Record<string, Record<string, unknown>>;

  if (!entries.wuphf) {
    // Install the plugin
    try {
      execFileSync("openclaw", ["plugins", "install", "@wuphf-ai/openclaw-plugin"], {
        stdio: "ignore",
        timeout: 30_000,
      });
    } catch {
      return { installed: false, message: "Failed to install OpenClaw plugin" };
    }
  }

  // Configure API key
  const freshConfig = readJsonFile(configPath);
  const freshPlugins = (freshConfig.plugins ?? {}) as Record<string, unknown>;
  const freshEntries = (freshPlugins.entries ?? {}) as Record<string, Record<string, unknown>>;

  if (!freshEntries.wuphf) freshEntries.wuphf = {};
  if (!freshEntries.wuphf.config) freshEntries.wuphf.config = {};
  (freshEntries.wuphf.config as Record<string, unknown>).apiKey = apiKey;
  freshEntries.wuphf.enabled = true;

  freshPlugins.entries = freshEntries;
  freshConfig.plugins = freshPlugins;

  writeJsonFile(configPath, freshConfig);
  return { installed: true, message: "OpenClaw plugin installed and configured" };
}

// ── 5. Custom Agent/Mode Installers ────────────────────────────────────

/**
 * Install VS Code custom agent (.github/agents/wuphf.agent.md).
 */
export function installVSCodeAgent(): {
  installed: boolean;
  agentPath: string;
} {
  const templatePath = join(getPlatformPluginsDir(), "vscode-agent.md");
  if (!existsSync(templatePath)) {
    return { installed: false, agentPath: "" };
  }

  const targetDir = join(process.cwd(), ".github", "agents");
  const targetPath = join(targetDir, "wuphf.agent.md");

  mkdirSync(targetDir, { recursive: true });
  copyFileSync(templatePath, targetPath);

  return { installed: true, agentPath: targetPath };
}

/**
 * Install Kilo Code custom mode (.kilocodemodes YAML).
 */
export function installKiloCodeMode(): {
  installed: boolean;
  modePath: string;
} {
  const templatePath = join(getPlatformPluginsDir(), "kilocode-modes.yaml");
  if (!existsSync(templatePath)) {
    return { installed: false, modePath: "" };
  }

  const targetPath = join(process.cwd(), ".kilocodemodes");

  // Read existing modes file if present
  let existing = "";
  try {
    existing = readFileSync(targetPath, "utf-8");
  } catch { /* doesn't exist */ }

  // If already has wuphf-crm mode, skip
  if (existing.includes("wuphf-crm")) {
    return { installed: true, modePath: targetPath };
  }

  const template = readFileSync(templatePath, "utf-8");

  if (existing) {
    // Append to existing customModes
    // Remove the "customModes:" header from template since it already exists
    const modesContent = template.replace(/^customModes:\s*\n/, "");
    writeFileSync(targetPath, existing.trimEnd() + "\n" + modesContent, "utf-8");
  } else {
    writeFileSync(targetPath, template, "utf-8");
  }

  return { installed: true, modePath: targetPath };
}

/**
 * Install Continue.dev context provider template.
 */
export function installContinueProvider(): {
  installed: boolean;
  providerPath: string;
} {
  const templatePath = join(getPlatformPluginsDir(), "continue-provider.ts");
  if (!existsSync(templatePath)) {
    return { installed: false, providerPath: "" };
  }

  const continueBase = existsSync(join(process.cwd(), ".continue"))
    ? join(process.cwd(), ".continue")
    : join(homedir(), ".continue");

  const targetDir = join(continueBase, ".plugins");
  const targetPath = join(targetDir, "wuphf-provider.ts");

  mkdirSync(targetDir, { recursive: true });
  copyFileSync(templatePath, targetPath);

  return { installed: true, providerPath: targetPath };
}

// ── 6. Workflow Installers ─────────────────────────────────────────────

/**
 * Install Windsurf workflow files (slash commands).
 */
export function installWindsurfWorkflows(): {
  installed: boolean;
  workflowCount: number;
} {
  const sourceDir = join(getPlatformPluginsDir(), "windsurf-workflows");
  if (!existsSync(sourceDir)) {
    return { installed: false, workflowCount: 0 };
  }

  const targetDir = join(process.cwd(), ".windsurf", "workflows");
  mkdirSync(targetDir, { recursive: true });

  let count = 0;
  try {
    const entries = readdirSync(sourceDir);
    for (const entry of entries) {
      if (!entry.endsWith(".md")) continue;
      copyFileSync(join(sourceDir, entry), join(targetDir, entry));
      count++;
    }
  } catch { /* non-critical */ }

  return { installed: count > 0, workflowCount: count };
}

// ── 7. Rules File Installer ────────────────────────────────────────────

const NEX_RULES_MARKER_START = "# --- WUPHF Context & Memory ---";
const NEX_RULES_MARKER_END = "# --- End WUPHF ---";

/**
 * Map platform ID to its rules template filename.
 */
const RULES_TEMPLATE_MAP: Record<string, string> = {
  cursor: "cursor-rules.md",
  vscode: "vscode-instructions.md",
  windsurf: "windsurf-rules.md",
  cline: "cline-rules.md",
  continue: "continue-rules.md",
  zed: "zed-rules.md",
  kilocode: "kilocode-rules.md",
  opencode: "opencode-agents.md",
  aider: "aider-conventions.md",
};

/**
 * Platforms where rules are APPENDED to an existing file (with markers)
 * rather than written as a standalone file.
 */
const APPEND_PLATFORMS = new Set(["zed", "opencode", "aider"]);

export function installRulesFile(
  platform: Platform,
): { installed: boolean; rulesPath: string } {
  const rulesPath = platform.rulesPath;
  if (!rulesPath) {
    return { installed: false, rulesPath: "" };
  }

  const templateName = RULES_TEMPLATE_MAP[platform.id];
  if (!templateName) {
    return { installed: false, rulesPath: "" };
  }

  const templatePath = join(getPluginRulesDir(), templateName);
  if (!existsSync(templatePath)) {
    return { installed: false, rulesPath: "" };
  }

  const template = readFileSync(templatePath, "utf-8");

  if (APPEND_PLATFORMS.has(platform.id)) {
    // Append mode: add/replace section in existing file
    let existing = "";
    try {
      existing = readFileSync(rulesPath, "utf-8");
    } catch {
      // File doesn't exist — will create
    }

    // Check if wuphf section already exists
    const startIdx = existing.indexOf(NEX_RULES_MARKER_START);
    const endIdx = existing.indexOf(NEX_RULES_MARKER_END);

    if (startIdx !== -1 && endIdx !== -1) {
      // Replace existing section
      const before = existing.slice(0, startIdx);
      const after = existing.slice(endIdx + NEX_RULES_MARKER_END.length);
      const updated = before + template.trim() + after;
      mkdirSync(dirname(rulesPath), { recursive: true });
      writeFileSync(rulesPath, updated, "utf-8");
    } else if (existing.includes("nex_ask") || existing.includes("WUPHF Context")) {
      // Already has wuphf content without markers — skip to avoid duplicates
      return { installed: true, rulesPath };
    } else {
      // Append to end
      const separator = existing && !existing.endsWith("\n\n") ? "\n\n" : "";
      mkdirSync(dirname(rulesPath), { recursive: true });
      writeFileSync(rulesPath, existing + separator + template.trim() + "\n", "utf-8");
    }
  } else {
    // Standalone mode: write the file directly
    mkdirSync(dirname(rulesPath), { recursive: true });
    writeFileSync(rulesPath, template, "utf-8");
  }

  return { installed: true, rulesPath };
}

// ── 8. Sync API key to ~/.wuphf/config.json ──────────────────────────────

/**
 * Persist API key to the canonical config file (~/.wuphf/config.json).
 * Name kept as syncApiKeyToMcpConfig for backward compatibility with callers.
 */
export function syncApiKeyToMcpConfig(apiKey: string): void {
  const configPath = join(homedir(), ".wuphf", "config.json");
  const config = readJsonFile(configPath);
  config.api_key = apiKey;
  writeJsonFile(configPath, config);
}
