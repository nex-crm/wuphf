/**
 * nex insight — list insights from your Nex workspace.
 */

import { program } from "../cli.js";
import { NexClient } from "../lib/client.js";
import { resolveApiKey, resolveFormat, resolveTimeout } from "../lib/config.js";
import { printOutput } from "../lib/output.js";
import type { Format } from "../lib/output.js";

function getClient(): { client: NexClient; format: Format } {
  const opts = program.opts();
  const client = new NexClient(resolveApiKey(opts.apiKey), resolveTimeout(opts.timeout));
  return { client, format: resolveFormat(opts.format) as Format };
}

const insightCmd = program
  .command("insight")
  .description("Manage insights");

insightCmd
  .command("list")
  .description("List insights")
  .option("--last <duration>", "Time duration (e.g. 7d, 24h)")
  .option("--from <date>", "Start date (ISO 8601)")
  .option("--to <date>", "End date (ISO 8601)")
  .option("--limit <n>", "Maximum number of results")
  .action(async (opts: { last?: string; from?: string; to?: string; limit?: string }) => {
    const { client, format } = getClient();

    const params = new URLSearchParams();
    if (opts.last) params.set("last", opts.last);
    if (opts.from) params.set("from", opts.from);
    if (opts.to) params.set("to", opts.to);
    if (opts.limit) params.set("limit", opts.limit);

    const qs = params.toString();
    const path = `/v1/insights${qs ? `?${qs}` : ""}`;

    const result = await client.get(path);
    printOutput(result, format);
  });
