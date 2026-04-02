import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { NexApiClient } from "../client.js";
import { existsSync, mkdirSync, writeFileSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
const NEX_DIR = ".nex";
const SKILLS_DIR = join(NEX_DIR, "skills");
const SYNC_STATE_FILE = join(NEX_DIR, ".skill-sync-state.json");
function ensureDirs() {
  for (const dir of [NEX_DIR, SKILLS_DIR]) {
    if (!existsSync(dir))
      mkdirSync(dir, { recursive: true });
  }
}
function loadSyncState() {
  if (existsSync(SYNC_STATE_FILE)) {
    try {
      return JSON.parse(readFileSync(SYNC_STATE_FILE, "utf-8"));
    } catch {}
  }
  return { last_sync: "", skills: {} };
}
function saveSyncState(state) {
  writeFileSync(SYNC_STATE_FILE, JSON.stringify(state, null, 2));
}
export function registerSkillSyncTools(server, client) {
  server.tool("sync_skills", "Sync all agent skills to the local .nex/skills/ folder as markdown files. Local agents can read these directly instead of making API calls. Run after skill compilation or periodically.", {
    force: z.boolean().optional().describe("If true, re-download all skills even if unchanged")
  }, { readOnlyHint: false }, async ({ force }) => {
    ensureDirs();
    const state = force ? { last_sync: "", skills: {} } : loadSyncState();
    const allSkills = await client.get("/v1/agent/skills?limit=200");
    let downloaded = 0;
    let skipped = 0;
    const total = allSkills.data?.length ?? 0;
    for (const skill of allSkills.data ?? []) {
      const existing = state.skills[skill.id];
      if (existing && existing.updated_at === skill.updated_at && !force) {
        skipped++;
        continue;
      }
      try {
        const content = await client.getRaw(`/v1/agent/skills/${skill.id}/download`);
        const filename = `${skill.slug || skill.id}.md`;
        const filepath = join(SKILLS_DIR, filename);
        writeFileSync(filepath, content);
        state.skills[skill.id] = {
          id: skill.id,
          slug: skill.slug,
          updated_at: skill.updated_at,
          file: filepath
        };
        downloaded++;
      } catch {
        continue;
      }
    }
    state.last_sync = new Date().toISOString();
    saveSyncState(state);
    const summary = [
      `Synced ${total} skills to .nex/skills/`,
      `  Downloaded: ${downloaded} (new/updated)`,
      `  Skipped: ${skipped} (unchanged)`,
      `  Location: ${SKILLS_DIR}/`
    ].join(`
`);
    return { content: [{ type: "text", text: summary }] };
  });
  server.tool("read_skill", "Read a skill from the local .nex/skills/ folder by slug or partial name. Falls back to API if not synced locally.", {
    slug: z.string().describe("Skill slug or partial match (e.g., 'jolt', 'pitch', 'accelerate')")
  }, { readOnlyHint: true }, async ({ slug }) => {
    ensureDirs();
    const searchTerm = slug.toLowerCase();
    const exactPath = join(SKILLS_DIR, `${searchTerm}.md`);
    if (existsSync(exactPath)) {
      const content = readFileSync(exactPath, "utf-8");
      return { content: [{ type: "text", text: content }] };
    }
    if (existsSync(SKILLS_DIR)) {
      const files = readdirSync(SKILLS_DIR).filter((f) => f.endsWith(".md"));
      const match = files.find((f) => f.toLowerCase().includes(searchTerm));
      if (match) {
        const content = readFileSync(join(SKILLS_DIR, match), "utf-8");
        return { content: [{ type: "text", text: content }] };
      }
    }
    try {
      const result = await client.get(`/v1/agent/skills/by-slug/${encodeURIComponent(searchTerm)}`);
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    } catch {
      const list = await client.get("/v1/agent/skills?limit=50");
      const apiMatch = (list.data ?? []).find((s) => s.slug?.toLowerCase().includes(searchTerm) || s.title?.toLowerCase().includes(searchTerm));
      if (apiMatch) {
        const content = await client.getRaw(`/v1/agent/skills/${apiMatch.id}/download`);
        return { content: [{ type: "text", text: content }] };
      }
    }
    return { content: [{ type: "text", text: `No skill found matching "${slug}". Run sync_skills first or check available skills with list_skills.` }] };
  });
}
