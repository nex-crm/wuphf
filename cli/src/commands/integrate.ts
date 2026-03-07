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

    // Human-readable text output
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
    lines.push("Connect:     nex integrate connect <type> <provider>");
    lines.push("Disconnect:  nex integrate disconnect <id>");

    process.stdout.write(lines.join("\n") + "\n");
  });

integrate
  .command("connect")
  .description("Connect a third-party integration via OAuth")
  .argument("<type>", "Integration type: email, calendar, crm, messaging")
  .argument("<provider>", "Provider: google, microsoft, attio, slack, salesforce, hubspot")
  .action(async (type: string, provider: string) => {
    const { client, format } = getClient();

    const result = await client.post<{ auth_url: string; connect_id: string }>(
      `/v1/integrations/${encodeURIComponent(type)}/${encodeURIComponent(provider)}/connect`
    );

    if (!result.auth_url) {
      throw new Error("No auth URL returned from API");
    }

    // Open browser using spawn (no shell interpolation — safe from injection)
    const url = result.auth_url;
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

    process.stderr.write("Waiting for OAuth completion...\n");

    // Poll for status
    const connectId = result.connect_id;
    const maxWaitMs = 5 * 60 * 1000; // 5 minutes
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
        // Fail fast on non-transient errors
        if (err instanceof AuthError) throw err;
        if (err instanceof ServerError && (err.status === 410 || err.status === 403)) throw err;
        // Continue polling on transient errors
        if (program.opts().debug) {
          process.stderr.write(`Poll error: ${err}\n`);
        }
      }
    }

    printError("Timed out waiting for OAuth completion. You can check status with 'nex integrate list'.");
    process.exit(1);
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
