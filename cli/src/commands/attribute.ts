/**
 * wuphf attribute — CRUD operations for object attributes.
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

const attr = program.command("attribute").description("Manage object attributes");

attr
  .command("create")
  .description("Create an attribute on an object")
  .argument("<object-slug>", "Object slug")
  .requiredOption("--name <name>", "Attribute name")
  .requiredOption("--slug <slug>", "Attribute slug")
  .requiredOption("--type <type>", "Attribute type")
  .option("--description <description>", "Attribute description")
  .option("--options <json>", "Options as JSON string")
  .action(async (objectSlug: string, opts: { name: string; slug: string; type: string; description?: string; options?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = { name: opts.name, slug: opts.slug, type: opts.type };
    if (opts.description !== undefined) body.description = opts.description;
    if (opts.options) {
      try { body.options = JSON.parse(opts.options); }
      catch { throw new Error(`Invalid JSON for --options: ${opts.options}`); }
    }
    const result = await client.post(`/v1/objects/${encodeURIComponent(objectSlug)}/attributes`, body);
    printOutput(result, format);
  });

attr
  .command("update")
  .description("Update an attribute")
  .argument("<object-slug>", "Object slug")
  .argument("<attr-id>", "Attribute ID")
  .option("--name <name>", "New name")
  .option("--description <description>", "New description")
  .option("--options <json>", "Options as JSON string")
  .action(async (objectSlug: string, attrId: string, opts: { name?: string; description?: string; options?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = {};
    if (opts.name !== undefined) body.name = opts.name;
    if (opts.description !== undefined) body.description = opts.description;
    if (opts.options) {
      try { body.options = JSON.parse(opts.options); }
      catch { throw new Error(`Invalid JSON for --options: ${opts.options}`); }
    }
    const result = await client.patch(`/v1/objects/${encodeURIComponent(objectSlug)}/attributes/${encodeURIComponent(attrId)}`, body);
    printOutput(result, format);
  });

attr
  .command("delete")
  .description("Delete an attribute")
  .argument("<object-slug>", "Object slug")
  .argument("<attr-id>", "Attribute ID")
  .action(async (objectSlug: string, attrId: string) => {
    const { client, format } = getClient();
    const result = await client.delete(`/v1/objects/${encodeURIComponent(objectSlug)}/attributes/${encodeURIComponent(attrId)}`);
    printOutput(result, format);
  });
