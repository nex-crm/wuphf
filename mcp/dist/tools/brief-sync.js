import { z } from "zod";
import { existsSync, mkdirSync, writeFileSync, readFileSync } from "node:fs";
import { join } from "node:path";
const NEX_DIR = ".nex";
const BRIEFS_DIR = join(NEX_DIR, "briefs");
const ENTITY_DIR = join(BRIEFS_DIR, "entities");
const WORKSPACE_DIR = join(BRIEFS_DIR, "workspace");
const PRIVATE_DIR = join(BRIEFS_DIR, "private");
const SYNC_STATE_FILE = join(NEX_DIR, ".sync-state.json");
function ensureDirs() {
    for (const dir of [NEX_DIR, BRIEFS_DIR, ENTITY_DIR, WORKSPACE_DIR, PRIVATE_DIR]) {
        if (!existsSync(dir))
            mkdirSync(dir, { recursive: true });
    }
}
function loadSyncState() {
    if (existsSync(SYNC_STATE_FILE)) {
        try {
            return JSON.parse(readFileSync(SYNC_STATE_FILE, "utf-8"));
        }
        catch {
            // corrupt state, start fresh
        }
    }
    return { last_sync: "", briefs: {} };
}
function saveSyncState(state) {
    writeFileSync(SYNC_STATE_FILE, JSON.stringify(state, null, 2));
}
function slugify(title) {
    return title
        .toLowerCase()
        .replace(/[^a-z0-9\s-]/g, "")
        .replace(/\s+/g, "-")
        .replace(/-+/g, "-")
        .slice(0, 80);
}
export function registerBriefSyncTools(server, client) {
    server.tool("sync_briefs", "Sync all entity briefs, workspace playbooks, and private playbooks to the local .nex/briefs/ folder. Downloads new and updated briefs as .md files. Local agents can read these files directly instead of making API calls. Run this periodically or after data changes.", {
        force: z.boolean().optional().describe("If true, re-download all briefs even if unchanged"),
    }, { readOnlyHint: false }, async ({ force }) => {
        ensureDirs();
        const state = force ? { last_sync: "", briefs: {} } : loadSyncState();
        // Fetch all briefs from API
        const allBriefs = await client.get("/v1/playbooks?limit=200");
        let downloaded = 0;
        let skipped = 0;
        let total = allBriefs.data?.length ?? 0;
        for (const brief of allBriefs.data ?? []) {
            const existing = state.briefs[brief.id];
            // Skip if hash matches (no changes)
            if (existing && existing.input_hash === brief.input_hash && !force) {
                skipped++;
                continue;
            }
            // Download the full markdown
            try {
                const content = await client.getRaw(`/v1/playbooks/${brief.id}/download`);
                // Determine file path based on scope_type
                let dir;
                let filename;
                if (brief.scope_type === 2) {
                    // Workspace playbook
                    dir = WORKSPACE_DIR;
                    filename = `${brief.slug || slugify(brief.title)}.md`;
                }
                else if (brief.scope_type === 3) {
                    // Private playbook
                    dir = PRIVATE_DIR;
                    filename = `${slugify(brief.title)}.md`;
                }
                else {
                    // Entity brief
                    dir = ENTITY_DIR;
                    filename = `${slugify(brief.title)}.md`;
                }
                const filepath = join(dir, filename);
                writeFileSync(filepath, content);
                state.briefs[brief.id] = {
                    id: brief.id,
                    input_hash: brief.input_hash ?? "",
                    updated_at: brief.updated_at,
                    file: filepath,
                };
                downloaded++;
            }
            catch (e) {
                // Skip individual failures
                continue;
            }
        }
        state.last_sync = new Date().toISOString();
        saveSyncState(state);
        const summary = [
            `Synced ${total} briefs to .nex/briefs/`,
            `  Downloaded: ${downloaded} (new/updated)`,
            `  Skipped: ${skipped} (unchanged)`,
            `  Entity briefs: ${ENTITY_DIR}/`,
            `  Workspace playbooks: ${WORKSPACE_DIR}/`,
            `  Private playbooks: ${PRIVATE_DIR}/`,
        ].join("\n");
        return { content: [{ type: "text", text: summary }] };
    });
    server.tool("read_brief", "Read a brief from the local .nex/briefs/ folder. Falls back to API if not synced locally. Faster than API calls for local agents.", {
        title: z.string().describe("Brief title or partial match (e.g., 'lenny', 'airbnb', 'b2b sales')"),
    }, { readOnlyHint: true }, async ({ title }) => {
        ensureDirs();
        const state = loadSyncState();
        const searchTerm = title.toLowerCase();
        // Search local files first
        for (const [, entry] of Object.entries(state.briefs)) {
            if (entry.file && entry.file.toLowerCase().includes(searchTerm)) {
                try {
                    const content = readFileSync(entry.file, "utf-8");
                    return { content: [{ type: "text", text: content }] };
                }
                catch {
                    // File missing, fall through to API
                }
            }
        }
        // Fallback: search via API
        const result = await client.get("/v1/playbooks?limit=50");
        const match = (result.data ?? []).find(b => b.title.toLowerCase().includes(searchTerm));
        if (match) {
            const content = await client.getRaw(`/v1/playbooks/${match.id}/download`);
            return { content: [{ type: "text", text: content }] };
        }
        return { content: [{ type: "text", text: `No brief found matching "${title}". Run sync_briefs first.` }] };
    });
}
//# sourceMappingURL=brief-sync.js.map