import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { NexApiClient } from "../client.js";
export function registerSkillTools(server, client) {
  server.tool("list_skills", "List all agent skills for the workspace. Skills are executable workflows synthesized from playbook rules, grounded to workspace-specific tools, people, and CRM schema.", {
    limit: z.number().optional().describe("Max results (default: 50)"),
    offset: z.number().optional().describe("Pagination offset (default: 0)")
  }, { readOnlyHint: true }, async ({ limit, offset }) => {
    const params = new URLSearchParams;
    if (limit !== undefined)
      params.set("limit", String(limit));
    if (offset !== undefined)
      params.set("offset", String(offset));
    const qs = params.toString();
    const result = await client.get(`/v1/agent/skills${qs ? `?${qs}` : ""}`);
    return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
  });
  server.tool("get_skill", "Get the full content of an agent skill by ID.", { id: z.string().describe("The skill ID") }, { readOnlyHint: true }, async ({ id }) => {
    const result = await client.get(`/v1/agent/skills/${encodeURIComponent(id)}`);
    return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
  });
  server.tool("get_skill_by_slug", "Get an agent skill by its slug name (e.g., 'jolt-indecision-recovery').", { slug: z.string().describe("The skill slug") }, { readOnlyHint: true }, async ({ slug }) => {
    const result = await client.get(`/v1/agent/skills/by-slug/${encodeURIComponent(slug)}`);
    return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
  });
  server.tool("download_skill", "Download an agent skill as raw markdown. Includes YAML frontmatter with trigger, confidence, tools, and Composio actions.", { id: z.string().describe("The skill ID") }, { readOnlyHint: true }, async ({ id }) => {
    const result = await client.getRaw(`/v1/agent/skills/${encodeURIComponent(id)}/download`);
    return { content: [{ type: "text", text: result }] };
  });
  server.tool("compile_skills", "Trigger skill compilation for the workspace. Scans playbook rules and generates executable skills grounded to workspace context. Runs automatically after playbook synthesis on cron.", {
    force: z.boolean().optional().describe("If true, recompile all skills even if unchanged")
  }, { readOnlyHint: false }, async ({ force }) => {
    const body = {};
    if (force)
      body.force = true;
    const result = await client.post("/v1/agent/skills/compile", body);
    return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
  });
}
