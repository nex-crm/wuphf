import { z } from "zod";
export function registerRecordTools(server, client) {
    server.tool("create_record", "Create a new record for an object type. Use only when you have clean, structured data with known attribute slugs. For unstructured text, use add_context instead.", {
        object_slug: z.string().describe("Object type slug (e.g. 'person', 'company')"),
        attributes: z.record(z.unknown()).describe("Record attributes — must include 'name'. Additional fields depend on the object schema."),
    }, { readOnlyHint: false }, async ({ object_slug, attributes }) => {
        const result = await client.post(`/v1/objects/${encodeURIComponent(object_slug)}`, { attributes });
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("upsert_record", "Create a record if it doesn't exist, or update it if a match is found on the specified attribute. Useful for deduplication (e.g. match on email).", {
        object_slug: z.string().describe("Object type slug (e.g. 'person', 'company')"),
        matching_attribute: z.string().describe("Attribute slug or ID to match on for dedup (e.g. 'email')"),
        attributes: z.record(z.unknown()).describe("Record attributes — must include 'name' when creating"),
    }, { readOnlyHint: false, idempotentHint: true }, async ({ object_slug, matching_attribute, attributes }) => {
        const result = await client.put(`/v1/objects/${encodeURIComponent(object_slug)}`, { matching_attribute, attributes });
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("get_record", "Retrieve a specific record by its ID, including all its attributes.", { record_id: z.string().describe("Record ID") }, { readOnlyHint: true }, async ({ record_id }) => {
        const result = await client.get(`/v1/records/${encodeURIComponent(record_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("update_record", "Update specific attributes on an existing record. Only the provided attributes are changed.", {
        record_id: z.string().describe("Record ID to update"),
        attributes: z.record(z.unknown()).describe("Attributes to update (only provided fields are changed)"),
    }, { readOnlyHint: false }, async ({ record_id, attributes }) => {
        const result = await client.patch(`/v1/records/${encodeURIComponent(record_id)}`, { attributes });
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("delete_record", "Permanently delete a record. This cannot be undone.", { record_id: z.string().describe("Record ID to delete") }, { readOnlyHint: false, destructiveHint: true }, async ({ record_id }) => {
        const result = await client.delete(`/v1/records/${encodeURIComponent(record_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("list_records", "List records for an object type with optional filtering, sorting, and pagination.", {
        object_slug: z.string().describe("Object type slug (e.g. 'person', 'company')"),
        attributes: z.union([
            z.enum(["all", "primary", "none"]),
            z.record(z.unknown()),
        ]).optional().describe("Which attributes to return: 'all', 'primary', 'none', or a custom object"),
        limit: z.number().optional().describe("Number of records to return"),
        offset: z.number().optional().describe("Pagination offset"),
        sort: z.object({
            attribute: z.string().describe("Attribute slug to sort by"),
            direction: z.enum(["asc", "desc"]).describe("Sort direction"),
        }).optional().describe("Sort configuration"),
    }, { readOnlyHint: true }, async ({ object_slug, attributes, limit, offset, sort }) => {
        const body = {};
        if (attributes !== undefined)
            body.attributes = attributes;
        if (limit !== undefined)
            body.limit = limit;
        if (offset !== undefined)
            body.offset = offset;
        if (sort)
            body.sort = sort;
        const result = await client.post(`/v1/objects/${encodeURIComponent(object_slug)}/records`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("get_record_timeline", "Get paginated timeline events for a record (tasks, notes, attribute changes, etc.).", {
        record_id: z.string().describe("Record ID"),
        limit: z.number().optional().describe("Max events (1-100, default: 50)"),
        cursor: z.string().optional().describe("Pagination cursor from previous response"),
    }, { readOnlyHint: true }, async ({ record_id, limit, cursor }) => {
        const params = new URLSearchParams();
        if (limit !== undefined)
            params.set("limit", String(limit));
        if (cursor)
            params.set("cursor", cursor);
        const qs = params.toString();
        const path = `/v1/records/${encodeURIComponent(record_id)}/timeline${qs ? `?${qs}` : ""}`;
        const result = await client.get(path);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
}
//# sourceMappingURL=records.js.map