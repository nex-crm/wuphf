import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { NexApiClient } from "../client.js";

export function registerSchemaTools(server: McpServer, client: NexApiClient) {
  server.tool(
    "create_object",
    "Create a new custom object type definition (e.g. 'Project', 'Deal'). Defines the schema for a new entity type in your workspace.",
    {
      name: z.string().describe("Display name for the object type"),
      slug: z.string().describe("URL-safe identifier (lowercase, hyphens)"),
      name_plural: z.string().optional().describe("Plural display name"),
      description: z.string().optional().describe("Description of the object type"),
      type: z.enum(["person", "company", "custom", "deal"]).optional().describe("Object category (default: custom)"),
    },
    { readOnlyHint: false },
    async ({ name, slug, name_plural, description, type }) => {
      const body: Record<string, unknown> = { name, slug };
      if (name_plural !== undefined) body.name_plural = name_plural;
      if (description !== undefined) body.description = description;
      if (type !== undefined) body.type = type;
      const result = await client.post("/v1/objects", body);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "get_object",
    "Get a single object type definition with its attributes. Use this to discover available fields before creating or querying records.",
    { slug: z.string().describe("Object type slug (e.g. 'person', 'company', 'deal')") },
    { readOnlyHint: true },
    async ({ slug }) => {
      const result = await client.get(`/v1/objects/${encodeURIComponent(slug)}`);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "list_objects",
    "List all object type definitions in the workspace. Call this first to discover available object types and their schemas before creating or querying records.",
    {
      include_attributes: z.boolean().optional().describe("Include attribute definitions in the response"),
    },
    { readOnlyHint: true },
    async ({ include_attributes }) => {
      const params = new URLSearchParams();
      if (include_attributes) params.set("include_attributes", "true");
      const qs = params.toString();
      const path = `/v1/objects${qs ? `?${qs}` : ""}`;
      const result = await client.get(path);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "update_object",
    "Update an existing object type definition (name, description, plural name).",
    {
      slug: z.string().describe("Object type slug to update"),
      name: z.string().optional().describe("New display name"),
      name_plural: z.string().optional().describe("New plural display name"),
      description: z.string().optional().describe("New description"),
    },
    { readOnlyHint: false },
    async ({ slug, name, name_plural, description }) => {
      const body: Record<string, unknown> = {};
      if (name !== undefined) body.name = name;
      if (name_plural !== undefined) body.name_plural = name_plural;
      if (description !== undefined) body.description = description;
      const result = await client.patch(`/v1/objects/${encodeURIComponent(slug)}`, body);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "delete_object",
    "Delete an object type definition and ALL its records. This is destructive and cannot be undone.",
    { slug: z.string().describe("Object type slug to delete") },
    { readOnlyHint: false, destructiveHint: true },
    async ({ slug }) => {
      const result = await client.delete(`/v1/objects/${encodeURIComponent(slug)}`);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "create_attribute",
    "Add a new attribute (field) to an object type. Supports types: text, number, email, phone, url, date, boolean, currency, location, select, social_profile, domain, full_name.",
    {
      object_slug: z.string().describe("Object type slug to add the attribute to"),
      name: z.string().describe("Display name for the attribute"),
      slug: z.string().describe("URL-safe identifier for the attribute"),
      type: z.enum(["text", "number", "email", "phone", "url", "date", "boolean", "currency", "location", "select", "social_profile", "domain", "full_name"]).describe("Attribute data type"),
      description: z.string().optional().describe("Description of the attribute"),
      options: z.object({
        is_required: z.boolean().optional(),
        is_unique: z.boolean().optional(),
        is_multi_value: z.boolean().optional(),
        use_raw_format: z.boolean().optional(),
        is_whole_number: z.boolean().optional(),
        select_options: z.array(z.object({ name: z.string() })).optional(),
      }).optional().describe("Attribute options"),
    },
    { readOnlyHint: false },
    async ({ object_slug, name, slug, type, description, options }) => {
      const body: Record<string, unknown> = { name, slug, type };
      if (description !== undefined) body.description = description;
      if (options) body.options = options;
      const result = await client.post(`/v1/objects/${encodeURIComponent(object_slug)}/attributes`, body);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "update_attribute",
    "Update an existing attribute definition on an object type.",
    {
      object_slug: z.string().describe("Object type slug"),
      attribute_id: z.string().describe("Attribute ID to update"),
      name: z.string().optional().describe("New display name"),
      description: z.string().optional().describe("New description"),
      options: z.object({
        is_required: z.boolean().optional(),
        select_options: z.array(z.object({ name: z.string() })).optional(),
        use_raw_format: z.boolean().optional(),
        is_whole_number: z.boolean().optional(),
      }).optional().describe("Updated options"),
    },
    { readOnlyHint: false },
    async ({ object_slug, attribute_id, name, description, options }) => {
      const body: Record<string, unknown> = {};
      if (name !== undefined) body.name = name;
      if (description !== undefined) body.description = description;
      if (options) body.options = options;
      const result = await client.patch(`/v1/objects/${encodeURIComponent(object_slug)}/attributes/${encodeURIComponent(attribute_id)}`, body);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "delete_attribute",
    "Delete an attribute from an object type. This removes the field and its data from all records. Cannot be undone.",
    {
      object_slug: z.string().describe("Object type slug"),
      attribute_id: z.string().describe("Attribute ID to delete"),
    },
    { readOnlyHint: false, destructiveHint: true },
    async ({ object_slug, attribute_id }) => {
      const result = await client.delete(`/v1/objects/${encodeURIComponent(object_slug)}/attributes/${encodeURIComponent(attribute_id)}`);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );
}
