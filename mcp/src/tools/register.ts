import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { NexApiClient } from "../client.js";
import { persistRegistration, CONFIG_PATH } from "../config.js";

export function registerRegistrationTools(server: McpServer, client: NexApiClient) {
  server.tool(
    "register",
    `Register for a Nex API key. Call this first if no NEX_API_KEY is configured. Requires the user's email. After successful registration, the API key is saved to ${CONFIG_PATH} and all other tools become available immediately. Returns the API key, workspace info, and plan details.`,
    {
      email: z.string().email().describe("User's email address (required for registration)"),
      name: z.string().optional().describe("User's full name"),
      company_name: z.string().optional().describe("Company or organization name"),
    },
    { readOnlyHint: false },
    async ({ email, name, company_name }) => {
      if (client.isAuthenticated) {
        return {
          content: [{
            type: "text",
            text: JSON.stringify({ message: "Already registered. API key is configured and active. You can use all other tools." }),
          }],
        };
      }
      const result = await client.register(email, name, company_name);
      persistRegistration(result as Record<string, unknown>);
      return {
        content: [{
          type: "text",
          text: JSON.stringify({
            ...(result as Record<string, unknown>),
            _config_saved_to: CONFIG_PATH,
          }, null, 2),
        }],
      };
    },
  );
}
