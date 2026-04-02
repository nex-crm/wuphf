import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { NexApiClient } from "./client.js";
import { registerContextTools } from "./tools/context.js";
import { registerSearchTools } from "./tools/search.js";
import { registerSchemaTools } from "./tools/schema.js";
import { registerRecordTools } from "./tools/records.js";
import { registerRelationshipTools } from "./tools/relationships.js";
import { registerListTools } from "./tools/lists.js";
import { registerTaskTools } from "./tools/tasks.js";
import { registerNoteTools } from "./tools/notes.js";
import { registerInsightTools } from "./tools/insights.js";
import { registerRegistrationTools } from "./tools/register.js";
import { registerScanTools } from "./tools/scan.js";
import { registerIntegrationTools } from "./tools/integrations.js";
import { registerTeamTools } from "./tools/team.js";
import { registerPlaybookTools } from "./tools/playbooks.js";
import { registerBriefSyncTools } from "./tools/brief-sync.js";
import { registerSkillTools } from "./tools/skills.js";
import { registerSkillSyncTools } from "./tools/skill-sync.js";

export function createServer(apiKey?: string): McpServer {
  const server = new McpServer({
    name: "wuphf",
    version: "0.1.0",
  });

  const client = new NexApiClient(apiKey);

  registerRegistrationTools(server, client);
  registerContextTools(server, client);
  registerSearchTools(server, client);
  registerSchemaTools(server, client);
  registerRecordTools(server, client);
  registerRelationshipTools(server, client);
  registerListTools(server, client);
  registerTaskTools(server, client);
  registerNoteTools(server, client);
  registerInsightTools(server, client);
  registerScanTools(server, client);
  registerIntegrationTools(server, client);
  registerTeamTools(server);
  registerPlaybookTools(server, client);
  registerBriefSyncTools(server, client);
  registerSkillTools(server, client);
  registerSkillSyncTools(server, client);

  return server;
}
