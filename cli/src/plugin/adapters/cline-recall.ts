#!/usr/bin/env node
/**
 * Cline UserPromptSubmit hook — auto-recall from WUPHF.
 * Input: { userPromptSubmit: { prompt: string } }
 * Output: { contextModification: "..." } or {}
 */

import { doRecall, readStdin } from "../shared.js";

interface ClineInput {
  userPromptSubmit?: { prompt?: string };
  session_id?: string;
}

async function main(): Promise<void> {
  try {
    const raw = await readStdin();
    let input: ClineInput = {};
    try { input = JSON.parse(raw); } catch { /* defaults */ }

    const prompt = input.userPromptSubmit?.prompt ?? "";
    const result = await doRecall(prompt, input.session_id);

    if (!result) {
      process.stdout.write("{}");
      return;
    }

    process.stdout.write(JSON.stringify({ contextModification: result.context }));
  } catch (err) {
    process.stderr.write(`[wuphf-cline] Recall error: ${err instanceof Error ? err.message : String(err)}\n`);
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
