import { z } from "zod";
export function registerListTools(server, client) {
    server.tool("list_object_lists", "Get all lists associated with an object type.", {
        object_slug: z.string().describe("Object type slug (e.g. 'person', 'company')"),
        include_attributes: z.boolean().optional().describe("Include attribute definitions"),
    }, { readOnlyHint: true }, async ({ object_slug, include_attributes }) => {
        const params = new URLSearchParams();
        if (include_attributes)
            params.set("include_attributes", "true");
        const qs = params.toString();
        const path = `/v1/objects/${encodeURIComponent(object_slug)}/lists${qs ? `?${qs}` : ""}`;
        const result = await client.get(path);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("create_list", "Create a new list under an object type.", {
        object_slug: z.string().describe("Object type slug"),
        name: z.string().describe("List display name"),
        slug: z.string().describe("URL-safe identifier"),
        name_plural: z.string().optional().describe("Plural name"),
        description: z.string().optional().describe("List description"),
    }, { readOnlyHint: false }, async ({ object_slug, name, slug, name_plural, description }) => {
        const body = { name, slug };
        if (name_plural !== undefined)
            body.name_plural = name_plural;
        if (description !== undefined)
            body.description = description;
        const result = await client.post(`/v1/objects/${encodeURIComponent(object_slug)}/lists`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("get_list", "Get a list definition by ID.", { list_id: z.string().describe("List ID") }, { readOnlyHint: true }, async ({ list_id }) => {
        const result = await client.get(`/v1/lists/${encodeURIComponent(list_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("delete_list", "Delete a list definition. Cannot be undone.", { list_id: z.string().describe("List ID to delete") }, { readOnlyHint: false, destructiveHint: true }, async ({ list_id }) => {
        const result = await client.delete(`/v1/lists/${encodeURIComponent(list_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("add_list_member", "Add an existing record to a list with optional list-specific attributes.", {
        list_id: z.string().describe("List ID"),
        parent_id: z.string().describe("ID of the existing record to add"),
        attributes: z.record(z.unknown()).optional().describe("List-specific attribute values"),
    }, { readOnlyHint: false }, async ({ list_id, parent_id, attributes }) => {
        const body = { parent_id };
        if (attributes)
            body.attributes = attributes;
        const result = await client.post(`/v1/lists/${encodeURIComponent(list_id)}`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("upsert_list_member", "Add a record to a list, or update its list-specific attributes if already a member.", {
        list_id: z.string().describe("List ID"),
        parent_id: z.string().describe("ID of the record"),
        attributes: z.record(z.unknown()).optional().describe("List-specific attribute values"),
    }, { readOnlyHint: false, idempotentHint: true }, async ({ list_id, parent_id, attributes }) => {
        const body = { parent_id };
        if (attributes)
            body.attributes = attributes;
        const result = await client.put(`/v1/lists/${encodeURIComponent(list_id)}`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("list_list_records", "Get paginated records from a specific list.", {
        list_id: z.string().describe("List ID"),
        attributes: z.union([
            z.enum(["all", "primary", "none"]),
            z.record(z.unknown()),
        ]).optional().describe("Which attributes to return"),
        limit: z.number().optional().describe("Number of records to return"),
        offset: z.number().optional().describe("Pagination offset"),
        sort: z.object({
            attribute: z.string(),
            direction: z.enum(["asc", "desc"]),
        }).optional().describe("Sort configuration"),
    }, { readOnlyHint: true }, async ({ list_id, attributes, limit, offset, sort }) => {
        const body = {};
        if (attributes !== undefined)
            body.attributes = attributes;
        if (limit !== undefined)
            body.limit = limit;
        if (offset !== undefined)
            body.offset = offset;
        if (sort)
            body.sort = sort;
        const result = await client.post(`/v1/lists/${encodeURIComponent(list_id)}/records`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("update_list_record", "Update list-specific attributes for a record within a list.", {
        list_id: z.string().describe("List ID"),
        record_id: z.string().describe("Record ID within the list"),
        attributes: z.record(z.unknown()).describe("Attributes to update"),
    }, { readOnlyHint: false }, async ({ list_id, record_id, attributes }) => {
        const result = await client.patch(`/v1/lists/${encodeURIComponent(list_id)}/records/${encodeURIComponent(record_id)}`, { attributes });
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("delete_list_record", "Remove a record from a list. The record itself is not deleted.", {
        list_id: z.string().describe("List ID"),
        record_id: z.string().describe("Record ID to remove from the list"),
    }, { readOnlyHint: false, destructiveHint: true }, async ({ list_id, record_id }) => {
        const result = await client.delete(`/v1/lists/${encodeURIComponent(list_id)}/records/${encodeURIComponent(record_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
}
//# sourceMappingURL=lists.js.map