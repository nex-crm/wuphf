#!/usr/bin/env node
/**
 * Windsurf post_cascade_response hook — auto-capture to WUPHF.
 * Input: { tool_info: { response: string } }
 * Output: {}
 */

import { doCapture, readStdin } from "../shared.js";

interface WindsurfInput {
  tool_info?: { response?: string };
}

async function main(): Promise<void> {
  try {
    const raw = await readStdin();
    let input: WindsurfInput = {};
    try { input = JSON.parse(raw); } catch { /* defaults */ }

    await doCapture({ message: input.tool_info?.response ?? "" });
    process.stdout.write("{}");
  } catch (err) {
    process.stderr.write(`[wuphf-windsurf] Capture error: ${err instanceof Error ? err.message : String(err)}\n`);
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
