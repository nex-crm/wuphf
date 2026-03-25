import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { NexApiClient } from "../client.js";

export function registerRelationshipTools(server: McpServer, client: NexApiClient) {
  server.tool(
    "create_relationship_definition",
    "Define a new relationship type between two object types (e.g. person 'works at' company).",
    {
      type: z.enum(["one_to_one", "one_to_many", "many_to_many"]).describe("Relationship cardinality"),
      entity_definition_1_id: z.string().describe("First object definition ID"),
      entity_definition_2_id: z.string().describe("Second object definition ID"),
      entity_1_to_2_predicate: z.string().optional().describe("Label for 1→2 direction (e.g. 'works at')"),
      entity_2_to_1_predicate: z.string().optional().describe("Label for 2→1 direction (e.g. 'employs')"),
    },
    { readOnlyHint: false },
    async ({ type, entity_definition_1_id, entity_definition_2_id, entity_1_to_2_predicate, entity_2_to_1_predicate }) => {
      const body: Record<string, unknown> = { type, entity_definition_1_id, entity_definition_2_id };
      if (entity_1_to_2_predicate !== undefined) body.entity_1_to_2_predicate = entity_1_to_2_predicate;
      if (entity_2_to_1_predicate !== undefined) body.entity_2_to_1_predicate = entity_2_to_1_predicate;
      const result = await client.post("/v1/relationships", body);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "list_relationship_definitions",
    "List all relationship type definitions in the workspace.",
    {},
    { readOnlyHint: true },
    async () => {
      const result = await client.get("/v1/relationships");
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "delete_relationship_definition",
    "Delete a relationship type definition. This removes all instances of this relationship. Cannot be undone.",
    { id: z.string().describe("Relationship definition ID to delete") },
    { readOnlyHint: false, destructiveHint: true },
    async ({ id }) => {
      const result = await client.delete(`/v1/relationships/${encodeURIComponent(id)}`);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "create_relationship",
    "Link two records using an existing relationship definition.",
    {
      record_id: z.string().describe("Record ID to create the relationship from (used in the URL path)"),
      definition_id: z.string().describe("Relationship definition ID"),
      entity_1_id: z.string().describe("First record ID"),
      entity_2_id: z.string().describe("Second record ID"),
    },
    { readOnlyHint: false },
    async ({ record_id, definition_id, entity_1_id, entity_2_id }) => {
      const result = await client.post(`/v1/records/${encodeURIComponent(record_id)}/relationships`, {
        definition_id,
        entity_1_id,
        entity_2_id,
      });
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "delete_relationship",
    "Remove a relationship between two records. Cannot be undone.",
    {
      record_id: z.string().describe("Record ID"),
      relationship_id: z.string().describe("Relationship instance ID to delete"),
    },
    { readOnlyHint: false, destructiveHint: true },
    async ({ record_id, relationship_id }) => {
      const result = await client.delete(`/v1/records/${encodeURIComponent(record_id)}/relationships/${encodeURIComponent(relationship_id)}`);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );
}
