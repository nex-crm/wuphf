#!/usr/bin/env node
/**
 * Windsurf pre_user_prompt hook — auto-recall from WUPHF.
 * Input: { tool_info: { user_prompt: string } }
 * Output: context string on stdout (displayed via show_output) or {}
 */

import { doRecall, readStdin } from "../shared.js";

interface WindsurfInput {
  tool_info?: { user_prompt?: string };
  session_id?: string;
}

async function main(): Promise<void> {
  try {
    const raw = await readStdin();
    let input: WindsurfInput = {};
    try { input = JSON.parse(raw); } catch { /* defaults */ }

    const prompt = input.tool_info?.user_prompt ?? "";
    const result = await doRecall(prompt, input.session_id);

    if (!result) {
      process.stdout.write("{}");
      return;
    }

    // Windsurf displays stdout content via show_output
    process.stdout.write(JSON.stringify({ additional_context: result.context }));
  } catch (err) {
    process.stderr.write(`[wuphf-windsurf] Recall error: ${err instanceof Error ? err.message : String(err)}\n`);
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
