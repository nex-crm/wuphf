/**
 * Core file scanner — walks project directories, detects changed files,
 * and ingests them into Nex via the developer API.
 *
 * No new dependencies — uses only Node.js built-ins.
 */

import { readdirSync, statSync, readFileSync } from "node:fs";
import { join, relative, extname } from "node:path";
import type { Stats } from "node:fs";
import type { NexClient } from "./nex-client.js";
import type { RateLimiter } from "./rate-limiter.js";
import type { ScanConfig } from "./config.js";
import { readManifest, writeManifest, isChanged, markIngested } from "./file-manifest.js";

export interface ScanResult {
  scanned: number;
  ingested: number;
  skipped: number;
  errors: number;
}

interface CandidateFile {
  absolutePath: string;
  relativePath: string;
  stat: Stats;
}

/**
 * Recursively collect candidate files up to scanDepth levels.
 */
function walkDir(
  dir: string,
  cwd: string,
  config: ScanConfig,
  depth: number,
  results: CandidateFile[]
): void {
  if (depth > config.scanDepth) return;

  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return; // Permission denied or missing — skip silently
  }

  for (const entry of entries) {
    const fullPath = join(dir, entry.name);

    if (entry.isDirectory()) {
      if (config.ignoreDirs.includes(entry.name)) continue;
      walkDir(fullPath, cwd, config, depth + 1, results);
    } else if (entry.isFile()) {
      const ext = extname(entry.name).toLowerCase();
      if (!config.extensions.includes(ext)) continue;

      try {
        const stat = statSync(fullPath);
        results.push({
          absolutePath: fullPath,
          relativePath: relative(cwd, fullPath),
          stat,
        });
      } catch {
        // stat failed — skip
      }
    }
  }
}

/**
 * Scan project directory for text files and ingest changed ones into Nex.
 */
export async function scanAndIngest(
  client: NexClient,
  rateLimiter: RateLimiter,
  cwd: string,
  config: ScanConfig
): Promise<ScanResult> {
  const result: ScanResult = { scanned: 0, ingested: 0, skipped: 0, errors: 0 };

  if (!config.enabled) return result;

  const manifest = readManifest();
  const candidates: CandidateFile[] = [];
  walkDir(cwd, cwd, config, 0, candidates);

  result.scanned = candidates.length;

  // Filter to changed files, sort by mtime descending (newest first)
  const changed = candidates
    .filter((f) => isChanged(f.absolutePath, f.stat, manifest))
    .sort((a, b) => b.stat.mtimeMs - a.stat.mtimeMs)
    .slice(0, config.maxFilesPerScan);

  result.skipped = candidates.length - changed.length;

  for (const file of changed) {
    if (!rateLimiter.canProceed()) {
      process.stderr.write(`[nex-scan] Rate limited — stopping after ${result.ingested} files\n`);
      result.skipped += changed.length - result.ingested - result.errors;
      break;
    }

    try {
      let content = readFileSync(file.absolutePath, "utf-8");

      // Truncate large files
      if (content.length > config.maxFileSize) {
        content = content.slice(0, config.maxFileSize) + "\n[...truncated]";
      }

      const context = `file-scan:${file.relativePath}`;
      await client.ingest(content, context, 10_000);
      markIngested(file.absolutePath, file.stat, context, manifest);
      result.ingested++;
    } catch (err) {
      process.stderr.write(
        `[nex-scan] Failed to ingest ${file.relativePath}: ${err instanceof Error ? err.message : String(err)}\n`
      );
      result.errors++;
    }
  }

  writeManifest(manifest);
  return result;
}
