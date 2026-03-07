import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { NexApiClient } from "../client.js";

export function registerIntegrationTools(server: McpServer, client: NexApiClient) {
  server.tool(
    "list_integrations",
    "List all available third-party integrations and their connection status. Calendar integrations (Google Calendar, Outlook Calendar) enable the Nex Meeting Bot which joins calls on any platform (Google Meet, Zoom, Webex, Teams, etc.) and feeds transcripts into the context graph. To connect, use connect_integration. To disconnect, use disconnect_integration.",
    {},
    { readOnlyHint: true },
    async () => {
      const result = await client.get("/v1/integrations/");
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "connect_integration",
    "Start connecting a third-party integration via OAuth. Returns an auth_url to open in the browser and a connect_id to poll for status. Calendar integrations (type: 'calendar') enable the Nex Meeting Bot which auto-joins calls and processes transcripts.",
    {
      type: z.enum(["email", "calendar", "crm", "messaging"]).describe("Integration type"),
      provider: z.enum(["google", "microsoft", "attio", "slack", "salesforce", "hubspot"]).describe("Integration provider"),
    },
    { readOnlyHint: false },
    async ({ type, provider }) => {
      const result = await client.post(`/v1/integrations/${encodeURIComponent(type)}/${encodeURIComponent(provider)}/connect`);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "get_connect_status",
    "Check the status of an in-progress OAuth connection. Poll this after connect_integration until status is 'connected'.",
    {
      connect_id: z.string().describe("Connect ID returned from connect_integration"),
    },
    { readOnlyHint: true },
    async ({ connect_id }) => {
      const result = await client.get(`/v1/integrations/connect/${encodeURIComponent(connect_id)}/status`);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );

  server.tool(
    "disconnect_integration",
    "Disconnect a third-party integration by connection ID. Get connection IDs from list_integrations.",
    {
      connection_id: z.string().describe("Connection ID to disconnect"),
    },
    { readOnlyHint: false, destructiveHint: true },
    async ({ connection_id }) => {
      const result = await client.delete(`/v1/integrations/connections/${encodeURIComponent(connection_id)}`);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    },
  );
}
