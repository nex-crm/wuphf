#!/usr/bin/env node
/**
 * Claude Code SessionStart hook — bulk context load from Nex + file scan.
 *
 * Fires once when a new Claude Code session begins. Queries Nex for
 * a baseline context summary and injects it so the agent "already knows"
 * relevant business context from the first message.
 *
 * On startup/clear: also scans project files and ingests changed ones.
 * On compact/resume: skips file scan (files already ingested, just re-query summary).
 *
 * On ANY error: outputs {} and exits 0 (graceful degradation).
 */

import { loadConfig, loadScanConfig } from "./config.js";
import { NexClient } from "./nex-client.js";
import { RateLimiter } from "./rate-limiter.js";
import { formatNexContext } from "./context-format.js";
import { SessionStore } from "./session-store.js";
import { recordRecall } from "./recall-filter.js";
import { scanAndIngest } from "./file-scanner.js";
import { ingestContextFiles } from "./context-files.js";

const sessions = new SessionStore();

interface HookInput {
  session_id?: string;
  source?: string; // "startup" | "resume" | "clear" | "compact"
}

const SESSION_START_QUERY = "Summarize the key active context, recent interactions, and important updates for this user.";

async function main(): Promise<void> {
  try {
    // Read stdin
    const chunks: Buffer[] = [];
    for await (const chunk of process.stdin) {
      chunks.push(chunk as Buffer);
    }
    const raw = Buffer.concat(chunks).toString("utf-8");

    let input: HookInput = {};
    try {
      input = JSON.parse(raw) as HookInput;
    } catch {
      process.stderr.write("[nex-session-start] Failed to parse stdin JSON, continuing with defaults\n");
    }

    let cfg;
    try {
      cfg = loadConfig();
    } catch (err) {
      process.stderr.write(
        `[nex-session-start] Config error: ${err instanceof Error ? err.message : String(err)}\n`
      );
      process.stdout.write("{}");
      return;
    }

    const client = new NexClient(cfg.apiKey, cfg.baseUrl);
    const contextParts: string[] = [];

    // --- File scan on startup or clear ---
    const source = input.source ?? "startup";
    const shouldScan = source === "startup" || source === "clear";

    if (shouldScan) {
      const rateLimiter = new RateLimiter();
      const cwd = process.cwd();

      // --- Ingest CLAUDE.md + memory files (highest priority) ---
      try {
        const ctxResult = await ingestContextFiles(client, rateLimiter, cwd);
        if (ctxResult.ingested > 0) {
          contextParts.push(
            `[Context files: ${ctxResult.ingested} ingested (${ctxResult.files.join(", ")})]`
          );
        }
      } catch (err) {
        process.stderr.write(
          `[nex-session-start] Context files error: ${err instanceof Error ? err.message : String(err)}\n`
        );
      }

      // --- Project file scan ---
      try {
        const scanConfig = loadScanConfig();
        if (scanConfig.enabled) {
          const scanResult = await scanAndIngest(client, rateLimiter, cwd, scanConfig);

          if (scanResult.ingested > 0) {
            contextParts.push(
              `[File scan: ${scanResult.ingested} file${scanResult.ingested === 1 ? "" : "s"} ingested, ${scanResult.scanned} scanned]`
            );
          }
        }
      } catch (err) {
        process.stderr.write(
          `[nex-session-start] File scan error: ${err instanceof Error ? err.message : String(err)}\n`
        );
      }
    }

    // --- Nex context query ---
    const result = await client.ask(SESSION_START_QUERY, undefined, 10_000);

    if (!result.answer && contextParts.length === 0) {
      process.stdout.write("{}");
      return;
    }

    // Store session mapping
    if (result.session_id && input.session_id) {
      sessions.set(input.session_id, result.session_id);
    }

    // Record this as a successful recall for debounce
    recordRecall(result.session_id);

    const entityCount = result.entity_references?.length ?? 0;
    const context = formatNexContext({
      answer: result.answer,
      entityCount,
      sessionId: result.session_id,
    });

    // Append scan summary if any
    const fullContext = contextParts.length > 0
      ? `${context}\n${contextParts.join("\n")}`
      : context;

    const output = JSON.stringify({
      hookSpecificOutput: {
        hookEventName: "SessionStart",
        additionalContext: fullContext,
      },
    });
    process.stdout.write(output);
  } catch (err) {
    process.stderr.write(
      `[nex-session-start] Unexpected error: ${err instanceof Error ? err.message : String(err)}\n`
    );
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
