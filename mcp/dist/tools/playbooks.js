import { z } from "zod";
export function registerPlaybookTools(server, client) {
    server.tool("list_briefs", "List entity briefs and workspace playbooks. Entity briefs are per-person/company intelligence documents. Workspace playbooks are cross-entity pattern documents. Use scope_type to filter: 1=entity briefs, 2=workspace playbooks, or omit for all.", {
        scope_type: z.number().optional().describe("Filter by type: 1=entity briefs, 2=workspace playbooks. Omit for all."),
        limit: z.number().optional().describe("Max results (default: 50)"),
        offset: z.number().optional().describe("Pagination offset (default: 0)"),
    }, { readOnlyHint: true }, async ({ scope_type, limit, offset }) => {
        const params = new URLSearchParams();
        if (scope_type !== undefined)
            params.set("scope_type", String(scope_type));
        if (limit !== undefined)
            params.set("limit", String(limit));
        if (offset !== undefined)
            params.set("offset", String(offset));
        const qs = params.toString();
        const result = await client.get(`/v1/playbooks${qs ? `?${qs}` : ""}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("get_brief", "Get the full content of an entity brief or workspace playbook by ID.", { id: z.string().describe("The brief/playbook ID") }, { readOnlyHint: true }, async ({ id }) => {
        const result = await client.get(`/v1/playbooks/${encodeURIComponent(id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("get_entity_brief", "Get the entity brief for a specific entity by its context ID.", { context_id: z.string().describe("The entity's context_id") }, { readOnlyHint: true }, async ({ context_id }) => {
        try {
            const result = await client.get(`/v1/playbooks/by-context/${encodeURIComponent(context_id)}`);
            return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
        }
        catch (e) {
            if (e && typeof e === "object" && "status" in e && e.status === 404) {
                return { content: [{ type: "text", text: "No brief exists for this entity yet. Use compile_brief to generate one." }] };
            }
            throw e;
        }
    });
    server.tool("get_workspace_playbook", "Get a workspace playbook by its slug.", { slug: z.string().describe("The workspace playbook slug") }, { readOnlyHint: true }, async ({ slug }) => {
        const result = await client.get(`/v1/playbooks/workspace/${encodeURIComponent(slug)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("download_brief", "Download an entity brief or workspace playbook as raw markdown.", { id: z.string().describe("The brief/playbook ID") }, { readOnlyHint: true }, async ({ id }) => {
        const result = await client.getRaw(`/v1/playbooks/${encodeURIComponent(id)}/download`);
        return { content: [{ type: "text", text: result }] };
    });
    server.tool("compile_brief", "Trigger compilation of an entity brief.", {
        context_id: z.string().describe("The entity's context_id to compile a brief for"),
        is_private: z.boolean().optional().describe("If true, creates a private brief visible only to you"),
    }, { readOnlyHint: false }, async ({ context_id, is_private }) => {
        const body = { context_id };
        if (is_private)
            body.is_private = true;
        const result = await client.post("/v1/playbooks/compile", body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("get_brief_history", "Get version history for a brief/playbook.", {
        id: z.string().describe("The brief/playbook ID"),
        limit: z.number().optional().describe("Max events to return (default: 10)"),
    }, { readOnlyHint: true }, async ({ id, limit }) => {
        const params = new URLSearchParams();
        if (limit !== undefined)
            params.set("limit", String(limit));
        const qs = params.toString();
        const result = await client.get(`/v1/playbooks/${encodeURIComponent(id)}/history${qs ? `?${qs}` : ""}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
}
//# sourceMappingURL=playbooks.js.map