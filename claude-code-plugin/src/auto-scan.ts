#!/usr/bin/env node
/**
 * Standalone entry point for the /scan slash command.
 *
 * Reads optional directory from argv[2] or stdin, scans for project files,
 * and ingests changed ones into WUPHF. Prints results to stdout.
 *
 * Usage: node dist/auto-scan.js [directory]
 */

import { loadConfig, loadScanConfig } from "./config.js";
import { NexClient } from "./wuphf-client.js";
import { RateLimiter } from "./rate-limiter.js";
import { scanAndIngest } from "./file-scanner.js";

async function main(): Promise<void> {
  try {
    // Determine target directory: argv[2] > cwd
    const targetDir = process.argv[2] || process.cwd();

    let cfg;
    try {
      cfg = loadConfig();
    } catch (err) {
      console.error(`Config error: ${err instanceof Error ? err.message : String(err)}`);
      process.exit(1);
    }

    const scanConfig = loadScanConfig();
    if (!scanConfig.enabled) {
      console.log("File scanning is disabled (WUPHF_SCAN_ENABLED=false).");
      return;
    }

    const client = new NexClient(cfg.apiKey, cfg.baseUrl);
    const rateLimiter = new RateLimiter();

    console.log(`Scanning ${targetDir} ...`);
    const result = await scanAndIngest(client, rateLimiter, targetDir, scanConfig);

    console.log(`Scan complete:`);
    console.log(`  Scanned: ${result.scanned} files`);
    console.log(`  Ingested: ${result.ingested} files`);
    console.log(`  Skipped: ${result.skipped} files (unchanged)`);
    if (result.errors > 0) {
      console.log(`  Errors: ${result.errors}`);
    }
  } catch (err) {
    console.error(`Scan failed: ${err instanceof Error ? err.message : String(err)}`);
    process.exit(1);
  }
}

main();
