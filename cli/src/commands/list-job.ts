/**
 * nex list-job — AI-powered list generation jobs.
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

const listJob = program.command("list-job").description("Manage list generation jobs");

listJob
  .command("create")
  .description("Create a list generation job")
  .argument("<query>", "Natural language query")
  .option("--type <type>", "Object type: contact, company")
  .option("--limit <n>", "Max results")
  .action(async (query: string, opts: { type?: string; limit?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = { query };
    if (opts.type) body.object_type = opts.type;
    if (opts.limit) body.limit = parseInt(opts.limit, 10);
    const result = await client.post("/v1/context/list/jobs", body);
    printOutput(result, format);
  });

listJob
  .command("status")
  .description("Get job status")
  .argument("<id>", "Job ID")
  .option("--include-attributes", "Include attributes in response")
  .action(async (id: string, opts: { includeAttributes?: boolean }) => {
    const { client, format } = getClient();
    const params = opts.includeAttributes ? "?include_attributes=true" : "";
    const result = await client.get(`/v1/context/list/jobs/${encodeURIComponent(id)}${params}`);
    printOutput(result, format);
  });
