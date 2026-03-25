#!/usr/bin/env node
/**
 * Cursor sessionStart hook — load baseline context from WUPHF.
 * Input: { session_id?: string }
 * Output: { additional_context: "..." } or {}
 */

import { doSessionStart, readStdin } from "../shared.js";

async function main(): Promise<void> {
  try {
    const raw = await readStdin();
    let input: { session_id?: string } = {};
    try { input = JSON.parse(raw); } catch { /* defaults */ }

    const result = await doSessionStart("startup", input.session_id);
    if (!result) {
      process.stdout.write("{}");
      return;
    }

    const context = result.registrationPrompt ?? result.context;
    process.stdout.write(JSON.stringify({ additional_context: context }));
  } catch (err) {
    process.stderr.write(`[wuphf-cursor] Session start error: ${err instanceof Error ? err.message : String(err)}\n`);
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
