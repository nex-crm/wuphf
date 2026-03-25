/**
 * wuphf note — CRUD operations for notes.
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

const note = program.command("note").description("Manage notes");

note
  .command("list")
  .description("List notes")
  .option("--entity <id>", "Filter by entity ID")
  .action(async (opts: { entity?: string }) => {
    const { client, format } = getClient();
    const params = opts.entity ? `?entity_id=${encodeURIComponent(opts.entity)}` : "";
    const result = await client.get(`/v1/notes${params}`);
    printOutput(result, format);
  });

note
  .command("get")
  .description("Get a note by ID")
  .argument("<id>", "Note ID")
  .action(async (id: string) => {
    const { client, format } = getClient();
    const result = await client.get(`/v1/notes/${encodeURIComponent(id)}`);
    printOutput(result, format);
  });

note
  .command("create")
  .description("Create a note")
  .requiredOption("--title <title>", "Note title")
  .option("--content <content>", "Note content")
  .option("--entity <id>", "Entity ID to attach to")
  .action(async (opts: { title: string; content?: string; entity?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = { title: opts.title };
    if (opts.content) body.content = opts.content;
    if (opts.entity) body.entity_id = opts.entity;
    const result = await client.post("/v1/notes", body);
    printOutput(result, format);
  });

note
  .command("update")
  .description("Update a note")
  .argument("<id>", "Note ID")
  .option("--title <title>", "New title")
  .option("--content <content>", "New content")
  .option("--entity <id>", "Change associated entity")
  .action(async (id: string, opts: { title?: string; content?: string; entity?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = {};
    if (opts.title !== undefined) body.title = opts.title;
    if (opts.content !== undefined) body.content = opts.content;
    if (opts.entity !== undefined) body.entity_id = opts.entity;
    const result = await client.patch(`/v1/notes/${encodeURIComponent(id)}`, body);
    printOutput(result, format);
  });

note
  .command("delete")
  .description("Delete a note")
  .argument("<id>", "Note ID")
  .action(async (id: string) => {
    const { client, format } = getClient();
    const result = await client.delete(`/v1/notes/${encodeURIComponent(id)}`);
    printOutput(result, format);
  });
