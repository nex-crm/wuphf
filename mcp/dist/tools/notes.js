import { z } from "zod";
export function registerNoteTools(server, client) {
    server.tool("create_note", "Create a new note, optionally linked to a record.", {
        title: z.string().min(1).describe("Note title"),
        content: z.string().optional().describe("Note body text"),
        entity_id: z.string().optional().describe("Associated record ID"),
    }, { readOnlyHint: false }, async ({ title, content, entity_id }) => {
        const body = { title };
        if (content !== undefined)
            body.content = content;
        if (entity_id !== undefined)
            body.entity_id = entity_id;
        const result = await client.post("/v1/notes", body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("list_notes", "List notes, optionally filtered by associated record. Returns up to 200 notes.", {
        entity_id: z.string().optional().describe("Filter notes by associated record ID"),
    }, { readOnlyHint: true }, async ({ entity_id }) => {
        const params = new URLSearchParams();
        if (entity_id)
            params.set("entity_id", entity_id);
        const qs = params.toString();
        const path = `/v1/notes${qs ? `?${qs}` : ""}`;
        const result = await client.get(path);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("get_note", "Get a single note by ID.", { note_id: z.string().describe("Note ID") }, { readOnlyHint: true }, async ({ note_id }) => {
        const result = await client.get(`/v1/notes/${encodeURIComponent(note_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("update_note", "Update a note's fields. All fields are optional — only provided fields are changed.", {
        note_id: z.string().describe("Note ID to update"),
        title: z.string().optional().describe("New title"),
        content: z.string().optional().describe("New content"),
        entity_id: z.string().optional().describe("Change associated record"),
    }, { readOnlyHint: false }, async ({ note_id, title, content, entity_id }) => {
        const body = {};
        if (title !== undefined)
            body.title = title;
        if (content !== undefined)
            body.content = content;
        if (entity_id !== undefined)
            body.entity_id = entity_id;
        const result = await client.patch(`/v1/notes/${encodeURIComponent(note_id)}`, body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("delete_note", "Archive a note (soft delete). Cannot be undone via API.", { note_id: z.string().describe("Note ID to delete") }, { readOnlyHint: false, destructiveHint: true }, async ({ note_id }) => {
        const result = await client.delete(`/v1/notes/${encodeURIComponent(note_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
}
//# sourceMappingURL=notes.js.map