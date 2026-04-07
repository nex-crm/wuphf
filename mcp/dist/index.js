#!/usr/bin/env node
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { createServer } from "./server.js";
import { createServer as createHttpServer } from "node:http";
import { loadApiKey } from "./config.js";
const apiKey = loadApiKey();
if (!apiKey) {
    console.error("No API key found (checked WUPHF_API_KEY env and ~/.wuphf/config.json). Starting in registration-only mode. Use the 'register' tool to create an account and get an API key. Once registered, all context, search, and scan tools become available.");
}
const transport = process.env.MCP_TRANSPORT ?? "stdio";
async function main() {
    const server = createServer(apiKey);
    if (transport === "http") {
        const port = parseInt(process.env.MCP_PORT ?? "3001", 10);
        const httpTransport = new StreamableHTTPServerTransport({
            sessionIdGenerator: undefined,
        });
        const httpServer = createHttpServer(async (req, res) => {
            const url = new URL(req.url ?? "/", `http://localhost:${port}`);
            if (url.pathname === "/mcp") {
                await httpTransport.handleRequest(req, res);
            }
            else if (url.pathname === "/health") {
                res.writeHead(200, { "Content-Type": "application/json" });
                res.end(JSON.stringify({ status: "ok" }));
            }
            else {
                res.writeHead(404);
                res.end("Not found");
            }
        });
        await server.connect(httpTransport);
        httpServer.listen(port, () => {
            console.error(`WUPHF MCP server running on http://localhost:${port}/mcp`);
        });
    }
    else {
        const stdioTransport = new StdioServerTransport();
        await server.connect(stdioTransport);
        console.error("WUPHF MCP server running on stdio");
    }
}
main().catch((err) => {
    console.error("Fatal error:", err);
    process.exit(1);
});
//# sourceMappingURL=index.js.map