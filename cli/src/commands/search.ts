/**
 * nex search — fuzzy keyword search across CRM records by name.
 */

import { program } from "../cli.js";
import { NexClient } from "../lib/client.js";
import { resolveApiKey, resolveFormat, resolveTimeout } from "../lib/config.js";
import { printOutput } from "../lib/output.js";
import type { Format } from "../lib/output.js";
import { heading, table, style, sym } from "../lib/tui.js";

function getClient(): { client: NexClient; format: Format } {
  const opts = program.opts();
  const client = new NexClient(resolveApiKey(opts.apiKey), resolveTimeout(opts.timeout));
  return { client, format: resolveFormat(opts.format) as Format };
}

interface SearchResult {
  results?: Array<{ name?: string; type?: string; id?: string | number; [k: string]: unknown }>;
  errored_search_types?: string[];
  [k: string]: unknown;
}

function formatSearchTTY(data: unknown): string | undefined {
  const d = data as SearchResult;
  if (!d || typeof d !== "object") return undefined;

  const lines: string[] = [];

  // Check for partial errors
  if (d.errored_search_types && d.errored_search_types.length > 0) {
    lines.push(`  ${sym.warning} Search partially failed for: ${d.errored_search_types.join(", ")}`);
    lines.push("");
  }

  const results = d.results;
  if (!results || !Array.isArray(results) || results.length === 0) {
    lines.push(`  ${style.dim("No records found.")}`);
    lines.push(`  ${style.dim("Tip: use 'nex ask <query>' for AI-powered context lookups.")}`);
    return lines.join("\n");
  }

  // Build table
  const headers = ["NAME", "TYPE", "ID"];
  const rows = results.map((r) => [
    String(r.name ?? ""),
    String(r.type ?? ""),
    String(r.id ?? ""),
  ]);

  lines.push(table({ headers, rows }));
  lines.push("");
  lines.push(`  ${style.dim(`${results.length} result${results.length !== 1 ? "s" : ""} found`)}`);

  return lines.join("\n");
}

program
  .command("search")
  .description("Fuzzy keyword search across CRM records by name (use 'ask' for AI-powered queries)")
  .argument("<query>", "Search query")
  .action(async (query: string) => {
    const { client, format } = getClient();
    try {
      const result = await client.post("/v1/search", { query });
      if (format === "text") {
        process.stdout.write(`${heading(`Search: "${query}"`)}\n\n`);
      }
      printOutput(result, format, formatSearchTTY);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      process.stderr.write(`\n${sym.error} Search failed: ${message}\n`);
      process.stderr.write(`  ${style.dim("Tip: use 'nex ask <query>' for AI-powered context lookups.")}\n\n`);
      process.exit(1);
    }
  });
