/**
 * nex record — CRUD operations for records.
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

const rec = program.command("record").description("Manage records");

rec
  .command("list")
  .description("List records for an object")
  .argument("<object-slug>", "Object slug")
  .option("--limit <n>", "Max records to return")
  .option("--offset <n>", "Offset for pagination")
  .option("--attributes <level>", "Attribute detail: all, primary, none", "primary")
  .option("--sort <sort>", "Sort as attr:asc or attr:desc")
  .action(async (objectSlug: string, opts: { limit?: string; offset?: string; attributes?: string; sort?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = { attributes: opts.attributes ?? "primary" };
    if (opts.limit) body.limit = parseInt(opts.limit, 10);
    if (opts.offset) body.offset = parseInt(opts.offset, 10);
    if (opts.sort) {
      const [attribute, direction] = opts.sort.split(":");
      body.sort = { attribute, direction };
    }
    const result = await client.post(`/v1/objects/${encodeURIComponent(objectSlug)}/records`, body);
    printOutput(result, format);
  });

rec
  .command("get")
  .description("Get a record by ID")
  .argument("<id>", "Record ID")
  .action(async (id: string) => {
    const { client, format } = getClient();
    const result = await client.get(`/v1/records/${encodeURIComponent(id)}`);
    printOutput(result, format);
  });

rec
  .command("create")
  .description("Create a record")
  .argument("<object-slug>", "Object slug")
  .requiredOption("--data <json>", "Attributes as JSON string")
  .action(async (objectSlug: string, opts: { data: string }) => {
    const { client, format } = getClient();
    let attributes: unknown;
    try { attributes = JSON.parse(opts.data); }
    catch { throw new Error(`Invalid JSON for --data: ${opts.data}`); }
    const result = await client.post(`/v1/objects/${encodeURIComponent(objectSlug)}`, { attributes });
    printOutput(result, format);
  });

rec
  .command("upsert")
  .description("Upsert a record")
  .argument("<object-slug>", "Object slug")
  .requiredOption("--match <attr>", "Matching attribute for upsert")
  .requiredOption("--data <json>", "Attributes as JSON string")
  .action(async (objectSlug: string, opts: { match: string; data: string }) => {
    const { client, format } = getClient();
    let attributes: unknown;
    try { attributes = JSON.parse(opts.data); }
    catch { throw new Error(`Invalid JSON for --data: ${opts.data}`); }
    const result = await client.put(`/v1/objects/${encodeURIComponent(objectSlug)}`, {
      matching_attribute: opts.match,
      attributes,
    });
    printOutput(result, format);
  });

rec
  .command("update")
  .description("Update a record")
  .argument("<id>", "Record ID")
  .requiredOption("--data <json>", "Attributes as JSON string")
  .action(async (id: string, opts: { data: string }) => {
    const { client, format } = getClient();
    let attributes: unknown;
    try { attributes = JSON.parse(opts.data); }
    catch { throw new Error(`Invalid JSON for --data: ${opts.data}`); }
    const result = await client.patch(`/v1/records/${encodeURIComponent(id)}`, { attributes });
    printOutput(result, format);
  });

rec
  .command("delete")
  .description("Delete a record")
  .argument("<id>", "Record ID")
  .action(async (id: string) => {
    const { client, format } = getClient();
    const result = await client.delete(`/v1/records/${encodeURIComponent(id)}`);
    printOutput(result, format);
  });

rec
  .command("timeline")
  .description("Get record timeline")
  .argument("<id>", "Record ID")
  .option("--limit <n>", "Max entries")
  .option("--cursor <cursor>", "Pagination cursor")
  .action(async (id: string, opts: { limit?: string; cursor?: string }) => {
    const { client, format } = getClient();
    const params = new URLSearchParams();
    if (opts.limit) params.set("limit", opts.limit);
    if (opts.cursor) params.set("cursor", opts.cursor);
    const qs = params.toString();
    const result = await client.get(`/v1/records/${encodeURIComponent(id)}/timeline${qs ? `?${qs}` : ""}`);
    printOutput(result, format);
  });
