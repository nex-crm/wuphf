/**
 * nex scan [dir] — Scan directory for text files and ingest into Nex.
 */

import { program } from "../cli.js";
import { resolveApiKey, resolveFormat, resolveTimeout } from "../lib/config.js";
import { NexClient } from "../lib/client.js";
import { printOutput, printError } from "../lib/output.js";
import { scanFiles, loadScanConfig, isScanEnabled } from "../lib/file-scanner.js";
import type { Format } from "../lib/output.js";
import { spinner as createSpinner, table, style, sym, isTTY } from "../lib/tui.js";

interface ScanFileEntry {
  path?: string;
  status?: string;
  [k: string]: unknown;
}

interface ScanOutput {
  scanned?: number;
  skipped?: number;
  errors?: number;
  files?: ScanFileEntry[];
  dry_run?: boolean;
  would_scan?: number;
  would_skip?: number;
  [k: string]: unknown;
}

function formatScanTTY(data: unknown): string | undefined {
  const d = data as ScanOutput;
  if (!d || typeof d !== "object") return undefined;

  const lines: string[] = [];

  if (d.dry_run) {
    lines.push(`  ${sym.info} Dry run — no files ingested`);
    lines.push(`  Would scan: ${d.would_scan ?? 0}, Would skip: ${d.would_skip ?? 0}`);
  }

  const files = d.files;
  if (files && Array.isArray(files) && files.length > 0) {
    const headers = ["FILE", "STATUS"];
    const rows = files.map((f) => [
      String(f.path ?? ""),
      String(f.status ?? ""),
    ]);
    lines.push(table({ headers, rows }));
    lines.push("");
  }

  if (!d.dry_run) {
    const parts: string[] = [];
    if (d.scanned) parts.push(`${d.scanned} ingested`);
    if (d.skipped) parts.push(`${d.skipped} skipped`);
    if (d.errors) parts.push(`${d.errors} errors`);
    if (parts.length > 0) {
      lines.push(`  ${style.dim(parts.join(", "))}`);
    }
  }

  return lines.length > 0 ? lines.join("\n") : undefined;
}

program
  .command("scan")
  .description("Scan a directory for text files and ingest new/changed files into Nex")
  .argument("[dir]", "Directory to scan (default: current directory)", ".")
  .option("--extensions <exts>", "File extensions to scan (comma-separated)")
  .option("--max-files <n>", "Maximum files per scan run")
  .option("--depth <n>", "Maximum directory depth")
  .option("--force", "Re-scan all files (ignore manifest)")
  .option("--dry-run", "Show what would be scanned without ingesting")
  .action(
    async (
      dir: string,
      opts: {
        extensions?: string;
        maxFiles?: string;
        depth?: string;
        force?: boolean;
        dryRun?: boolean;
      },
    ) => {
      const globalOpts = program.opts();
      const apiKey = resolveApiKey(globalOpts.apiKey);
      const format = resolveFormat(globalOpts.format) as Format;

      if (!opts.dryRun && !apiKey) {
        printError("No API key. Run 'nex register --email <email>' first or set NEX_API_KEY.");
        process.exit(2);
      }

      if (!isScanEnabled()) {
        printError("File scanning is disabled (NEX_SCAN_ENABLED=false).");
        process.exit(0);
      }

      const scanOpts = loadScanConfig({
        extensions: opts.extensions?.split(",").map((e) => e.trim()),
        maxFiles: opts.maxFiles ? parseInt(opts.maxFiles, 10) : undefined,
        depth: opts.depth ? parseInt(opts.depth, 10) : undefined,
        force: opts.force,
        dryRun: opts.dryRun,
      });

      const client = new NexClient(apiKey, resolveTimeout(globalOpts.timeout));

      // Show spinner for TTY text output
      const showSpinner = isTTY && format === "text" && !opts.dryRun;
      const spin = showSpinner ? createSpinner("Scanning project files...") : null;

      try {
        const result = await scanFiles(dir, scanOpts, async (content, context) => {
          return client.post("/v1/context/text", { content, context });
        });

        if (spin) {
          spin.succeed("Scan complete");
          process.stderr.write("\n");
        }

        if (opts.dryRun) {
          printOutput(
            {
              dry_run: true,
              would_scan: result.scanned,
              would_skip: result.skipped,
              files: result.files,
            },
            format,
            formatScanTTY,
          );
        } else {
          printOutput(
            {
              scanned: result.scanned,
              skipped: result.skipped,
              errors: result.errors,
              files: result.files,
            },
            format,
            formatScanTTY,
          );
        }
      } catch (err) {
        if (spin) {
          spin.fail("Scan failed");
        }
        throw err;
      }
    },
  );
