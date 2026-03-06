/**
 * nex search — search across your Nex context.
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

program
  .command("search")
  .description("Search across your Nex context")
  .argument("<query>", "Search query")
  .action(async (query: string) => {
    const { client, format } = getClient();
    const result = await client.post("/v1/search", { query });
    printOutput(result, format);
  });
