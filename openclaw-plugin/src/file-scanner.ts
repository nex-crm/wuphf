/**
 * File scanner for OpenClaw plugin.
 * Discovers text files, tracks changes via SHA-256 content hash,
 * and ingests new/changed files into Nex.
 */

import { createHash } from "node:crypto";
import { readFileSync, writeFileSync, mkdirSync, statSync, readdirSync } from "node:fs";
import { join, extname, resolve, dirname } from "node:path";
import { homedir } from "node:os";
import { NexClient, type IngestResponse } from "./nex-client.js";

// --- Types ---

interface ManifestEntry {
  hash: string;
  size: number;
  scanned_at: string;
}

interface Manifest {
  version: number;
  files: Record<string, ManifestEntry>;
}

export interface ScanResult {
  scanned: number;
  skipped: number;
  errors: number;
  files: Array<{ path: string; status: string; reason?: string }>;
}

// --- Constants ---

const MANIFEST_PATH = join(homedir(), ".nex", "file-scan-manifest.json");
const DEFAULT_EXTENSIONS = [".md", ".txt", ".csv", ".json", ".yaml", ".yml"];
const SKIP_DIRS = new Set([
  "node_modules", ".git", "dist", "build", ".next", "__pycache__", ".venv",
  ".cache", ".turbo", "coverage", ".nyc_output",
]);

// --- Manifest ---

function loadManifest(): Manifest {
  try {
    const raw = readFileSync(MANIFEST_PATH, "utf-8");
    const data = JSON.parse(raw) as Manifest;
    if (data.version === 1 && typeof data.files === "object") return data;
  } catch { /* missing or corrupt */ }
  return { version: 1, files: {} };
}

function saveManifest(manifest: Manifest): void {
  mkdirSync(dirname(MANIFEST_PATH), { recursive: true });
  writeFileSync(MANIFEST_PATH, JSON.stringify(manifest, null, 2) + "\n", "utf-8");
}

// --- Discovery ---

interface DiscoveredFile {
  path: string;
  size: number;
  mtime: number;
}

function discoverFiles(dir: string, extensions: Set<string>, maxDepth: number, depth = 0): DiscoveredFile[] {
  if (depth > maxDepth) return [];
  const results: DiscoveredFile[] = [];
  let entries: string[];
  try { entries = readdirSync(dir); } catch { return results; }

  for (const entry of entries) {
    if (SKIP_DIRS.has(entry)) continue;
    const fullPath = join(dir, entry);
    let stat;
    try { stat = statSync(fullPath); } catch { continue; }

    if (stat.isDirectory()) {
      results.push(...discoverFiles(fullPath, extensions, maxDepth, depth + 1));
    } else if (stat.isFile() && extensions.has(extname(entry).toLowerCase())) {
      results.push({ path: fullPath, size: stat.size, mtime: stat.mtimeMs });
    }
  }
  return results;
}

function hashFile(filePath: string): string {
  const content = readFileSync(filePath);
  return "sha256-" + createHash("sha256").update(content).digest("hex");
}

// --- Scanner ---

export async function scanFiles(
  dir: string,
  client: NexClient,
  opts?: {
    extensions?: string[];
    maxFiles?: number;
    depth?: number;
    force?: boolean;
  },
): Promise<ScanResult> {
  const absDir = resolve(dir);
  const extensions = opts?.extensions ?? DEFAULT_EXTENSIONS;
  const extSet = new Set(extensions.map((e) => (e.startsWith(".") ? e : `.${e}`).toLowerCase()));
  const maxFiles = opts?.maxFiles ?? 5;
  const maxDepth = opts?.depth ?? 2;
  const force = opts?.force ?? false;

  const discovered = discoverFiles(absDir, extSet, maxDepth);
  discovered.sort((a, b) => b.mtime - a.mtime);
  const candidates = discovered.slice(0, maxFiles);

  const manifest = force ? { version: 1, files: {} } as Manifest : loadManifest();
  const result: ScanResult = { scanned: 0, skipped: 0, errors: 0, files: [] };

  for (const file of candidates) {
    const hash = hashFile(file.path);
    const existing = manifest.files[file.path];

    if (existing && existing.hash === hash && !force) {
      result.skipped++;
      result.files.push({ path: file.path, status: "skipped", reason: "unchanged" });
      continue;
    }

    try {
      const content = readFileSync(file.path, "utf-8");
      if (!content.trim()) {
        result.skipped++;
        result.files.push({ path: file.path, status: "skipped", reason: "empty" });
        continue;
      }

      await client.ingest(content, `file-scan:${file.path}`);

      manifest.files[file.path] = {
        hash,
        size: file.size,
        scanned_at: new Date().toISOString(),
      };

      result.scanned++;
      result.files.push({ path: file.path, status: "ingested", reason: existing ? "changed" : "new" });
    } catch (err) {
      result.errors++;
      result.files.push({ path: file.path, status: "error", reason: err instanceof Error ? err.message : String(err) });
    }
  }

  saveManifest(manifest);
  return result;
}
