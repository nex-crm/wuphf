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

import { doCapture, readStdin } from "./shared.js";

interface HookInput {
  last_assistant_message?: string;
  session_id?: string;
}

async function main(): Promise<void> {
  try {
    const raw = await readStdin();

    let input: HookInput;
    try {
      input = JSON.parse(raw) as HookInput;
    } catch {
      process.stdout.write("{}");
      return;
    }

    await doCapture({
      message: input.last_assistant_message ?? "",
      sessionId: input.session_id,
    });

    process.stdout.write("{}");
  } catch (err) {
    process.stderr.write(
      `[wuphf-capture] Unexpected error: ${err instanceof Error ? err.message : String(err)}\n`
    );
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
