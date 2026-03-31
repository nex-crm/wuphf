import { z } from "zod";
export function registerInsightTools(server, client) {
    server.tool("get_insights", "Query insights by time window. Returns discovered opportunities, risks, relationship changes, milestones, and other insights. Use 'last' for a duration window (e.g. '30m', '2h') or 'from'/'to' for an absolute time range.", {
        last: z.string().optional().describe("Duration window, e.g. '30m', '2h', '1h30m'"),
        from: z.string().optional().describe("Start of time range in RFC3339 format"),
        to: z.string().optional().describe("End of time range in RFC3339 format"),
        limit: z.number().optional().describe("Max results (default: 20, max: 100)"),
    }, { readOnlyHint: true }, async ({ last, from, to, limit }) => {
        const params = new URLSearchParams();
        if (last)
            params.set("last", last);
        if (from)
            params.set("from", from);
        if (to)
            params.set("to", to);
        if (limit !== undefined)
            params.set("limit", String(limit));
        const qs = params.toString();
        const path = `/v1/insights${qs ? `?${qs}` : ""}`;
        const result = await client.get(path);
        return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    });
}
//# sourceMappingURL=insights.js.map