#!/usr/bin/env node
/**
 * Claude Code SessionStart hook — bulk context load from Nex.
 *
 * Fires once when a new Claude Code session begins. Queries Nex for
 * a baseline context summary and injects it so the agent "already knows"
 * relevant business context from the first message.
 *
 * On ANY error: outputs {} and exits 0 (graceful degradation).
 */

import { loadConfig } from "./config.js";
import { NexClient } from "./nex-client.js";
import { formatNexContext } from "./context-format.js";
import { SessionStore } from "./session-store.js";
import { recordRecall } from "./recall-filter.js";
import { isScanEnabled, scanDirectory } from "./file-scanner.js";

const sessions = new SessionStore();

interface HookInput {
  session_id?: string;
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

    const result = await client.ask(SESSION_START_QUERY, undefined, 10_000);

    if (!result.answer) {
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

    // --- File scan (best-effort, non-blocking) ---
    let scanSummary = "";
    if (isScanEnabled()) {
      try {
        const cwd = process.cwd();
        const scanResult = await scanDirectory(cwd, client);
        if (scanResult.scanned > 0) {
          scanSummary = `\n[nex-scan] Ingested ${scanResult.scanned} file(s) from ${cwd}.`;
        }
      } catch (err) {
        process.stderr.write(
          `[nex-session-start] File scan error: ${err instanceof Error ? err.message : String(err)}\n`
        );
      }
    }

    const output = JSON.stringify({
      hookSpecificOutput: {
        hookEventName: "SessionStart",
        additionalContext: context + scanSummary,
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
