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
import { resolveApiKey, resolveFormat, resolveTimeout, persistRegistration, loadConfig } from "../lib/config.js";
import { NexClient } from "../lib/client.js";
import { printOutput, printError } from "../lib/output.js";
import { confirm, ask, choose } from "../lib/prompt.js";
import type { Format } from "../lib/output.js";
import { heading, keyValue, tree, badge, style, sym, spinner as createSpinner, exitHint } from "../lib/tui.js";

const INTEGRATIONS_MAP: Record<string, { type: string; provider: string }> = {
  gmail: { type: "email", provider: "google" },
  "google-calendar": { type: "calendar", provider: "google" },
  outlook: { type: "email", provider: "microsoft" },
  "outlook-calendar": { type: "calendar", provider: "microsoft" },
  slack: { type: "messaging", provider: "slack" },
  salesforce: { type: "crm", provider: "salesforce" },
  hubspot: { type: "crm", provider: "hubspot" },
  attio: { type: "crm", provider: "attio" },
};
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

async function showStatus(format: Format, overrideApiKey?: string): Promise<void> {
  const opts = program.opts();
  const apiKey = overrideApiKey ?? resolveApiKey(opts.apiKey);
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

    // Fetch integrations if authenticated (short timeout — don't block setup)
    if (apiKey) {
      try {
        const client = new NexClient(apiKey, 5_000);
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
  lines.push(heading("Nex Setup Status"));
  lines.push("");

  // Auth
  const authEntries: [string, string][] = [];
  if (apiKey) {
    authEntries.push(["Auth", maskKey(apiKey)]);
  } else {
    authEntries.push(["Auth", style.yellow("Not configured") + style.dim(" — run `nex register` first")]);
  }
  lines.push(keyValue(authEntries));
  lines.push("");

  // Project config
  lines.push(keyValue([
    ["Project Config", projectConfigExists() ? style.green(".nex.toml found") : style.dim(".nex.toml not found")],
  ]));
  lines.push("");

  // Platforms
  lines.push(`  ${style.bold("Platforms")}`);
  const platformItems = platforms.map((p) => {
    let statusLabel: string;
    if (!p.detected) {
      statusLabel = `${p.displayName}     ${badge("not detected", "dim")}`;
      return { label: statusLabel };
    }
    const children: string[] = [];
    if (p.pluginSupport) {
      children.push(p.nexInstalled ? badge("hooks installed", "success") : badge("not installed", "dim"));
    }
    if (p.id !== "claude-code") {
      children.push(p.nexInstalled ? badge("MCP installed", "success") : badge("not installed", "dim"));
    }
    return { label: `${p.displayName}`, children };
  });
  lines.push(tree(platformItems));
  lines.push("");

  // Integrations (short timeout — don't block setup)
  if (apiKey) {
    try {
      const client = new NexClient(apiKey, 5_000);
      const integrations = await client.get<Record<string, unknown>[]>("/v1/integrations/");
      if (Array.isArray(integrations) && integrations.length > 0) {
        lines.push(`  ${style.bold("Connections")}`);
        const connItems = integrations.map((integration) => {
          const type = String(integration.type ?? "");
          const provider = String(integration.provider ?? "");
          const label = `${type} / ${provider}`;
          const connections = integration.connections as Array<Record<string, unknown>> | undefined;

          if (connections && connections.length > 0) {
            const children = connections.map((conn) => {
              const displayName = String(conn.display_name ?? conn.email ?? "");
              return `${badge("connected", "success")}  ${displayName}  ${style.dim(`(ID: ${conn.id})`)}`;
            });
            return { label, children };
          }

          // Find shortcut name for connect command
          const shortcut = Object.entries(INTEGRATIONS_MAP).find(
            ([, v]) => v.type === type && v.provider === provider
          );
          const connectHint = shortcut
            ? style.dim(`→ nex integrate connect ${shortcut[0]}`)
            : "";
          return { label: `${label}     ${badge("not connected", "dim")} ${connectHint}` };
        });
        lines.push(tree(connItems));
      }
    } catch {
      lines.push(`  ${style.dim("Connections: Could not fetch (check auth)")}`);
    }
  }

  process.stdout.write(lines.join("\n") + "\n");
}

// --- Main setup flow ---

async function registerAndPersist(globalOpts: { timeout?: string; apiKey?: string }, existingEmail?: string): Promise<string> {
  const email = existingEmail ?? await ask("Email (required):", true);
  const name = await ask("Name (optional):");

  process.stderr.write("\nRegistering...\n");
  const client = new NexClient(undefined, resolveTimeout(globalOpts.timeout));
  const data = await client.register(email, name || undefined);
  // Ensure email is persisted even if the API doesn't return it
  data.email = email;
  persistRegistration(data);
  const apiKey = data.api_key as string;

  const wsSlug = data.workspace_slug as string | undefined;
  process.stderr.write(`\n  ✓ Registered!${wsSlug ? ` (${wsSlug})` : ""}\n`);
  process.stderr.write(`    API key: ${apiKey}\n`);
  process.stderr.write(`    Saved to: ~/.nex/config.json\n\n`);
  return apiKey;
}

async function runSetup(opts: {
  platform?: string;
  withMcp: boolean;
  noPlugin: boolean;
  noScan: boolean;
  format: Format;
}): Promise<void> {
  const globalOpts = program.opts();
  let apiKey = resolveApiKey(globalOpts.apiKey);

  // 1. Register or re-register
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

    apiKey = await registerAndPersist(globalOpts);
  } else {
    // Key exists — offer to regenerate (picks up new scopes, fixes expired keys)
    const config = loadConfig();
    const maskedKey = apiKey.slice(0, 6) + "..." + apiKey.slice(-4);
    const existingEmail = config.email;

    process.stderr.write(`\nAPI key: ${maskedKey}`);
    if (config.workspace_slug) {
      process.stderr.write(` (workspace: ${config.workspace_slug})`);
    }
    if (existingEmail) {
      process.stderr.write(`\nEmail:   ${existingEmail}`);
    }
    process.stderr.write("\n\n");

    const choice = await choose("Generate a new API key?", [
      `Generate new key${existingEmail ? ` for ${existingEmail}` : ""}`,
      "Change email and generate new key",
      "Keep current key",
    ]);

    if (choice === 0) {
      // Regenerate with existing email (no prompt)
      apiKey = await registerAndPersist(globalOpts, existingEmail);
    } else if (choice === 1) {
      // Change email — prompt for new one
      apiKey = await registerAndPersist(globalOpts);
    }
    // choice === 2: keep current key, do nothing
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
    const showSpinner = opts.format !== "json";
    const spin = showSpinner ? createSpinner(`Scanning project files...  ${exitHint}`) : null;
    const scanOpts = loadScanConfig();
    const client = new NexClient(apiKey, resolveTimeout(globalOpts.timeout));
    try {
      const scanResult = await scanFiles(process.cwd(), scanOpts, async (content, context) => {
        return client.post("/v1/context/text", { content, context });
      }, (current, total, filePath) => {
        const name = filePath.replace(process.cwd() + "/", "");
        spin?.update(`Scanning files (${current}/${total}): ${name}  ${exitHint}`);
      });
      if (scanResult.scanned > 0) {
        spin?.succeed(`${scanResult.scanned} files ingested, ${scanResult.skipped} skipped, ${scanResult.errors} errors`);
        results.push(`File scan: ${scanResult.scanned} files ingested, ${scanResult.skipped} skipped, ${scanResult.errors} errors`);
      } else if (scanResult.skipped > 0) {
        spin?.succeed(`All ${scanResult.skipped} files already up to date`);
        results.push(`File scan: all ${scanResult.skipped} files already up to date`);
      } else {
        spin?.succeed("No eligible files found in current directory");
        results.push("File scan: no eligible files found in current directory");
      }
    } catch (err) {
      spin?.fail(`Scan failed — ${err instanceof Error ? err.message : String(err)}`);
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

  // 8. Show status (pass current apiKey — may differ from global opts after re-registration)
  if (opts.format !== "json") {
    await showStatus(opts.format, apiKey);
  }
}

// --- Helpers ---

function maskKey(key: string): string {
  if (key.length <= 8) return "****";
  return key.slice(0, 4) + "****" + key.slice(-4);
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
