#!/usr/bin/env node
/**
 * Cursor userPromptSubmit hook — auto-recall from WUPHF.
 * Input: { prompt?: string, attachments?: unknown[], session_id?: string }
 * Output: { continue: true, user_message: "<original + context>" } or {}
 */

import { doRecall, readStdin } from "../shared.js";

interface CursorInput {
  prompt?: string;
  attachments?: unknown[];
  session_id?: string;
}

async function main(): Promise<void> {
  try {
    const raw = await readStdin();
    let input: CursorInput = {};
    try { input = JSON.parse(raw); } catch { /* defaults */ }

    const prompt = input.prompt ?? "";
    const result = await doRecall(prompt, input.session_id);

    if (!result) {
      process.stdout.write("{}");
      return;
    }

    // Inject context as additional_context (Cursor's context injection mechanism)
    process.stdout.write(JSON.stringify({ additional_context: result.context }));
  } catch (err) {
    process.stderr.write(`[wuphf-cursor] Recall error: ${err instanceof Error ? err.message : String(err)}\n`);
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
