/**
 * nex scan [dir] — Scan directory for text files and ingest into Nex.
 */

import { program } from "../cli.js";
import { resolveApiKey, resolveFormat, resolveTimeout } from "../lib/config.js";
import { NexClient } from "../lib/client.js";
import { printOutput, printError } from "../lib/output.js";
import { scanFiles, loadScanConfig, isScanEnabled } from "../lib/file-scanner.js";
import type { Format } from "../lib/output.js";

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

      const result = await scanFiles(dir, scanOpts, async (content, context) => {
        return client.post("/v1/context/text", { content, context });
      });

      if (opts.dryRun) {
        printOutput(
          {
            dry_run: true,
            would_scan: result.scanned,
            would_skip: result.skipped,
            files: result.files,
          },
          format,
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
        );
      }
    },
  );
