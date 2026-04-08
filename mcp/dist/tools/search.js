import { z } from "zod";
export function registerSearchTools(server, client) {
    server.tool("search_records", "Search records by name across all object types (person, company, deal, etc.). Returns matches grouped by object type with relevance scores.", { query: z.string().min(1).max(500).describe("Search query (1-500 characters)") }, { readOnlyHint: true }, async ({ query }) => {
        const result = await client.post("/v1/search", { query });
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
}
//# sourceMappingURL=search.js.map