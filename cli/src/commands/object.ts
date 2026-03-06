/**
 * nex object — CRUD operations for object definitions.
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

const obj = program.command("object").description("Manage object definitions");

obj
  .command("list")
  .description("List all objects")
  .option("--include-attributes", "Include attributes in response")
  .action(async (opts: { includeAttributes?: boolean }) => {
    const { client, format } = getClient();
    const params = opts.includeAttributes ? "?include_attributes=true" : "";
    const result = await client.get(`/v1/objects${params}`);
    printOutput(result, format);
  });

obj
  .command("get")
  .description("Get an object by slug")
  .argument("<slug>", "Object slug")
  .action(async (slug: string) => {
    const { client, format } = getClient();
    const result = await client.get(`/v1/objects/${encodeURIComponent(slug)}`);
    printOutput(result, format);
  });

obj
  .command("create")
  .description("Create a new object")
  .requiredOption("--name <name>", "Object name")
  .requiredOption("--slug <slug>", "Object slug")
  .option("--type <type>", "Object type: person, company, custom, deal")
  .option("--description <description>", "Object description")
  .action(async (opts: { name: string; slug: string; type?: string; description?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = { name: opts.name, slug: opts.slug };
    if (opts.type) body.type = opts.type;
    if (opts.description) body.description = opts.description;
    const result = await client.post("/v1/objects", body);
    printOutput(result, format);
  });

obj
  .command("update")
  .description("Update an object")
  .argument("<slug>", "Object slug")
  .option("--name <name>", "New name")
  .option("--description <description>", "New description")
  .option("--name-plural <plural>", "New plural name")
  .action(async (slug: string, opts: { name?: string; description?: string; namePlural?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = {};
    if (opts.name !== undefined) body.name = opts.name;
    if (opts.namePlural !== undefined) body.name_plural = opts.namePlural;
    if (opts.description !== undefined) body.description = opts.description;
    const result = await client.patch(`/v1/objects/${encodeURIComponent(slug)}`, body);
    printOutput(result, format);
  });

obj
  .command("delete")
  .description("Delete an object")
  .argument("<slug>", "Object slug")
  .action(async (slug: string) => {
    const { client, format } = getClient();
    const result = await client.delete(`/v1/objects/${encodeURIComponent(slug)}`);
    printOutput(result, format);
  });
