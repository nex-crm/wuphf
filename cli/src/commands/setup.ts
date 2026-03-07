/**
 * `nex setup` command — detect platforms, install plugin/MCP, create .nex.toml.
 *
 * nex setup                     Interactive: detect → install plugin + scan files + config
 * nex setup --platform <name>   Direct install for specific platform
 * nex setup --with-mcp          Also install MCP server
 * nex setup --no-plugin         Skip hooks/commands, only config files
 * nex setup --no-scan           Skip file scanning during setup
 * nex setup status              Show install status + integration connections
 */

import { existsSync } from "node:fs";
import { join } from "node:path";
import { program } from "../cli.js";
import { resolveApiKey, resolveFormat, resolveTimeout, persistRegistration } from "../lib/config.js";
import { NexClient } from "../lib/client.js";
import { printOutput, printError } from "../lib/output.js";
import { confirm, ask } from "../lib/prompt.js";
import type { Format } from "../lib/output.js";
import { detectPlatforms, getPlatformById, VALID_PLATFORM_IDS } from "../lib/platform-detect.js";
import type { Platform } from "../lib/platform-detect.js";
import { installMcpServer, installClaudeCodePlugin, syncApiKeyToMcpConfig } from "../lib/installers.js";
import { writeDefaultProjectConfig } from "../lib/project-config.js";
import { scanFiles, loadScanConfig, isScanEnabled } from "../lib/file-scanner.js";

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
  noScan: boolean;
  format: Format;
}): Promise<void> {
  const globalOpts = program.opts();
  let apiKey = resolveApiKey(globalOpts.apiKey);

  // 1. Register if no API key
  if (!apiKey) {
    process.stderr.write("\nNo API key found. Let's set up your Nex account.\n\n");
    const wantsRegister = await confirm("Register a new Nex workspace?");

    if (!wantsRegister) {
      process.stderr.write("\nSetup complete, but no API key configured.\n\n");
      process.stderr.write("To use Nex, set your API key in one of these locations:\n");
      process.stderr.write('  1. Environment variable: export NEX_API_KEY="sk-..."\n');
      process.stderr.write('  2. Global config:        ~/.nex-mcp.json  → {"api_key": "sk-..."}\n');
      process.stderr.write('  3. Project config:       .nex.toml        → [auth] api_key = "sk-..."\n');
      process.stderr.write("\nGet an API key: nex register --email you@company.com\n\n");

      // Still install plugin hooks/commands (they'll gracefully degrade without a key)
      const platforms = detectPlatforms().filter((p) => p.detected);
      for (const platform of platforms) {
        if (platform.id === "claude-code" && !opts.noPlugin) {
          installClaudeCodePlugin();
        }
      }

      writeDefaultProjectConfig();
      return;
    }

    const email = await ask("Email (required):", true);
    const name = await ask("Name (optional):");

    process.stderr.write("\nRegistering...\n");
    const client = new NexClient(undefined, resolveTimeout(globalOpts.timeout));
    const data = await client.register(email, name || undefined);
    persistRegistration(data);
    apiKey = data.api_key as string;

    process.stderr.write(`\n  ✓ Workspace created! API key: ${maskKey(apiKey)}\n`);
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
        if (result.commandsCopied.length > 0) {
          results.push(`Claude Code: commands installed (${result.commandsCopied.join(", ")})`);
        }
      } else {
        results.push("Claude Code: bundled plugin not found — reinstall @nex-ai/nex");
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

  // 6. Scan and ingest project files (requires API key)
  if (!opts.noScan && apiKey && isScanEnabled()) {
    if (opts.format !== "json") {
      process.stderr.write("\nScanning project files...\n");
    }
    const scanOpts = loadScanConfig();
    const client = new NexClient(apiKey, resolveTimeout(globalOpts.timeout));
    try {
      const scanResult = await scanFiles(process.cwd(), scanOpts, async (content, context) => {
        return client.post("/v1/context/text", { content, context });
      });
      if (scanResult.scanned > 0) {
        results.push(`File scan: ${scanResult.scanned} files ingested, ${scanResult.skipped} skipped, ${scanResult.errors} errors`);
      } else if (scanResult.skipped > 0) {
        results.push(`File scan: all ${scanResult.skipped} files already up to date`);
      } else {
        results.push("File scan: no eligible files found in current directory");
      }
    } catch (err) {
      results.push(`File scan: failed — ${err instanceof Error ? err.message : String(err)}`);
    }
  }

  // 7. Output results
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

  // 8. Show status
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
  .option("--no-scan", "Skip file scanning during setup")
  .action(async (cmdOpts) => {
    const globalOpts = program.opts();
    const format = resolveFormat(globalOpts.format) as Format;
    await runSetup({
      platform: cmdOpts.platform,
      withMcp: cmdOpts.withMcp,
      noPlugin: cmdOpts.plugin === false,
      noScan: cmdOpts.scan === false,
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
