/**
 * Persistent file manifest — tracks which files have been ingested
 * using mtime + size as change detection.
 *
 * Stored at ~/.wuphf/file-scan-manifest.json.
 */
import { readFileSync, writeFileSync, mkdirSync, type Stats } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

const DATA_DIR = join(homedir(), ".wuphf");
const MANIFEST_PATH = join(DATA_DIR, "file-scan-manifest.json");

export interface FileManifestEntry {
  mtime: number;
  size: number;
  ingestedAt: number;
  context: string;
}

export interface FileManifest {
  version: 1;
  files: Record<string, FileManifestEntry>;
}

export function readManifest(): FileManifest {
  try {
    const raw = readFileSync(MANIFEST_PATH, "utf-8");
    const data = JSON.parse(raw);
    if (data && data.version === 1 && data.files) {
      return data;
    }
    return { version: 1, files: {} };
  } catch {
    return { version: 1, files: {} };
  }
}

export function writeManifest(manifest: FileManifest): void {
  try {
    mkdirSync(DATA_DIR, { recursive: true });
    writeFileSync(MANIFEST_PATH, JSON.stringify(manifest, null, 2), "utf-8");
  } catch {
    // Best-effort — if we can't write, next scan re-ingests
  }
}

export function isChanged(path: string, stat: Stats, manifest: FileManifest): boolean {
  const entry = manifest.files[path];
  if (!entry) return true;
  return entry.mtime !== stat.mtimeMs || entry.size !== stat.size;
}

export function markIngested(path: string, stat: Stats, context: string, manifest: FileManifest): void {
  manifest.files[path] = {
    mtime: stat.mtimeMs,
    size: stat.size,
    ingestedAt: Date.now(),
    context,
  };
}
