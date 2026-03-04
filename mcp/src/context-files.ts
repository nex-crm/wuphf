/**
 * Ingests Claude Code context files (CLAUDE.md + memory files) into Nex.
 *
 * Reads from both global and project-level locations:
 * - ~/.claude/CLAUDE.md (global instructions)
 * - {cwd}/CLAUDE.md (project instructions)
 * - ~/.claude/projects/{project-key}/memory/*.md (memory files)
 *
 * Uses the file manifest for change detection — unchanged files are skipped.
 */
import { existsSync, readdirSync, statSync, readFileSync } from "node:fs";
import { join, extname, basename } from "node:path";
import { homedir } from "node:os";
import { readManifest, writeManifest, isChanged, markIngested } from "./file-manifest.js";
import type { NexApiClient } from "./client.js";
import type { RateLimiter } from "./rate-limiter.js";

const CLAUDE_DIR = join(homedir(), ".claude");
const INGEST_TIMEOUT_MS = 10_000;
const MAX_FILE_SIZE = 100_000;

export interface ContextFilesResult {
  ingested: number;
  skipped: number;
  errors: number;
  files: string[];
}

function projectKey(cwd: string): string {
  return cwd.replace(/\//g, "-");
}

function collectContextFiles(cwd: string): Array<{ path: string; contextTag: string }> {
  const files: Array<{ path: string; contextTag: string }> = [];
  const key = projectKey(cwd);

  const globalClaude = join(CLAUDE_DIR, "CLAUDE.md");
  if (existsSync(globalClaude)) {
    files.push({ path: globalClaude, contextTag: "claude-md:global" });
  }

  const projectClaude = join(cwd, "CLAUDE.md");
  if (existsSync(projectClaude)) {
    files.push({ path: projectClaude, contextTag: "claude-md:project" });
  }

  const memoryDir = join(CLAUDE_DIR, "projects", key, "memory");
  if (existsSync(memoryDir)) {
    try {
      const entries = readdirSync(memoryDir, { withFileTypes: true });
      for (const entry of entries) {
        if (!entry.isFile()) continue;
        if (extname(entry.name).toLowerCase() !== ".md") continue;
        const fullPath = join(memoryDir, entry.name);
        const name = basename(entry.name, ".md");
        files.push({ path: fullPath, contextTag: `claude-memory:${name}` });
      }
    } catch {
      // memoryDir unreadable — skip
    }
  }

  return files;
}

export async function ingestContextFiles(
  client: NexApiClient,
  rateLimiter: RateLimiter,
  cwd: string,
): Promise<ContextFilesResult> {
  const result: ContextFilesResult = { ingested: 0, skipped: 0, errors: 0, files: [] };
  const manifest = readManifest();
  const candidates = collectContextFiles(cwd);
  let dirty = false;

  for (const { path, contextTag } of candidates) {
    try {
      const stat = statSync(path);
      if (!isChanged(path, stat, manifest)) {
        result.skipped++;
        continue;
      }
      if (!rateLimiter.canProceed()) {
        process.stderr.write("[nex-context-files] Rate limited — stopping context file ingest\n");
        result.skipped += candidates.length - result.ingested - result.skipped - result.errors;
        break;
      }
      let content = readFileSync(path, "utf-8");
      if (content.length > MAX_FILE_SIZE) {
        content = content.slice(0, MAX_FILE_SIZE) + "\n[...truncated]";
      }
      await client.post("/v1/context/text", { content, context: contextTag });
      rateLimiter.recordRequest();
      markIngested(path, stat, contextTag, manifest);
      result.ingested++;
      result.files.push(contextTag);
      dirty = true;
    } catch (err) {
      process.stderr.write(`[nex-context-files] Failed to ingest ${contextTag}: ${err instanceof Error ? err.message : String(err)}\n`);
      result.errors++;
    }
  }

  if (dirty) {
    writeManifest(manifest);
  }
  return result;
}
