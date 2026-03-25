/**
 * /init command — complete setup flow that takes a user from "nothing configured"
 * to "WUPHF is fully integrated with all their AI agents."
 *
 * Steps:
 *   1. Registration (if no API key)
 *   2. Platform detection
 *   3. Installation per platform (hooks, commands, rules, MCP)
 *   4. Persist config to ~/.wuphf/config.json
 */

import { NexClient } from "../lib/client.js";
import { loadConfig, persistRegistration, saveConfig, resolveApiKey } from "../lib/config.js";
import {
  installClaudeCodePlugin,
  installMcpServer,
  installRulesFile,
  installHooks,
  installOpenCodePlugin,
  installOpenClawPlugin,
  installVSCodeAgent,
  installKiloCodeMode,
  installWindsurfWorkflows,
} from "../lib/installers.js";
import {
  detectPlatforms as detectAllPlatforms,
  getDetectedPlatforms,
} from "../lib/platform-detect.js";
import type { Platform } from "../lib/platform-detect.js";

// ── Public types ──────────────────────────────────────────────────────

export interface InitProgress {
  step: string;
  detail?: string;
  done?: boolean;
  error?: string;
}

export type InitProgressCallback = (progress: InitProgress) => void;

export interface DetectedPlatform {
  name: string;
  slug: string;
  detected: boolean;
  nexInstalled: boolean;
  configPath: string;
  capabilities: {
    hooks: boolean;
    rules: boolean;
    mcp: boolean;
    commands: boolean;
    plugins: boolean;
    agents: boolean;
    workflows: boolean;
  };
}

export interface RegistrationResult {
  apiKey: string;
  workspaceId: string;
  workspaceSlug: string;
}

// ── Helpers ──────────────────────────────────────────────────────────

function mapPlatform(p: Platform): DetectedPlatform {
  return {
    name: p.displayName,
    slug: p.id,
    detected: p.detected,
    nexInstalled: p.nexInstalled,
    configPath: p.configPath,
    capabilities: {
      hooks: p.supportsHooks,
      rules: p.supportsRules,
      mcp: !!p.configPath,
      commands: p.id === "claude-code",
      plugins: p.pluginSupport,
      agents: p.supportsCustomAgents,
      workflows: p.supportsWorkflows,
    },
  };
}

// ── Step 1: Registration ─────────────────────────────────────────────

export async function registerUser(
  email: string,
): Promise<RegistrationResult> {
  const client = new NexClient();
  const data = await client.register(email);

  const apiKey = typeof data.api_key === "string" ? data.api_key : "";
  const workspaceId = String(data.workspace_id ?? "");
  const workspaceSlug = typeof data.workspace_slug === "string" ? data.workspace_slug : "";

  if (!apiKey) {
    throw new Error("Registration did not return an API key.");
  }

  // Persist to ~/.wuphf/config.json (canonical config)
  persistRegistration({
    api_key: apiKey,
    email,
    workspace_id: workspaceId,
    workspace_slug: workspaceSlug,
  });

  return { apiKey, workspaceId, workspaceSlug };
}

// ── Step 2: Platform Detection ───────────────────────────────────────

export function detectPlatforms(): DetectedPlatform[] {
  return detectAllPlatforms().map(mapPlatform);
}

export function getDetected(): DetectedPlatform[] {
  return getDetectedPlatforms().map(mapPlatform);
}

// ── Step 3: Installation per platform ────────────────────────────────

export function installForPlatform(
  platform: DetectedPlatform,
  apiKey: string,
  onProgress: InitProgressCallback,
): void {
  // We need the full Platform object for the installers
  const allPlatforms = detectAllPlatforms();
  const fullPlatform = allPlatforms.find((p) => p.id === platform.slug);

  if (!fullPlatform) {
    onProgress({ step: "install", detail: `Unknown platform: ${platform.name}`, error: "Platform not found" });
    return;
  }

  // Layer 1: Hooks
  if (platform.capabilities.hooks) {
    try {
      if (platform.slug === "claude-code") {
        const result = installClaudeCodePlugin();
        if (result.installed) {
          onProgress({
            step: "hooks",
            detail: `Installed hooks for ${platform.name}: ${result.hooksAdded.join(", ")}`,
          });
          if (result.commandsCopied.length > 0) {
            onProgress({
              step: "commands",
              detail: `Copied ${result.commandsCopied.length} slash commands to ~/.claude/commands/`,
            });
          }
        }
      } else {
        const result = installHooks(fullPlatform);
        if (result.installed) {
          onProgress({
            step: "hooks",
            detail: `Installed hooks for ${platform.name}: ${result.hooksAdded.join(", ")}`,
          });
        }
      }
    } catch (err) {
      onProgress({
        step: "hooks",
        detail: `Failed to install hooks for ${platform.name}`,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }

  // Layer 2: Custom tools/plugins
  if (platform.capabilities.plugins) {
    try {
      if (platform.slug === "opencode") {
        const result = installOpenCodePlugin();
        if (result.installed) {
          onProgress({
            step: "plugins",
            detail: `Installed OpenCode plugin at ${result.pluginPath}`,
          });
        }
      } else if (platform.slug === "openclaw") {
        const result = installOpenClawPlugin(apiKey);
        onProgress({
          step: "plugins",
          detail: result.message,
        });
      }
    } catch (err) {
      onProgress({
        step: "plugins",
        detail: `Failed to install plugin for ${platform.name}`,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }

  // Layer 3: Custom agents/modes
  if (platform.capabilities.agents) {
    try {
      if (platform.slug === "vscode") {
        const result = installVSCodeAgent();
        if (result.installed) {
          onProgress({
            step: "agents",
            detail: `Installed VS Code agent at ${result.agentPath}`,
          });
        }
      } else if (platform.slug === "kilocode") {
        const result = installKiloCodeMode();
        if (result.installed) {
          onProgress({
            step: "agents",
            detail: `Installed Kilo Code mode at ${result.modePath}`,
          });
        }
      }
    } catch (err) {
      onProgress({
        step: "agents",
        detail: `Failed to install agent for ${platform.name}`,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }

  // Layer 4: Workflows
  if (platform.capabilities.workflows && platform.slug === "windsurf") {
    try {
      const result = installWindsurfWorkflows();
      if (result.installed) {
        onProgress({
          step: "workflows",
          detail: `Installed ${result.workflowCount} Windsurf workflows`,
        });
      }
    } catch (err) {
      onProgress({
        step: "workflows",
        detail: `Failed to install workflows for ${platform.name}`,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }

  // Layer 5: Rules
  if (platform.capabilities.rules) {
    try {
      const result = installRulesFile(fullPlatform);
      if (result.installed) {
        onProgress({
          step: "rules",
          detail: `Installed rules for ${platform.name} at ${result.rulesPath}`,
        });
      }
    } catch (err) {
      onProgress({
        step: "rules",
        detail: `Failed to install rules for ${platform.name}`,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }

  // Layer 6: MCP server
  if (platform.capabilities.mcp && platform.slug !== "claude-code") {
    try {
      const result = installMcpServer(fullPlatform, apiKey);
      if (result.installed) {
        onProgress({
          step: "mcp",
          detail: `Installed MCP server for ${platform.name} at ${result.configPath}`,
        });
      }
    } catch (err) {
      onProgress({
        step: "mcp",
        detail: `Failed to install MCP server for ${platform.name}`,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }
}

// ── Step 4: Persist config to ~/.wuphf/config.json ─────────────────────

export function writeMcpConfig(
  apiKey: string,
  workspaceId: string,
  workspaceSlug: string,
): void {
  const existing = loadConfig();
  existing.api_key = apiKey;
  existing.workspace_id = workspaceId;
  existing.workspace_slug = workspaceSlug;
  saveConfig(existing);
}

// ── Main init flow ───────────────────────────────────────────────────

export async function runInit(
  onProgress: InitProgressCallback,
  options?: {
    /** If provided, skip the registration prompt. */
    email?: string;
    /** If provided, skip registration entirely. */
    apiKey?: string;
  },
): Promise<void> {
  // Step 1: Ensure we have an API key
  let apiKey = options?.apiKey ?? resolveApiKey();
  let workspaceId = "";
  let workspaceSlug = "";

  if (apiKey) {
    onProgress({ step: "auth", detail: "Already authenticated." });
    const config = loadConfig();
    workspaceId = config.workspace_id ?? "";
    workspaceSlug = config.workspace_slug ?? "";
  } else {
    const email = options?.email;
    if (!email) {
      onProgress({ step: "auth", detail: "Email required for registration.", error: "no_email" });
      return;
    }

    onProgress({ step: "auth", detail: `Registering with ${email}...` });

    try {
      const result = await registerUser(email);
      apiKey = result.apiKey;
      workspaceId = result.workspaceId;
      workspaceSlug = result.workspaceSlug;
      onProgress({ step: "auth", detail: "Registration complete." });
    } catch (err) {
      onProgress({
        step: "auth",
        detail: "Registration failed.",
        error: err instanceof Error ? err.message : String(err),
      });
      return;
    }
  }

  // Step 2: Detect platforms
  onProgress({ step: "detect", detail: "Scanning for AI coding platforms..." });

  const detected = getDetected();
  const found = detected.filter((p) => p.detected);

  if (found.length === 0) {
    onProgress({
      step: "detect",
      detail: "No AI coding platforms detected.",
      done: true,
    });
    // Still write MCP config
    writeMcpConfig(apiKey, workspaceId, workspaceSlug);
    return;
  }

  const platformNames = found.map((p) => p.name).join(", ");
  onProgress({
    step: "detect",
    detail: `Found ${found.length} platform(s): ${platformNames}`,
  });

  // Step 3: Install for each detected platform
  for (const platform of found) {
    if (platform.nexInstalled) {
      onProgress({
        step: "install",
        detail: `${platform.name}: WUPHF already installed, updating...`,
      });
    } else {
      onProgress({
        step: "install",
        detail: `Installing WUPHF for ${platform.name}...`,
      });
    }

    installForPlatform(platform, apiKey, onProgress);
  }

  // Step 4: Ensure ~/.wuphf/config.json is written
  writeMcpConfig(apiKey, workspaceId, workspaceSlug);

  onProgress({
    step: "complete",
    detail: `WUPHF installed for ${found.length} platform(s).`,
    done: true,
  });
}
