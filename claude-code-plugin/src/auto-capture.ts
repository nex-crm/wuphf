#!/usr/bin/env node
/**
 * Claude Code Stop hook — auto-capture conversation to WUPHF + plan file ingestion.
 *
 * Reads { last_assistant_message, session_id } from stdin,
 * filters and sends to WUPHF for ingestion. Also checks .claude/plans/
 * for changed plan files and ingests up to 2.
 *
 * On ANY error: outputs {} and exits 0 (graceful degradation).
 */

import { readdirSync, statSync, readFileSync } from "node:fs";
import { join, extname } from "node:path";
import { loadConfig, loadScanConfig, isHookEnabled } from "./config.js";
import { NexClient } from "./wuphf-client.js";
import { captureFilter } from "./capture-filter.js";
import { RateLimiter } from "./rate-limiter.js";
import { readManifest, writeManifest, isChanged, markIngested } from "./file-manifest.js";

/** Ingest timeout — 3s leaves buffer within hook timeout */
const INGEST_TIMEOUT_MS = 3_000;
const MAX_PLAN_FILES = 2;

const rateLimiter = new RateLimiter();

interface HookInput {
  last_assistant_message?: string;
  session_id?: string;
}

/**
 * Scan .claude/plans/ for changed .md files and ingest up to MAX_PLAN_FILES.
 */
async function ingestPlanFiles(client: NexClient): Promise<void> {
  const scanConfig = loadScanConfig();
  if (!scanConfig.enabled) return;

  const plansDir = join(process.cwd(), ".claude", "plans");

  let entries;
  try {
    entries = readdirSync(plansDir, { withFileTypes: true });
  } catch {
    return; // No .claude/plans/ dir — normal, skip silently
  }

  const manifest = readManifest();
  let ingested = 0;

  for (const entry of entries) {
    if (ingested >= MAX_PLAN_FILES) break;
    if (!entry.isFile()) continue;
    if (extname(entry.name).toLowerCase() !== ".md") continue;

    const fullPath = join(plansDir, entry.name);
    try {
      const stat = statSync(fullPath);
      if (!isChanged(fullPath, stat, manifest)) continue;

      if (!rateLimiter.canProceed()) {
        process.stderr.write("[wuphf-capture] Rate limited — skipping plan file ingest\n");
        break;
      }

      let content = readFileSync(fullPath, "utf-8");
      if (content.length > 100_000) {
        content = content.slice(0, 100_000) + "\n[...truncated]";
      }

      const context = `claude-code-plan:${entry.name}`;
      await client.ingest(content, context, INGEST_TIMEOUT_MS);
      markIngested(fullPath, stat, context, manifest);
      ingested++;
    } catch (err) {
      process.stderr.write(
        `[wuphf-capture] Plan file ingest failed (${entry.name}): ${err instanceof Error ? err.message : String(err)}\n`
      );
    }
  }

  if (ingested > 0) {
    writeManifest(manifest);
  }
}

async function main(): Promise<void> {
  try {
    // Read stdin
    const chunks: Buffer[] = [];
    for await (const chunk of process.stdin) {
      chunks.push(chunk as Buffer);
    }
    const raw = Buffer.concat(chunks).toString("utf-8");

    // Check .wuphf.toml kill switch
    if (!isHookEnabled("capture")) {
      process.stdout.write("{}");
      return;
    }

    let input: HookInput;
    try {
      input = JSON.parse(raw) as HookInput;
    } catch {
      process.stdout.write("{}");
      return;
    }

    let cfg;
    try {
      cfg = loadConfig();
    } catch {
      process.stdout.write("{}");
      return;
    }

    const client = new NexClient(cfg.apiKey, cfg.baseUrl);

    // --- Conversation capture (existing behavior) ---
    const message = input.last_assistant_message?.trim();
    if (message) {
      const filterResult = captureFilter(message);

      if (!filterResult.skipped) {
        if (rateLimiter.canProceed()) {
          try {
            await client.ingest(filterResult.text, "claude-code-conversation", INGEST_TIMEOUT_MS);
          } catch (err) {
            process.stderr.write(
              `[wuphf-capture] Ingest failed: ${err instanceof Error ? err.message : String(err)}\n`
            );
          }
        } else {
          process.stderr.write("[wuphf-capture] Rate limited — skipping conversation ingest\n");
        }
      }
    }

    // --- Plan file ingestion ---
    try {
      await ingestPlanFiles(client);
    } catch (err) {
      process.stderr.write(
        `[wuphf-capture] Plan file scan error: ${err instanceof Error ? err.message : String(err)}\n`
      );
    }

    process.stdout.write("{}");
  } catch (err) {
    process.stderr.write(
      `[wuphf-capture] Unexpected error: ${err instanceof Error ? err.message : String(err)}\n`
    );
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
