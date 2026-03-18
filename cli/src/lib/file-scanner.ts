/**
 * File scanner — discovers text files, tracks changes via content hash,
 * and ingests new/changed files into Nex via POST /v1/context/text.
 *
 * Manifest stored at ~/.nex/file-scan-manifest.json.
 */

import { createHash } from "node:crypto";
import { readFileSync, writeFileSync, mkdirSync, statSync, readdirSync } from "node:fs";
import { join, extname, resolve, dirname } from "node:path";
import { homedir } from "node:os";

// --- Types ---

export interface ManifestEntry {
  hash: string;
  size: number;
  scanned_at: string;
}

export interface Manifest {
  version: number;
  files: Record<string, ManifestEntry>;
}

export interface ScanOptions {
  extensions: string[];
  maxFiles: number;
  depth: number;
  force: boolean;
  dryRun: boolean;
}

export interface ScanResult {
  scanned: number;
  skipped: number;
  errors: number;
  files: Array<{
    path: string;
    status: "ingested" | "skipped" | "error";
    reason?: string;
  }>;
}

// --- Constants ---

export const MANIFEST_PATH = join(homedir(), ".nex", "file-scan-manifest.json");

const DEFAULT_EXTENSIONS = [
  // Documents
  ".md", ".txt", ".rtf", ".html", ".htm",
  // Data / config
  ".csv", ".tsv", ".json", ".yaml", ".yml", ".toml", ".xml",
  // Code / scripts
  ".js", ".ts", ".jsx", ".tsx", ".py", ".rb", ".go", ".rs", ".java",
  ".sh", ".bash", ".zsh", ".fish",
  // Markup / notes
  ".org", ".rst", ".adoc", ".tex", ".log",
  // Config / CI
  ".env", ".ini", ".cfg", ".conf", ".properties",
];
const SKIP_DIRS = new Set([
  "node_modules", ".git", "dist", "build", ".next", "__pycache__", ".venv",
  ".cache", ".turbo", "coverage", ".nyc_output",
]);

// --- Config from env ---

export function loadScanConfig(overrides?: Partial<ScanOptions>): ScanOptions {
  const envEnabled = process.env.NEX_SCAN_ENABLED;
  if (envEnabled === "false" || envEnabled === "0") {
    // Caller should check this separately; config still resolves
  }

  const envExts = process.env.NEX_SCAN_EXTENSIONS;
  const extensions = overrides?.extensions
    ?? (envExts ? envExts.split(",").map((e) => e.trim()) : DEFAULT_EXTENSIONS);

  const envMaxFiles = process.env.NEX_SCAN_MAX_FILES;
  const maxFiles = overrides?.maxFiles
    ?? (envMaxFiles ? parseInt(envMaxFiles, 10) : 1000);

  const envDepth = process.env.NEX_SCAN_DEPTH;
  const depth = overrides?.depth
    ?? (envDepth ? parseInt(envDepth, 10) : 20);

  return {
    extensions,
    maxFiles,
    depth,
    force: overrides?.force ?? false,
    dryRun: overrides?.dryRun ?? false,
  };
}

export function isScanEnabled(): boolean {
  const v = process.env.NEX_SCAN_ENABLED;
  return v !== "false" && v !== "0";
}

// --- Manifest ---

export function loadManifest(): Manifest {
  try {
    const raw = readFileSync(MANIFEST_PATH, "utf-8");
    const data = JSON.parse(raw) as Manifest;
    if (data.version === 1 && typeof data.files === "object") return data;
  } catch {
    // missing or corrupt
  }
  return { version: 1, files: {} };
}

export function saveManifest(manifest: Manifest): void {
  mkdirSync(dirname(MANIFEST_PATH), { recursive: true });
  writeFileSync(MANIFEST_PATH, JSON.stringify(manifest, null, 2) + "\n", "utf-8");
}

// --- File discovery ---

interface DiscoveredFile {
  path: string;
  size: number;
  mtime: number;
}

function discoverFiles(
  dir: string,
  extensions: Set<string>,
  maxDepth: number,
  currentDepth = 0,
): DiscoveredFile[] {
  if (currentDepth > maxDepth) return [];

  const results: DiscoveredFile[] = [];
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return results;
  }

  for (const entry of entries) {
    if (entry.startsWith(".") && SKIP_DIRS.has(entry)) continue;
    if (SKIP_DIRS.has(entry)) continue;

    const fullPath = join(dir, entry);
    let stat;
    try {
      stat = statSync(fullPath);
    } catch {
      continue;
    }

    if (stat.isDirectory()) {
      results.push(...discoverFiles(fullPath, extensions, maxDepth, currentDepth + 1));
    } else if (stat.isFile() && extensions.has(extname(entry).toLowerCase())) {
      results.push({ path: fullPath, size: stat.size, mtime: stat.mtimeMs });
    }
  }

  return results;
}

// --- Hashing ---

function hashFile(filePath: string): string {
  const content = readFileSync(filePath);
  return "sha256-" + createHash("sha256").update(content).digest("hex");
}

// --- Concurrency helper ---

const DEFAULT_CONCURRENCY = 5;

async function runConcurrent<T>(
  items: T[],
  concurrency: number,
  fn: (item: T, index: number) => Promise<void>,
): Promise<void> {
  let next = 0;
  async function worker(): Promise<void> {
    while (next < items.length) {
      const idx = next++;
      await fn(items[idx], idx);
    }
  }
  const workers = Array.from({ length: Math.min(concurrency, items.length) }, () => worker());
  await Promise.all(workers);
}

// --- Scanner ---

export async function scanFiles(
  dir: string,
  opts: ScanOptions,
  ingestFn: (content: string, context: string) => Promise<unknown>,
  onProgress?: (current: number, total: number, filePath: string) => void,
): Promise<ScanResult> {
  const absDir = resolve(dir);
  const extSet = new Set(opts.extensions.map((e) => (e.startsWith(".") ? e : `.${e}`).toLowerCase()));

  // Discover files
  const discovered = discoverFiles(absDir, extSet, opts.depth);

  // Sort by mtime descending (newest first), cap at maxFiles
  discovered.sort((a, b) => b.mtime - a.mtime);
  const candidates = discovered.slice(0, opts.maxFiles);

  const manifest = opts.force ? { version: 1, files: {} } as Manifest : loadManifest();
  const result: ScanResult = { scanned: 0, skipped: 0, errors: 0, files: [] };

  // Pre-filter: hash check and dry-run handling (sequential, fast)
  interface IngestJob {
    file: DiscoveredFile;
    hash: string;
    existing: ManifestEntry | undefined;
    content: string;
  }
  const jobs: IngestJob[] = [];

  for (const file of candidates) {
    const hash = hashFile(file.path);
    const existing = manifest.files[file.path];

    if (existing && existing.hash === hash && !opts.force) {
      result.skipped++;
      result.files.push({ path: file.path, status: "skipped", reason: "unchanged" });
      continue;
    }

    if (opts.dryRun) {
      result.scanned++;
      result.files.push({ path: file.path, status: "ingested", reason: existing ? "changed" : "new" });
      continue;
    }

    const content = readFileSync(file.path, "utf-8");
    if (!content.trim()) {
      result.skipped++;
      result.files.push({ path: file.path, status: "skipped", reason: "empty" });
      continue;
    }

    jobs.push({ file, hash, existing, content });
  }

  if (opts.dryRun || jobs.length === 0) {
    if (!opts.dryRun) saveManifest(manifest);
    return result;
  }

  // Ingest concurrently
  // TODO: Replace with server-side batch endpoint (POST /v1/context/batch) once available
  const concurrency = Math.min(DEFAULT_CONCURRENCY, jobs.length);
  let completed = 0;

  await runConcurrent(jobs, concurrency, async (job) => {
    try {
      await ingestFn(job.content, `file-scan:${job.file.path}`);

      manifest.files[job.file.path] = {
        hash: job.hash,
        size: job.file.size,
        scanned_at: new Date().toISOString(),
      };

      result.scanned++;
      result.files.push({ path: job.file.path, status: "ingested", reason: job.existing ? "changed" : "new" });
    } catch (err) {
      result.errors++;
      result.files.push({ path: job.file.path, status: "error", reason: err instanceof Error ? err.message : String(err) });
    }

    completed++;
    onProgress?.(completed, jobs.length, job.file.path);
  });

  saveManifest(manifest);
  return result;
}
