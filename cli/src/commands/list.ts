/**
 * nex list — CRUD operations for lists and list records.
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

const list = program.command("list").description("Manage lists");

list
  .command("list")
  .description("List all lists for an object")
  .argument("<object-slug>", "Object slug")
  .option("--include-attributes", "Include attributes in response")
  .action(async (objectSlug: string, opts: { includeAttributes?: boolean }) => {
    const { client, format } = getClient();
    const params = opts.includeAttributes ? "?include_attributes=true" : "";
    const result = await client.get(`/v1/objects/${encodeURIComponent(objectSlug)}/lists${params}`);
    printOutput(result, format);
  });

list
  .command("get")
  .description("Get a list by ID")
  .argument("<id>", "List ID")
  .action(async (id: string) => {
    const { client, format } = getClient();
    const result = await client.get(`/v1/lists/${encodeURIComponent(id)}`);
    printOutput(result, format);
  });

list
  .command("create")
  .description("Create a new list")
  .argument("<object-slug>", "Object slug")
  .requiredOption("--name <name>", "List name")
  .requiredOption("--slug <slug>", "List slug")
  .option("--description <description>", "List description")
  .action(async (objectSlug: string, opts: { name: string; slug: string; description?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = { name: opts.name, slug: opts.slug };
    if (opts.description) body.description = opts.description;
    const result = await client.post(`/v1/objects/${encodeURIComponent(objectSlug)}/lists`, body);
    printOutput(result, format);
  });

list
  .command("delete")
  .description("Delete a list")
  .argument("<id>", "List ID")
  .action(async (id: string) => {
    const { client, format } = getClient();
    const result = await client.delete(`/v1/lists/${encodeURIComponent(id)}`);
    printOutput(result, format);
  });

list
  .command("add-member")
  .description("Add a member to a list")
  .argument("<list-id>", "List ID")
  .requiredOption("--parent <record-id>", "Parent record ID")
  .option("--data <json>", "Attributes as JSON string")
  .action(async (listId: string, opts: { parent: string; data?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = { parent_id: opts.parent };
    if (opts.data) {
      try { body.attributes = JSON.parse(opts.data); }
      catch { throw new Error(`Invalid JSON for --data: ${opts.data}`); }
    }
    const result = await client.post(`/v1/lists/${encodeURIComponent(listId)}`, body);
    printOutput(result, format);
  });

list
  .command("upsert-member")
  .description("Upsert a member in a list")
  .argument("<list-id>", "List ID")
  .requiredOption("--parent <record-id>", "Parent record ID")
  .option("--data <json>", "Attributes as JSON string")
  .action(async (listId: string, opts: { parent: string; data?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = { parent_id: opts.parent };
    if (opts.data) {
      try { body.attributes = JSON.parse(opts.data); }
      catch { throw new Error(`Invalid JSON for --data: ${opts.data}`); }
    }
    const result = await client.put(`/v1/lists/${encodeURIComponent(listId)}`, body);
    printOutput(result, format);
  });

list
  .command("records")
  .description("List records in a list")
  .argument("<list-id>", "List ID")
  .option("--limit <n>", "Max records to return")
  .option("--offset <n>", "Offset for pagination")
  .option("--attributes <level>", "Attribute detail: all, primary, none", "primary")
  .option("--sort <sort>", "Sort as attr:dir")
  .action(async (listId: string, opts: { limit?: string; offset?: string; attributes?: string; sort?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = { attributes: opts.attributes ?? "primary" };
    if (opts.limit) body.limit = parseInt(opts.limit, 10);
    if (opts.offset) body.offset = parseInt(opts.offset, 10);
    if (opts.sort) {
      const [attribute, direction] = opts.sort.split(":");
      body.sort = { attribute, direction };
    }
    const result = await client.post(`/v1/lists/${encodeURIComponent(listId)}/records`, body);
    printOutput(result, format);
  });

list
  .command("update-record")
  .description("Update a record in a list")
  .argument("<list-id>", "List ID")
  .argument("<record-id>", "Record ID")
  .requiredOption("--data <json>", "Attributes as JSON string")
  .action(async (listId: string, recordId: string, opts: { data: string }) => {
    const { client, format } = getClient();
    let attributes: unknown;
    try { attributes = JSON.parse(opts.data); }
    catch { throw new Error(`Invalid JSON for --data: ${opts.data}`); }
    const result = await client.patch(`/v1/lists/${encodeURIComponent(listId)}/records/${encodeURIComponent(recordId)}`, { attributes });
    printOutput(result, format);
  });

list
  .command("remove-record")
  .description("Remove a record from a list")
  .argument("<list-id>", "List ID")
  .argument("<record-id>", "Record ID")
  .action(async (listId: string, recordId: string) => {
    const { client, format } = getClient();
    const result = await client.delete(`/v1/lists/${encodeURIComponent(listId)}/records/${encodeURIComponent(recordId)}`);
    printOutput(result, format);
  });
