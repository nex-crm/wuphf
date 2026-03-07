/**
 * `nex setup` command — detect platforms, install plugin/MCP, create .nex.toml.
 *
 * nex setup                     Interactive: detect → install plugin + config
 * nex setup --platform <name>   Direct install for specific platform
 * nex setup --with-mcp          Also install MCP server
 * nex setup --no-plugin         Skip hooks/commands, only config files
 * nex setup status              Show install status + integration connections
 */

import { existsSync } from "node:fs";
import { join } from "node:path";
import { program } from "../cli.js";
import { resolveApiKey, resolveFormat, resolveTimeout } from "../lib/config.js";
import { NexClient } from "../lib/client.js";
import { printOutput, printError } from "../lib/output.js";
import type { Format } from "../lib/output.js";
import { detectPlatforms, getPlatformById, VALID_PLATFORM_IDS } from "../lib/platform-detect.js";
import type { Platform } from "../lib/platform-detect.js";
import { installMcpServer, installClaudeCodePlugin, syncApiKeyToMcpConfig } from "../lib/installers.js";
import { writeDefaultProjectConfig } from "../lib/project-config.js";

function getClient(): { client: NexClient; format: Format } {
  const opts = program.opts();
  const client = new NexClient(resolveApiKey(opts.apiKey), resolveTimeout(opts.timeout));
  return { client, format: resolveFormat(opts.format) as Format };
}

// --- Status subcommand ---

async function showStatus(format: Format): Promise<void> {
  const opts = program.opts();
  const apiKey = resolveApiKey(opts.apiKey);
  const platforms = detectPlatforms();

  if (format === "json") {
    const data: Record<string, unknown> = {
      auth: apiKey ? { configured: true, key_preview: maskKey(apiKey) } : { configured: false },
      platforms: platforms.map((p) => ({
        id: p.id,
        name: p.displayName,
        detected: p.detected,
        nex_installed: p.nexInstalled,
        plugin_support: p.pluginSupport,
      })),
      project_config: projectConfigExists(),
    };

    // Fetch integrations if authenticated
    if (apiKey) {
      try {
        const client = new NexClient(apiKey, resolveTimeout(opts.timeout));
        data.integrations = await client.get("/v1/integrations/");
      } catch {
        data.integrations = null;
      }
    }

    printOutput(data, "json");
    return;
  }

  // Text format
  const lines: string[] = [];
  lines.push("Nex Setup Status");
  lines.push("================");
  lines.push("");

  // Auth
  if (apiKey) {
    lines.push(`Auth: Configured (${maskKey(apiKey)})`);
  } else {
    lines.push("Auth: Not configured — run `nex register` first");
  }
  lines.push("");

  // Platforms
  lines.push("Platforms:");
  for (const p of platforms) {
    if (!p.detected) {
      lines.push(`  ${padRight(p.displayName, 20)} Not found`);
      continue;
    }

    const parts = [`  ${padRight(p.displayName, 20)} Detected`];

    if (p.pluginSupport) {
      parts.push(`   Plugin: ${p.nexInstalled ? "installed" : "not installed"}`);
    }

    // MCP status
    if (p.id !== "claude-code") {
      parts.push(`   MCP: ${p.nexInstalled ? "installed" : "not installed"}`);
    }

    lines.push(parts.join(""));
  }
  lines.push("");

  // Project config
  lines.push(`Project Config: ${projectConfigExists() ? ".nex.toml found" : ".nex.toml not found"}`);
  lines.push("");

  // Integrations
  if (apiKey) {
    try {
      const client = new NexClient(apiKey, resolveTimeout(opts.timeout));
      const integrations = await client.get<Record<string, unknown>[]>("/v1/integrations/");
      if (Array.isArray(integrations) && integrations.length > 0) {
        lines.push("Connections:");
        for (const integration of integrations) {
          const type = String(integration.type ?? "");
          const provider = String(integration.provider ?? "");
          const label = `${type} / ${provider}`;
          const connections = integration.connections as Array<Record<string, unknown>> | undefined;

          if (connections && connections.length > 0) {
            for (const conn of connections) {
              const displayName = conn.display_name ?? conn.email ?? "";
              lines.push(`  ${padRight(label, 25)} \u25CF connected     ${displayName}     (ID: ${conn.id})`);
            }
          } else {
            lines.push(`  ${padRight(label, 25)} \u25CB not connected \u2192 nex integrate connect ${type} ${provider}`);
          }
        }
      }
    } catch {
      lines.push("Connections: Could not fetch (check auth)");
    }
  }

  process.stdout.write(lines.join("\n") + "\n");
}

// --- Main setup flow ---

async function runSetup(opts: {
  platform?: string;
  withMcp: boolean;
  noPlugin: boolean;
  format: Format;
}): Promise<void> {
  const globalOpts = program.opts();
  const apiKey = resolveApiKey(globalOpts.apiKey);

  // 1. Check auth
  if (!apiKey) {
    printError("No API key found. Run `nex register` first to create an account.");
    process.exit(2);
  }

  // 2. Sync API key to shared config
  syncApiKeyToMcpConfig(apiKey);

  // 3. Detect or select platforms
  let targetPlatforms: Platform[];

  if (opts.platform) {
    const p = getPlatformById(opts.platform);
    if (!p) {
      printError(
        `Unknown platform "${opts.platform}". Valid: ${VALID_PLATFORM_IDS.join(", ")}`
      );
      process.exit(1);
    }
    targetPlatforms = [p];
  } else {
    targetPlatforms = detectPlatforms().filter((p) => p.detected);

    if (targetPlatforms.length === 0) {
      process.stderr.write("No supported platforms detected.\n");
      process.stderr.write(`Supported: ${VALID_PLATFORM_IDS.join(", ")}\n`);
      process.stderr.write("Use --platform <name> to install manually.\n");
    }
  }

  const results: string[] = [];

  // 4. Install for each platform
  for (const platform of targetPlatforms) {
    // Claude Code — install plugin (hooks + commands) unless --no-plugin
    if (platform.id === "claude-code" && !opts.noPlugin) {
      const result = installClaudeCodePlugin();
      if (result.installed) {
        if (result.hooksAdded.length > 0) {
          results.push(`Claude Code: hooks installed (${result.hooksAdded.join(", ")})`);
        } else {
          results.push("Claude Code: hooks already configured");
        }
        if (result.commandsLinked.length > 0) {
          results.push(`Claude Code: commands linked (${result.commandsLinked.join(", ")})`);
        }
      } else {
        results.push("Claude Code: plugin directory not found — install @nex-crm/claude-code-nex-plugin");
      }
    }

    // MCP server — only with --with-mcp
    if (opts.withMcp && platform.id !== "claude-code") {
      const result = installMcpServer(platform, apiKey);
      if (result.installed) {
        results.push(`${platform.displayName}: MCP server configured at ${result.configPath}`);
      }
    } else if (platform.id !== "claude-code" && !opts.withMcp) {
      results.push(`${platform.displayName}: detected — use --with-mcp to install MCP server`);
    }

    // Claude Code MCP — also with --with-mcp
    if (opts.withMcp && platform.id === "claude-code") {
      const result = installMcpServer(platform, apiKey);
      if (result.installed) {
        results.push(`Claude Code: MCP server configured`);
      }
    }
  }

  // 5. Create .nex.toml
  const created = writeDefaultProjectConfig();
  if (created) {
    results.push("Created .nex.toml with default settings");
  } else {
    results.push(".nex.toml already exists");
  }

  // 6. Output results
  if (opts.format === "json") {
    printOutput({
      platforms: targetPlatforms.map((p) => ({
        id: p.id,
        name: p.displayName,
        detected: p.detected,
      })),
      results,
    }, "json");
  } else {
    process.stderr.write("\n");
    for (const r of results) {
      process.stderr.write(`  \u2713 ${r}\n`);
    }
    process.stderr.write("\n");
  }

  // 7. Show status
  if (opts.format !== "json") {
    await showStatus(opts.format);
  }
}

// --- Helpers ---

function maskKey(key: string): string {
  if (key.length <= 8) return "****";
  return key.slice(0, 4) + "****" + key.slice(-4);
}

function padRight(str: string, len: number): string {
  return str.length >= len ? str : str + " ".repeat(len - str.length);
}

function projectConfigExists(): boolean {
  return existsSync(join(process.cwd(), ".nex.toml"));
}

// --- Command registration ---

const setup = program
  .command("setup")
  .description("Set up Nex integration for your development environment")
  .option("--platform <name>", `Target platform: ${VALID_PLATFORM_IDS.join(", ")}`)
  .option("--with-mcp", "Also install MCP server for detected platforms", false)
  .option("--no-plugin", "Skip plugin (hooks/commands), only update config files")
  .action(async (cmdOpts) => {
    const globalOpts = program.opts();
    const format = resolveFormat(globalOpts.format) as Format;
    await runSetup({
      platform: cmdOpts.platform,
      withMcp: cmdOpts.withMcp,
      noPlugin: cmdOpts.noPlugin ?? false,
      format,
    });
  });

setup
  .command("status")
  .description("Show Nex installation status across all platforms")
  .action(async () => {
    const globalOpts = program.opts();
    const format = resolveFormat(globalOpts.format) as Format;
    await showStatus(format);
  });
