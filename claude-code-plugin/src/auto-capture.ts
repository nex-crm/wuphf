#!/usr/bin/env node
/**
 * Claude Code Stop hook — auto-capture conversation to Nex.
 *
 * Reads { last_assistant_message, session_id } from stdin,
 * filters and sends to Nex for ingestion.
 *
 * On ANY error: outputs {} and exits 0 (graceful degradation).
 */

import { loadConfig } from "./config.js";
import { NexClient } from "./nex-client.js";
import { captureFilter } from "./capture-filter.js";
import { RateLimiter } from "./rate-limiter.js";

/** Ingest timeout — 3s leaves buffer within 5s hook timeout */
const INGEST_TIMEOUT_MS = 3_000;

const rateLimiter = new RateLimiter();

interface HookInput {
  last_assistant_message?: string;
  session_id?: string;
}

async function main(): Promise<void> {
  try {
    // Read stdin
    const chunks: Buffer[] = [];
    for await (const chunk of process.stdin) {
      chunks.push(chunk as Buffer);
    }
    const raw = Buffer.concat(chunks).toString("utf-8");

    let input: HookInput;
    try {
      input = JSON.parse(raw) as HookInput;
    } catch {
      process.stdout.write("{}");
      return;
    }

    const message = input.last_assistant_message?.trim();
    if (!message) {
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

    const filterResult = captureFilter(message);

    if (filterResult.skipped) {
      process.stdout.write("{}");
      return;
    }

    // Check rate limit before making API call
    if (!rateLimiter.canProceed()) {
      process.stderr.write("[nex-capture] Rate limited — skipping ingest\n");
      process.stdout.write("{}");
      return;
    }

    // Fire-and-forget ingest with tight timeout
    const client = new NexClient(cfg.apiKey, cfg.baseUrl);
    try {
      await client.ingest(filterResult.text, "claude-code-conversation", INGEST_TIMEOUT_MS);
    } catch (err) {
      process.stderr.write(
        `[nex-capture] Ingest failed: ${err instanceof Error ? err.message : String(err)}\n`
      );
    }

    process.stdout.write("{}");
  } catch (err) {
    process.stderr.write(
      `[nex-capture] Unexpected error: ${err instanceof Error ? err.message : String(err)}\n`
    );
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
