/**
 * nex rel — manage relationship definitions and instances.
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

const rel = program.command("rel").description("Manage relationships");

rel
  .command("list-defs")
  .description("List relationship definitions")
  .action(async () => {
    const { client, format } = getClient();
    const result = await client.get("/v1/relationships");
    printOutput(result, format);
  });

rel
  .command("create-def")
  .description("Create a relationship definition")
  .requiredOption("--type <type>", "Relationship type: one_to_one, one_to_many, many_to_many")
  .requiredOption("--entity1 <id>", "Entity definition 1 ID")
  .requiredOption("--entity2 <id>", "Entity definition 2 ID")
  .option("--pred12 <predicate>", "Entity 1 to 2 predicate")
  .option("--pred21 <predicate>", "Entity 2 to 1 predicate")
  .action(async (opts: { type: string; entity1: string; entity2: string; pred12?: string; pred21?: string }) => {
    const { client, format } = getClient();
    const body: Record<string, unknown> = {
      type: opts.type,
      entity_definition_1_id: opts.entity1,
      entity_definition_2_id: opts.entity2,
    };
    if (opts.pred12) body.entity_1_to_2_predicate = opts.pred12;
    if (opts.pred21) body.entity_2_to_1_predicate = opts.pred21;
    const result = await client.post("/v1/relationships", body);
    printOutput(result, format);
  });

rel
  .command("delete-def")
  .description("Delete a relationship definition")
  .argument("<id>", "Relationship definition ID")
  .action(async (id: string) => {
    const { client, format } = getClient();
    const result = await client.delete(`/v1/relationships/${encodeURIComponent(id)}`);
    printOutput(result, format);
  });

rel
  .command("create")
  .description("Create a relationship between records")
  .argument("<record-id>", "Record ID")
  .requiredOption("--def <id>", "Relationship definition ID")
  .requiredOption("--entity1 <id>", "Entity 1 ID")
  .requiredOption("--entity2 <id>", "Entity 2 ID")
  .action(async (recordId: string, opts: { def: string; entity1: string; entity2: string }) => {
    const { client, format } = getClient();
    const body = {
      definition_id: opts.def,
      entity_1_id: opts.entity1,
      entity_2_id: opts.entity2,
    };
    const result = await client.post(`/v1/records/${encodeURIComponent(recordId)}/relationships`, body);
    printOutput(result, format);
  });

rel
  .command("delete")
  .description("Delete a relationship")
  .argument("<record-id>", "Record ID")
  .argument("<rel-id>", "Relationship ID")
  .action(async (recordId: string, relId: string) => {
    const { client, format } = getClient();
    const result = await client.delete(`/v1/records/${encodeURIComponent(recordId)}/relationships/${encodeURIComponent(relId)}`);
    printOutput(result, format);
  });
