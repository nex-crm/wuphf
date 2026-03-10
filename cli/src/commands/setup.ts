/**
 * `nex setup` command — detect platforms, install hooks/plugins/MCP, create .nex.toml.
 *
 * nex setup                     Interactive: detect → install full stack + scan files + config
 * nex setup --platform <name>   Direct install for specific platform
 * nex setup --no-hooks          Skip hook installation
 * nex setup --no-rules          Skip rules/instruction file installation
 * nex setup --no-plugin         Skip hooks/commands (alias for --no-hooks)
 * nex setup --no-integrations    Skip integration connection step
 * nex setup --no-scan           Skip file scanning during setup
 * nex setup status              Show install status + integration connections
 */

import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { program } from "../cli.js";
import { resolveApiKey, resolveFormat, resolveTimeout, persistRegistration, loadConfig } from "../lib/config.js";
import { NexClient } from "../lib/client.js";
import { printOutput, printError } from "../lib/output.js";
import { confirm, ask, choose, multiSelect } from "../lib/prompt.js";
import type { Format } from "../lib/output.js";
import { heading, keyValue, tree, badge, style, sym, spinner as createSpinner, exitHint, isTTY } from "../lib/tui.js";
import { openBrowser } from "./integrate.js";

const INTEGRATIONS_MAP: Record<string, { type: string; provider: string; displayName: string; description: string }> = {
  gmail: { type: "email", provider: "google", displayName: "Gmail", description: "Connect your Gmail account to sync emails" },
  "google-calendar": { type: "calendar", provider: "google", displayName: "Google Calendar (Nex Meeting Bot)", description: "Connect Google Calendar for meeting transcripts" },
  outlook: { type: "email", provider: "microsoft", displayName: "Outlook", description: "Connect your Outlook account to sync emails" },
  "outlook-calendar": { type: "calendar", provider: "microsoft", displayName: "Outlook Calendar (Nex Meeting Bot)", description: "Connect Outlook Calendar for meeting transcripts" },
  slack: { type: "messaging", provider: "slack", displayName: "Slack", description: "Connect Slack to sync messages" },
  salesforce: { type: "crm", provider: "salesforce", displayName: "Salesforce", description: "Connect Salesforce CRM" },
  hubspot: { type: "crm", provider: "hubspot", displayName: "HubSpot", description: "Connect HubSpot CRM" },
  attio: { type: "crm", provider: "attio", displayName: "Attio", description: "Connect Attio CRM" },
};
import { detectPlatforms, getPlatformById, VALID_PLATFORM_IDS } from "../lib/platform-detect.js";
import type { Platform } from "../lib/platform-detect.js";
import {
  installMcpServer,
  installClaudeCodePlugin,
  installRulesFile,
  syncApiKeyToMcpConfig,
  installHooks,
  installOpenCodePlugin,
  installOpenClawPlugin,
  installVSCodeAgent,
  installKiloCodeMode,
  installContinueProvider,
  installWindsurfWorkflows,
} from "../lib/installers.js";
import { writeDefaultProjectConfig } from "../lib/project-config.js";
import { scanFiles, loadScanConfig, isScanEnabled } from "../lib/file-scanner.js";

function getClient(): { client: NexClient; format: Format } {
  const opts = program.opts();
  const client = new NexClient(resolveApiKey(opts.apiKey), resolveTimeout(opts.timeout));
  return { client, format: resolveFormat(opts.format) as Format };
}

// --- Helpers ---

function hasNexMcpInConfig(platform: Platform): boolean {
  try {
    const raw = readFileSync(platform.configPath, "utf-8");
    const config = JSON.parse(raw);
    return !!(config?.mcpServers?.nex || config?.context_servers?.nex);
  } catch {
    return false;
  }
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
        hooks: p.supportsHooks,
        custom_tools: p.supportsCustomTools,
        custom_agents: p.supportsCustomAgents,
        workflows: p.supportsWorkflows,
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

    // Hooks status
    if (p.supportsHooks || p.pluginSupport) {
      children.push(p.nexInstalled ? badge("hooks installed", "success") : badge("hooks not installed", "dim"));
    }

    // Custom tools/plugins
    if (p.supportsCustomTools) {
      children.push(badge("plugin support", "dim"));
    }

    // Custom agents
    if (p.supportsCustomAgents) {
      children.push(badge("agent support", "dim"));
    }

    // Workflows
    if (p.supportsWorkflows && p.id !== "claude-code") {
      children.push(badge("workflows", "dim"));
    }

    // Rules
    if (p.supportsRules) {
      const rulesInstalled = p.rulesPath ? existsSync(p.rulesPath) : false;
      children.push(rulesInstalled ? badge("rules installed", "success") : badge("rules not installed", "dim"));
    }

    // MCP
    if (p.id !== "claude-code" && p.id !== "openclaw" && p.id !== "aider" && p.configPath) {
      const mcpInstalled = hasNexMcpInConfig(p);
      children.push(mcpInstalled ? badge("MCP installed", "success") : badge("MCP not installed", "dim"));
    }

    // OpenClaw plugin
    if (p.id === "openclaw") {
      children.push(p.nexInstalled ? badge("plugin installed", "success") : badge("plugin not installed", "dim"));
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
  const name = existingEmail ? undefined : await ask("Name (optional):");

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
  noPlugin: boolean;
  noHooks: boolean;
  noRules: boolean;
  noIntegrations: boolean;
  noScan: boolean;
  format: Format;
}): Promise<void> {
  const globalOpts = program.opts();
  let apiKey = resolveApiKey(globalOpts.apiKey);

  // Combine --no-plugin and --no-hooks (both skip hooks)
  const skipHooks = opts.noPlugin || opts.noHooks;

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
        if (platform.id === "claude-code" && !skipHooks) {
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
      apiKey = await registerAndPersist(globalOpts, existingEmail);
    } else if (choice === 1) {
      apiKey = await registerAndPersist(globalOpts);
    }
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

  // 4. Install for each platform — 6-layer hierarchy
  for (const platform of targetPlatforms) {
    const installed: string[] = [];

    // ── Layer 1: Hooks (event-driven scripts) ──
    if (!skipHooks) {
      if (platform.id === "claude-code" && platform.pluginSupport) {
        // Claude Code has its own installer (hooks + slash commands)
        const result = installClaudeCodePlugin();
        if (result.installed) {
          if (result.hooksAdded.length > 0) {
            installed.push(`hooks (${result.hooksAdded.join(", ")})`);
          }
          if (result.commandsCopied.length > 0) {
            installed.push(`commands (${result.commandsCopied.length} slash commands)`);
          }
        } else {
          results.push(`${platform.displayName}: bundled plugin not found — reinstall @nex-ai/nex`);
          continue;
        }
      } else if (platform.supportsHooks) {
        const result = installHooks(platform);
        if (result.installed && result.hooksAdded.length > 0) {
          installed.push(`hooks (${result.hooksAdded.join(", ")})`);
        }
      }
    }

    // ── Layer 2: Custom tools/plugins ──
    if (platform.id === "opencode" && platform.supportsCustomTools) {
      const result = installOpenCodePlugin();
      if (result.installed) {
        installed.push(`plugin (${result.pluginPath.replace(process.cwd() + "/", "")})`);
      }
    }

    if (platform.id === "openclaw" && platform.pluginSupport) {
      const result = installOpenClawPlugin(apiKey);
      installed.push(result.message);
      if (!result.installed) {
        results.push(`${platform.displayName}: ${result.message}`);
        continue;
      }
    }

    // ── Layer 3: Custom agents/modes ──
    if (platform.supportsCustomAgents) {
      if (platform.id === "vscode") {
        const result = installVSCodeAgent();
        if (result.installed) {
          installed.push(`agent (${result.agentPath.replace(process.cwd() + "/", "")})`);
        }
      }
      if (platform.id === "kilocode") {
        const result = installKiloCodeMode();
        if (result.installed) {
          installed.push(`mode (${result.modePath.replace(process.cwd() + "/", "")})`);
        }
      }
    }

    // ── Layer 4: Workflows/slash commands ──
    if (platform.supportsWorkflows && platform.id !== "claude-code") {
      if (platform.id === "windsurf") {
        const result = installWindsurfWorkflows();
        if (result.installed) {
          installed.push(`workflows (${result.workflowCount})`);
        }
      }
    }

    // ── Layer 5: Rules/instructions file ──
    if (platform.supportsRules && !opts.noRules) {
      const result = installRulesFile(platform);
      if (result.installed) {
        const relPath = result.rulesPath.replace(process.cwd() + "/", "");
        installed.push(`rules (${relPath})`);
      }
    }

    // ── Layer 6: MCP server ──
    // For all platforms except: Claude Code (hooks-only), OpenClaw (plugin-only), Aider (no MCP)
    if (platform.id !== "claude-code" && platform.id !== "openclaw" && platform.id !== "aider" && platform.configPath) {
      const result = installMcpServer(platform, apiKey);
      if (result.installed) {
        installed.push(`MCP (${result.configPath})`);
      }
    }

    if (installed.length > 0) {
      results.push(`${platform.displayName}: ${installed.join(" + ")}`);
    } else {
      results.push(`${platform.displayName}: detected`);
    }
  }

  // 5. Interactive integration connection
  if (apiKey && isTTY && opts.format !== "json" && !opts.noIntegrations) {
    await connectIntegrations(apiKey);
  }

  // 6. Create .nex.toml
  const created = writeDefaultProjectConfig();
  if (created) {
    results.push("Created .nex.toml with default settings");
  } else {
    results.push(".nex.toml already exists");
  }

  // 7. Scan and ingest project files (requires API key)
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

  // 8. Output results
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

  // 9. Show status (pass current apiKey — may differ from global opts after re-registration)
  if (opts.format !== "json") {
    await showStatus(opts.format, apiKey);
  }
}

// --- Integration connection step ---

export interface IntegrationEntry {
  type: string;
  provider: string;
  display_name: string;
  description: string;
  connections: Array<{ id: string | number; status: string; identifier: string }>;
}

/** Exported for testing. */
export function fallbackIntegrations(): IntegrationEntry[] {
  return Object.values(INTEGRATIONS_MAP).map((entry) => ({
    type: entry.type,
    provider: entry.provider,
    display_name: entry.displayName,
    description: entry.description,
    connections: [],
  }));
}

async function connectIntegrations(apiKey: string): Promise<void> {
  const client = new NexClient(apiKey, 10_000);

  let integrations: IntegrationEntry[];
  try {
    const fetched = await client.get<IntegrationEntry[]>("/v1/integrations/", 10_000);
    if (!Array.isArray(fetched) || fetched.length === 0) {
      process.stderr.write(`\n  ${style.dim("No integrations available yet.")} Run ${style.bold("nex integrate")} to connect integrations later.\n`);
      return;
    }
    integrations = fetched;
  } catch {
    process.stderr.write(`\n  ${style.dim("Could not fetch integrations.")} Run ${style.bold("nex integrate")} to connect integrations later.\n`);
    return;
  }

  // Build multi-select options
  const options = integrations.map((item) => {
    const connected = item.connections.length > 0;
    const connInfo = connected
      ? ` ${style.green("connected")} (${item.connections.map((c) => c.identifier).join(", ")})`
      : "";
    return {
      label: `${item.display_name}${connInfo}`,
      checked: false,
      disabled: connected,
    };
  });

  // Check if all are already connected
  if (options.every((o) => o.disabled)) {
    process.stderr.write(`\n  ${sym.success} All integrations already connected\n`);
    return;
  }

  process.stderr.write("\n");
  const selectedIndices = await multiSelect("Connect integrations:", options);

  if (selectedIndices.length === 0) return;

  // Connect each selected integration
  for (const idx of selectedIndices) {
    const item = integrations[idx];
    const shortcut = Object.entries(INTEGRATIONS_MAP).find(
      ([, v]) => v.type === item.type && v.provider === item.provider
    );

    if (!shortcut) continue;

    const integration = INTEGRATIONS_MAP[shortcut[0]];
    process.stderr.write(`  ${sym.info} Opening browser to connect ${item.display_name}...\n`);

    try {
      // Snapshot existing connection IDs
      let existingIds = new Set<string | number>();
      for (const conn of item.connections) {
        existingIds.add(conn.id);
      }

      const result = await client.post<{ auth_url: string }>(
        `/v1/integrations/${encodeURIComponent(integration.type)}/${encodeURIComponent(integration.provider)}/connect`
      );

      if (!result.auth_url) {
        process.stderr.write(`  ${sym.error} No auth URL returned for ${item.display_name}\n`);
        continue;
      }

      openBrowser(result.auth_url);

      // Poll for connection
      process.stderr.write(`  ${style.dim("Waiting for OAuth completion...")}  ${exitHint}\n`);
      const connected = await pollForSetupConnection(client, integration.type, integration.provider, existingIds);
      if (connected) {
        process.stderr.write(`  ${sym.success} ${item.display_name} connected!\n\n`);
      } else {
        process.stderr.write(`  ${sym.warning} ${item.display_name} timed out — connect later with: nex integrate connect ${shortcut[0]}\n\n`);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      process.stderr.write(`  ${sym.error} Failed to connect ${item.display_name}: ${msg}\n`);
    }
  }
}

async function pollForSetupConnection(
  client: NexClient,
  type: string,
  provider: string,
  existingIds: Set<string | number>,
): Promise<boolean> {
  const maxWaitMs = 3 * 60 * 1000; // 3 minutes (shorter than integrate command)
  const pollIntervalMs = 3000;
  const startTime = Date.now();

  while (Date.now() - startTime < maxWaitMs) {
    await new Promise((resolve) => setTimeout(resolve, pollIntervalMs));

    try {
      const integrations = await client.get<IntegrationEntry[]>("/v1/integrations/", 5_000);
      if (!Array.isArray(integrations)) continue;

      for (const entry of integrations) {
        if (entry.type === type && entry.provider === provider && Array.isArray(entry.connections)) {
          for (const conn of entry.connections) {
            if (!existingIds.has(conn.id)) return true; // New connection found
          }
          // Reconnect scenario: same ID but refreshed
          if (entry.connections.length > 0 && existingIds.size === 0) return true;
        }
      }
    } catch {
      // Continue polling
    }
  }
  return false;
}

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
  .option("--no-plugin", "Skip plugin (hooks/commands), only update config files")
  .option("--no-hooks", "Skip hook installation for all platforms")
  .option("--no-rules", "Skip rules/instruction file installation")
  .option("--no-integrations", "Skip integration connection step")
  .option("--no-scan", "Skip file scanning during setup")
  .action(async (cmdOpts) => {
    const globalOpts = program.opts();
    const format = resolveFormat(globalOpts.format) as Format;
    await runSetup({
      platform: cmdOpts.platform,
      noPlugin: cmdOpts.plugin === false,
      noHooks: cmdOpts.hooks === false,
      noRules: cmdOpts.rules === false,
      noIntegrations: cmdOpts.integrations === false,
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
