/**
 * wuphf graph — Visualize the workspace entity graph in the browser.
 * Fetches server-rendered HTML visualization directly from the API.
 */

import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { spawn } from "node:child_process";
import { program } from "../cli.js";
import { NexClient } from "../lib/client.js";
import { resolveApiKey, resolveFormat, resolveTimeout } from "../lib/config.js";
import { printOutput, printError } from "../lib/output.js";
import type { Format } from "../lib/output.js";
import { sym, style } from "../lib/tui.js";

function getClient(): { client: NexClient; format: Format } {
  const opts = program.opts();
  const client = new NexClient(resolveApiKey(opts.apiKey), resolveTimeout(opts.timeout));
  return { client, format: resolveFormat(opts.format) as Format };
}

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

program
  .command("graph")
  .description("Visualize the workspace entity graph in the browser")
  .option("--out <file>", "Save HTML to a specific file path")
  .option("--limit <n>", "Maximum number of entities to fetch (default: 1000)", "1000")
  .option("--no-open", "Generate the file without opening the browser")
  .action(async (opts: { out?: string; limit: string; open: boolean }) => {
    const { client, format } = getClient();
    const limit = parseInt(opts.limit, 10) || 1000;

    process.stderr.write(`${sym.info} Fetching workspace graph...\n`);

    let html: string;
    try {
      html = await client.getRaw(`/v1/graph?limit=${limit}&format=html`);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      printError(`Failed to fetch graph: ${message}`);
      process.exit(1);
    }

    const outPath =
      opts.out ?? join(tmpdir(), `wuphf-graph-${Date.now()}.html`);
    writeFileSync(outPath, html, "utf-8");

    if (format === "json") {
      printOutput({ path: outPath }, "json");
      return;
    }

    if (opts.open) {
      openBrowser(`file://${outPath}`);
      process.stderr.write(
        `${sym.success} Graph opened in browser\n`,
      );
    } else {
      process.stderr.write(
        `${sym.success} Graph saved to ${outPath}\n`,
      );
    }
  });
