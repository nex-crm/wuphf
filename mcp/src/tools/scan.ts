/**
 * MCP tool: scan_project_files — discover and ingest text files from a directory.
 */

import { createHash } from "node:crypto";
import { readFileSync, writeFileSync, mkdirSync, statSync, readdirSync } from "node:fs";
import { join, extname, resolve, dirname } from "node:path";
import { homedir } from "node:os";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { NexApiClient } from "../client.js";

// --- Manifest ---

interface ManifestEntry {
  hash: string;
  size: number;
  scanned_at: string;
}

interface Manifest {
  version: number;
  files: Record<string, ManifestEntry>;
}

const MANIFEST_PATH = join(homedir(), ".nex", "file-scan-manifest.json");

const DEFAULT_EXTENSIONS = [
  ".md", ".txt", ".rtf", ".html", ".htm",
  ".csv", ".tsv", ".json", ".yaml", ".yml", ".toml", ".xml",
  ".js", ".ts", ".jsx", ".tsx", ".py", ".rb", ".go", ".rs", ".java",
  ".sh", ".bash", ".zsh", ".fish",
  ".org", ".rst", ".adoc", ".tex", ".log",
  ".env", ".ini", ".cfg", ".conf", ".properties",
];
const SKIP_DIRS = new Set([
  "node_modules", ".git", "dist", "build", ".next", "__pycache__", ".venv",
  ".cache", ".turbo", "coverage", ".nyc_output",
]);

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

// --- File discovery ---

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

// --- Tool registration ---

export function registerScanTools(server: McpServer, client: NexApiClient) {
  server.tool(
    "scan_project_files",
    "Scan a directory for text files (.md, .txt, .csv, .json, .yaml, .yml) and ingest new or changed files into Nex. Uses SHA-256 content hashing to skip already-ingested files. Manifest stored at ~/.nex/file-scan-manifest.json.",
    {
      dir: z.string().describe("Directory path to scan"),
      extensions: z.string().optional().describe("Comma-separated file extensions (default: .md,.txt,.csv,.json,.yaml,.yml)"),
      max_files: z.number().optional().describe("Maximum files to scan per run (default: 5)"),
      depth: z.number().optional().describe("Maximum directory depth (default: 2)"),
      force: z.boolean().optional().describe("Re-scan all files ignoring manifest (default: false)"),
    },
    { readOnlyHint: false },
    async ({ dir, extensions, max_files, depth, force }) => {
      const absDir = resolve(dir);
      const extList = extensions
        ? extensions.split(",").map((e) => (e.trim().startsWith(".") ? e.trim() : `.${e.trim()}`).toLowerCase())
        : DEFAULT_EXTENSIONS;
      const extSet = new Set(extList);
      const maxFiles = max_files ?? 5;
      const maxDepth = depth ?? 20;

      const discovered = discoverFiles(absDir, extSet, maxDepth);
      discovered.sort((a, b) => b.mtime - a.mtime);
      const candidates = discovered.slice(0, maxFiles);

      const manifest = force ? { version: 1, files: {} } as Manifest : loadManifest();
      const results: Array<{ path: string; status: string; reason?: string }> = [];
      let scanned = 0;
      let skipped = 0;

      for (const file of candidates) {
        const hash = hashFile(file.path);
        const existing = manifest.files[file.path];

        if (existing && existing.hash === hash && !force) {
          skipped++;
          results.push({ path: file.path, status: "skipped", reason: "unchanged" });
          continue;
        }

        try {
          const content = readFileSync(file.path, "utf-8");
          if (!content.trim()) {
            skipped++;
            results.push({ path: file.path, status: "skipped", reason: "empty" });
            continue;
          }

          await client.post("/v1/context/text", { content, context: `file-scan:${file.path}` });

          manifest.files[file.path] = {
            hash,
            size: file.size,
            scanned_at: new Date().toISOString(),
          };

          scanned++;
          results.push({ path: file.path, status: "ingested", reason: existing ? "changed" : "new" });
        } catch (err) {
          results.push({ path: file.path, status: "error", reason: err instanceof Error ? err.message : String(err) });
        }
      }

      saveManifest(manifest);

      const summary = { scanned, skipped, total_discovered: discovered.length, files: results };
      return { content: [{ type: "text", text: JSON.stringify(summary, null, 2) }] };
    },
  );
}
