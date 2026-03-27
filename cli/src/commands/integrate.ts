/**
 * Integration commands: list, connect, disconnect.
 */

import { spawn } from "node:child_process";
import { program } from "../cli.js";
import { NexClient } from "../lib/client.js";
import { resolveApiKey, resolveFormat, resolveTimeout } from "../lib/config.js";
import { AuthError } from "../lib/errors.js";
import { printOutput, printError } from "../lib/output.js";
import type { Format } from "../lib/output.js";
import { style, sym, badge, isTTY, exitHint } from "../lib/tui.js";

function getClient(): { client: NexClient; format: Format } {
  const opts = program.opts();
  const client = new NexClient(resolveApiKey(opts.apiKey), resolveTimeout(opts.timeout));
  return { client, format: resolveFormat(opts.format) as Format };
}

const INTEGRATIONS: Record<string, { type: string; provider: string }> = {
  gmail: { type: "email", provider: "google" },
  "google-calendar": { type: "calendar", provider: "google" },
  outlook: { type: "email", provider: "microsoft" },
  "outlook-calendar": { type: "calendar", provider: "microsoft" },
  slack: { type: "messaging", provider: "slack" },
  salesforce: { type: "crm", provider: "salesforce" },
  hubspot: { type: "crm", provider: "hubspot" },
  attio: { type: "crm", provider: "attio" },
};

const INTEGRATION_NAMES = Object.keys(INTEGRATIONS).join(", ");

export function openBrowser(url: string): void {
  try {
    let cmd: string;
    let args: string[];
    if (process.platform === "darwin") {
      cmd = "open";
      args = [url];
    } else if (process.platform === "linux") {
      cmd = "xdg-open";
      args = [url];
    } else if (process.platform === "win32") {
      cmd = "cmd";
      args = ["/c", "start", "", url];
    } else {
      throw new Error("Unsupported platform");
    }
    spawn(cmd, args, { stdio: "ignore", detached: true }).unref();
  } catch {
    process.stderr.write(`Open this URL in your browser:\n${url}\n\n`);
  }
}

interface IntegrationEntry {
  type?: string;
  provider?: string;
  connections?: Array<{ id?: string | number; [key: string]: unknown }>;
  [key: string]: unknown;
}

function getConnections(integrations: IntegrationEntry[], type: string, provider: string): Array<{ id: string | number }> {
  for (const entry of integrations) {
    if (entry.type === type && entry.provider === provider && Array.isArray(entry.connections)) {
      return entry.connections.filter((c) => c.id !== undefined && c.id !== null) as Array<{ id: string | number }>;
    }
  }
  return [];
}

async function pollForConnection(
  client: NexClient,
  type: string,
  provider: string,
  existingIds: Set<string | number>,
  format: Format,
): Promise<void> {
  process.stderr.write(`\n${sym.info} Waiting for OAuth completion...  ${exitHint}\n`);
  process.stderr.write(`  ${style.dim("Complete the OAuth flow in your browser, then return here.")}\n\n`);

  const maxWaitMs = 5 * 60 * 1000;
  const pollIntervalMs = 3000;
  const startTime = Date.now();
  let dots = 0;
  let pollCount = 0;

  while (Date.now() - startTime < maxWaitMs) {
    await new Promise((resolve) => setTimeout(resolve, pollIntervalMs));
    pollCount++;

    try {
      const integrations = await client.get<IntegrationEntry[]>("/v1/integrations/", 5_000);

      if (!Array.isArray(integrations)) continue;

      const connections = getConnections(integrations, type, provider);

      // Check for new connection ID
      for (const conn of connections) {
        if (!existingIds.has(conn.id)) {
          process.stderr.write(`\n\n${sym.success} Connected successfully!\n`);
          printOutput({ status: "connected", connection_id: conn.id }, format);
          return;
        }
      }

      // If we had no connections before but now we do, or if after several polls
      // the connection count matches (reconnect/refresh scenario), report success
      if (connections.length > 0 && (existingIds.size === 0 || pollCount >= 3)) {
        // Connection exists — OAuth likely refreshed an existing connection (same ID)
        process.stderr.write(`\n\n${sym.success} Connected successfully!\n`);
        printOutput({ status: "connected", connection_id: connections[0].id }, format);
        return;
      }

      dots = (dots + 1) % 4;
      process.stderr.write(`\rPolling${".".repeat(dots)}${" ".repeat(3 - dots)}`);
    } catch (err) {
      if (err instanceof AuthError) throw err;
      dots = (dots + 1) % 4;
      const msg = err instanceof Error ? err.message : String(err);
      process.stderr.write(`\rPolling${".".repeat(dots)}${" ".repeat(3 - dots)}  (${msg.slice(0, 40)})`);
    }
  }

  printError("Timed out after 5 minutes. Check status with 'wuphf integrate list'.");
  process.exit(1);
}

/** Calendar integrations include the WUPHF Meeting Bot annotation. */
const CALENDAR_TYPES = new Set(["calendar"]);

function displayName(item: FullIntegrationEntry): string {
  if (CALENDAR_TYPES.has(item.type)) {
    return `${item.display_name} (WUPHF Meeting Bot)`;
  }
  return item.display_name;
}

const integrate = program
  .command("integrate")
  .description("Manage third-party integrations (Gmail, Slack, Salesforce, etc.)");

interface FullIntegrationEntry {
  type: string;
  provider: string;
  display_name: string;
  description: string;
  connections: Array<{ id: string | number; status: string; identifier: string }>;
}

interface ActionItem {
  label: string;
  action: "connect" | "disconnect" | "reconnect";
  connectionId?: string | number;
}

function getActions(item: FullIntegrationEntry): ActionItem[] {
  const connected = item.connections.length > 0;
  if (connected) {
    const actions: ActionItem[] = [];
    for (const conn of item.connections) {
      actions.push({ label: `Disconnect ${conn.identifier}`, action: "disconnect", connectionId: conn.id });
    }
    actions.push({ label: "Reconnect (re-auth)", action: "reconnect" });
    return actions;
  }
  return [{ label: "Connect", action: "connect" }];
}

function renderList(items: FullIntegrationEntry[], selected: number, expanded: boolean, actionIndex: number): string {
  const lines: string[] = [];

  lines.push(`${style.bold("Integrations")}  ${style.dim("(↑↓ navigate, enter expand, ←→ actions)")}  ${exitHint}`);
  lines.push("");

  for (let i = 0; i < items.length; i++) {
    const item = items[i];
    const isSelected = i === selected;
    const pointer = isSelected ? sym.pointer : " ";
    const connected = item.connections.length > 0;
    const status = connected ? badge("connected", "success") : badge("not connected", "dim");
    const dname = displayName(item);
    const name = isSelected ? style.bold(dname) : dname;

    lines.push(`  ${pointer} ${name}  ${status}`);

    if (isSelected && expanded) {
      lines.push(`    ${style.dim(item.description)}`);
      if (CALENDAR_TYPES.has(item.type)) {
        lines.push(`    ${style.dim("Joins calls on Google Meet, Zoom, Webex, Teams, etc. Transcripts are processed into your context graph.")}`);
      }
      if (connected) {
        for (const conn of item.connections) {
          lines.push(`    ${style.green("\u2514")} ${conn.identifier}  ${style.dim(`(ID: ${conn.id}, status: ${conn.status})`)}`);
        }
      }
      // Render action items
      const actions = getActions(item);
      lines.push("");
      for (let a = 0; a < actions.length; a++) {
        const isActionSelected = a === actionIndex;
        const actionPointer = isActionSelected ? style.cyan(sym.pointer) : " ";
        const actionLabel = isActionSelected ? style.bold(actions[a].label) : style.dim(actions[a].label);
        lines.push(`    ${actionPointer} ${actionLabel}`);
      }
    }
  }

  lines.push("");
  return lines.join("\n");
}

function interactiveList(items: FullIntegrationEntry[], client: NexClient, format: Format): Promise<void> {
  return new Promise((resolve) => {
    let selected = 0;
    let expanded = false;
    let actionIndex = 0;

    const draw = () => {
      process.stdout.write("\x1b[2J\x1b[H");
      process.stdout.write(renderList(items, selected, expanded, actionIndex));
    };

    if (!process.stdin.isTTY) {
      for (const item of items) {
        const connected = item.connections.length > 0;
        const status = connected ? badge("connected", "success") : badge("not connected", "dim");
        process.stdout.write(`${displayName(item)}  ${status}\n`);
        if (connected) {
          for (const conn of item.connections) {
            process.stdout.write(`  \u2514 ${conn.identifier} (ID: ${conn.id})\n`);
          }
        }
      }
      resolve();
      return;
    }

    const cleanup = () => {
      process.stdin.setRawMode(false);
      process.stdin.removeListener("data", onData);
      process.stdin.pause();
    };

    const executeAction = async (item: FullIntegrationEntry, action: ActionItem) => {
      cleanup();
      process.stdout.write("\x1b[2J\x1b[H");

      const shortcut = Object.entries(INTEGRATIONS).find(
        ([, v]) => v.type === item.type && v.provider === item.provider
      );

      if (action.action === "connect" || action.action === "reconnect") {
        if (!shortcut) {
          printError(`No integration mapping for ${item.display_name}`);
          resolve();
          return;
        }
        const integration = INTEGRATIONS[shortcut[0]];

        // Snapshot existing connections
        let existingIds = new Set<string | number>();
        try {
          const integrations = await client.get<IntegrationEntry[]>("/v1/integrations/", 5_000);
          if (Array.isArray(integrations)) {
            existingIds = new Set(getConnections(integrations, integration.type, integration.provider).map((c) => c.id));
          }
        } catch { /* continue */ }

        process.stderr.write(`${sym.info} Opening browser to connect ${item.display_name}...\n`);
        const result = await client.post<{ auth_url: string; connect_id?: string }>(
          `/v1/integrations/${encodeURIComponent(integration.type)}/${encodeURIComponent(integration.provider)}/connect`
        );

        if (!result.auth_url) {
          printError("No auth URL returned from API");
          resolve();
          return;
        }

        openBrowser(result.auth_url);
        await pollForConnection(client, integration.type, integration.provider, existingIds, format);
        resolve();
      } else if (action.action === "disconnect" && action.connectionId !== undefined) {
        try {
          await client.delete(`/v1/integrations/connections/${encodeURIComponent(action.connectionId)}`);
          // Optimistic local update — remove connection from in-memory list
          // so the UI reflects the change immediately (avoids read-replica lag)
          item.connections = item.connections.filter((c) => c.id !== action.connectionId);
          process.stdin.setRawMode(true);
          process.stdin.resume();
          process.stdin.on("data", onData);
          expanded = true;
          actionIndex = 0;
          draw();
          return; // Stay in interactive list
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err);
          printError(`Failed to disconnect: ${msg}`);
        }
        resolve();
      }
    };

    process.stdin.setRawMode(true);
    process.stdin.resume();
    process.stdin.setEncoding("utf-8");

    draw();

    const onData = (key: string) => {
      if (key === "q" || key === "\x03") {
        cleanup();
        process.stdout.write("\x1b[2J\x1b[H");
        resolve();
        return;
      }

      if (key === "\x1b[A") {
        // Up arrow
        if (expanded) {
          actionIndex = Math.max(0, actionIndex - 1);
        } else {
          selected = Math.max(0, selected - 1);
        }
      } else if (key === "\x1b[B") {
        // Down arrow
        if (expanded) {
          const actions = getActions(items[selected]);
          actionIndex = Math.min(actions.length - 1, actionIndex + 1);
        } else {
          selected = Math.min(items.length - 1, selected + 1);
        }
      } else if (key === "\r") {
        // Enter
        if (expanded) {
          const actions = getActions(items[selected]);
          if (actions[actionIndex]) {
            executeAction(items[selected], actions[actionIndex]);
            return; // Don't redraw — executeAction resolves the promise
          }
        } else {
          expanded = true;
          actionIndex = 0;
        }
      } else if (key === "\x1b[D" || key === "\x1b[C") {
        // Left/Right arrow — toggle expand
        if (expanded) {
          expanded = false;
          actionIndex = 0;
        } else {
          expanded = true;
          actionIndex = 0;
        }
      }

      draw();
    };

    process.stdin.on("data", onData);
  });
}

integrate
  .command("list")
  .description("List all available integrations and their connection status")
  .action(async () => {
    const { client, format } = getClient();
    const result = await client.get<FullIntegrationEntry[]>("/v1/integrations/");

    if (format === "json") {
      printOutput(result, "json");
      return;
    }

    if (!Array.isArray(result) || result.length === 0) {
      process.stdout.write("No integrations available.\n");
      return;
    }

    await interactiveList(result, client, format);
  });

integrate
  .command("connect")
  .description(`Connect an integration: ${INTEGRATION_NAMES}`)
  .argument("<name>", `Integration name`)
  .action(async (name: string) => {
    const integration = INTEGRATIONS[name.toLowerCase()];

    if (!integration) {
      printError(`Unknown integration "${name}". Available: ${INTEGRATION_NAMES}`);
      process.exit(1);
    }

    const { client, format } = getClient();

    // Snapshot existing connections before OAuth
    let existingIds = new Set<string | number>();
    try {
      const integrations = await client.get<IntegrationEntry[]>("/v1/integrations/", 5_000);
      if (Array.isArray(integrations)) {
        existingIds = new Set(getConnections(integrations, integration.type, integration.provider).map((c) => c.id));
      }
    } catch {
      // Continue — we'll just not be able to detect duplicates
    }

    const result = await client.post<{ auth_url: string; connect_id?: string }>(
      `/v1/integrations/${encodeURIComponent(integration.type)}/${encodeURIComponent(integration.provider)}/connect`
    );

    if (!result.auth_url) {
      throw new Error("No auth URL returned from API");
    }

    openBrowser(result.auth_url);
    await pollForConnection(client, integration.type, integration.provider, existingIds, format);
  });

integrate
  .command("disconnect")
  .description("Disconnect an integration")
  .argument("<connection_id>", "Connection ID to disconnect")
  .action(async (connectionId: string) => {
    const { client, format } = getClient();
    const result = await client.delete(`/v1/integrations/connections/${encodeURIComponent(connectionId)}`);
    printOutput(result, format);
    process.stderr.write(`${sym.success} Disconnected successfully.\n`);
  });
