import { z } from "zod";
export function registerContextTools(server, client) {
    server.tool("query_context", "Query the WUPHF context graph with a natural language question. Returns an AI-generated answer with supporting entities and evidence. Use for open-ended questions about contacts, companies, relationships, or history.", {
        query: z.string().describe("Natural language question about your contacts, companies, or relationships"),
        session_id: z.string().optional().describe("Session ID for multi-turn conversational continuity"),
    }, { readOnlyHint: true, openWorldHint: true }, async ({ query, session_id }) => {
        const body = { query };
        if (session_id)
            body.session_id = session_id;
        const result = await client.post("/v1/context/ask", body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("add_context", "Ingest unstructured text (meeting notes, emails, conversation transcripts) into the WUPHF context graph. Automatically extracts entities, relationships, and insights. Returns an artifact_id — use get_artifact_status to check processing results.", {
        content: z.string().describe("The text content to process (meeting notes, email, conversation transcript, etc.)"),
        context: z.string().optional().describe("Additional context about the text, e.g. 'Sales call notes' or 'Email from client'"),
    }, { readOnlyHint: false }, async ({ content, context }) => {
        const body = { content };
        if (context !== undefined)
            body.context = context;
        const result = await client.post("/v1/context/text", body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("get_artifact_status", "Check the processing status and results of a previously submitted text artifact. Poll until status is 'completed' or 'failed'. Returns extracted entities, relationships, and insights.", { artifact_id: z.string().describe("The artifact ID returned by add_context") }, { readOnlyHint: true }, async ({ artifact_id }) => {
        const result = await client.get(`/v1/context/artifacts/${encodeURIComponent(artifact_id)}`);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("create_list_job", "Create an AI-powered list generation job. Uses natural language to search the context graph and generate a curated list of contacts or companies. Returns a job_id — use get_list_job_status to poll for results.", {
        query: z.string().describe("Natural language description of the list you want, e.g. 'high priority contacts in enterprise deals'"),
        object_type: z.enum(["contact", "company"]).optional().describe("Type of entities to search for (default: contact)"),
        limit: z.number().optional().describe("Maximum number of results (default: 50, max: 100)"),
        include_attributes: z.boolean().optional().describe("Include full attribute values for each entity"),
    }, { readOnlyHint: false }, async ({ query, object_type, limit, include_attributes }) => {
        const body = { query };
        if (object_type)
            body.object_type = object_type;
        if (limit !== undefined)
            body.limit = limit;
        if (include_attributes !== undefined)
            body.include_attributes = include_attributes;
        const result = await client.post("/v1/context/list/jobs", body);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("get_list_job_status", "Check the status and results of an AI list generation job. Poll until status is 'completed' or 'failed'. Returns matched entities with reasons and highlights.", {
        job_id: z.string().describe("The job ID returned by create_list_job"),
        include_attributes: z.boolean().optional().describe("Include full attribute values for each entity"),
    }, { readOnlyHint: true }, async ({ job_id, include_attributes }) => {
        const params = new URLSearchParams();
        if (include_attributes)
            params.set("include_attributes", "true");
        const qs = params.toString();
        const path = `/v1/context/list/jobs/${encodeURIComponent(job_id)}${qs ? `?${qs}` : ""}`;
        const result = await client.get(path);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
    server.tool("search_entities", "Search for entities (people, companies, topics) in the WUPHF knowledge base. Returns a structured list with names, types, and mention counts.", { query: z.string().describe("Search query to find entities") }, { readOnlyHint: true }, async ({ query }) => {
        const result = await client.post("/v1/context/ask", { query });
        const entities = result.entity_references ?? [];
        if (entities.length === 0) {
            return { content: [{ type: "text", text: "No matching entities found." }] };
        }
        const lines = entities.map(e => `- ${e.name} (${e.type})${e.count ? ` — ${e.count} mentions` : ""}`);
        return { content: [{ type: "text", text: `Found ${entities.length} entities:\n${lines.join("\n")}` }] };
    });
}
//# sourceMappingURL=context.js.map