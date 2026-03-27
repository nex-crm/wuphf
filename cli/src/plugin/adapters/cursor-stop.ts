#!/usr/bin/env node
/**
 * Cursor stop hook — auto-capture conversation to WUPHF.
 * Input: { last_message?: string, status?: string, session_id?: string }
 * Output: {}
 */

import { doCapture, readStdin } from "../shared.js";

interface CursorStopInput {
  last_message?: string;
  status?: string;
  session_id?: string;
}

async function main(): Promise<void> {
  try {
    const raw = await readStdin();
    let input: CursorStopInput = {};
    try { input = JSON.parse(raw); } catch { /* defaults */ }

    await doCapture({ message: input.last_message ?? "" });
    process.stdout.write("{}");
  } catch (err) {
    process.stderr.write(`[wuphf-cursor] Capture error: ${err instanceof Error ? err.message : String(err)}\n`);
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
