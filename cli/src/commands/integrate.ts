/**
 * Integration commands: list, connect, disconnect.
 */

import { spawn } from "node:child_process";
import { program } from "../cli.js";
import { NexClient } from "../lib/client.js";
import { resolveApiKey, resolveFormat, resolveTimeout } from "../lib/config.js";
import { AuthError, ServerError } from "../lib/errors.js";
import { printOutput, printError } from "../lib/output.js";
import type { Format } from "../lib/output.js";

function padRight(str: string, len: number): string {
  return str.length >= len ? str : str + " ".repeat(len - str.length);
}

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

function openBrowser(url: string): void {
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

async function pollForConnection(client: NexClient, connectId: string, format: Format): Promise<void> {
  process.stderr.write("Waiting for OAuth completion...\n");

  const maxWaitMs = 5 * 60 * 1000;
  const pollIntervalMs = 2000;
  const startTime = Date.now();

  while (Date.now() - startTime < maxWaitMs) {
    await new Promise((resolve) => setTimeout(resolve, pollIntervalMs));

    try {
      const status = await client.get<{ status: string; connection_id?: number }>(
        `/v1/integrations/connect/${encodeURIComponent(connectId)}/status`
      );

      if (status.status === "connected") {
        process.stderr.write(`\nConnected successfully!\n`);
        printOutput(status, format);
        return;
      }
    } catch (err) {
      if (err instanceof AuthError) throw err;
      if (err instanceof ServerError && (err.status === 410 || err.status === 403)) throw err;
    }
  }

  printError("Timed out waiting for OAuth completion. Check status with 'nex integrate list'.");
  process.exit(1);
}

const integrate = program
  .command("integrate")
  .description("Manage third-party integrations (Gmail, Slack, Salesforce, etc.)");

integrate
  .command("list")
  .description("List all available integrations and their connection status")
  .action(async () => {
    const { client, format } = getClient();
    const result = await client.get<Record<string, unknown>[]>("/v1/integrations/");

    if (format === "json") {
      printOutput(result, "json");
      return;
    }

    if (!Array.isArray(result) || result.length === 0) {
      process.stdout.write("No integrations available.\n");
      return;
    }

    const lines: string[] = [];
    lines.push("Integrations");
    lines.push("\u2500".repeat(50));

    for (const integration of result) {
      const type = String(integration.type ?? "");
      const provider = String(integration.provider ?? "");
      const label = `${type} / ${provider}`;
      const connections = integration.connections as Array<Record<string, unknown>> | undefined;

      if (connections && connections.length > 0) {
        for (const conn of connections) {
          const displayName = conn.display_name ?? conn.email ?? "";
          lines.push(
            `${padRight(label, 25)} \u25CF connected     ${displayName}     (ID: ${conn.id})`
          );
        }
      } else {
        lines.push(`${padRight(label, 25)} \u25CB not connected`);
      }
    }

    lines.push("");
    lines.push(`Connect:     nex integrate connect <name>  (${INTEGRATION_NAMES})`);
    lines.push("Disconnect:  nex integrate disconnect <id>");

    process.stdout.write(lines.join("\n") + "\n");
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
    const result = await client.post<{ auth_url: string; connect_id: string }>(
      `/v1/integrations/${encodeURIComponent(integration.type)}/${encodeURIComponent(integration.provider)}/connect`
    );

    if (!result.auth_url) {
      throw new Error("No auth URL returned from API");
    }

    openBrowser(result.auth_url);
    await pollForConnection(client, result.connect_id, format);
  });

integrate
  .command("disconnect")
  .description("Disconnect an integration")
  .argument("<connection_id>", "Connection ID to disconnect")
  .action(async (connectionId: string) => {
    const { client, format } = getClient();
    const result = await client.delete(`/v1/integrations/connections/${encodeURIComponent(connectionId)}`);
    printOutput(result, format);
    process.stderr.write("Disconnected successfully.\n");
  });
