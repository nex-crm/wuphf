#!/usr/bin/env node
/**
 * Cline TaskStart hook — load baseline context from WUPHF.
 * Input: { taskStart: { task: string } }
 * Output: { contextModification: "..." } or {}
 */

import { doSessionStart, readStdin } from "../shared.js";

interface ClineInput {
  taskStart?: { task?: string };
  session_id?: string;
}

async function main(): Promise<void> {
  try {
    const raw = await readStdin();
    let input: ClineInput = {};
    try { input = JSON.parse(raw); } catch { /* defaults */ }

    const result = await doSessionStart("startup", input.session_id);
    if (!result) {
      process.stdout.write("{}");
      return;
    }

    const context = result.registrationPrompt ?? result.context;
    process.stdout.write(JSON.stringify({ contextModification: context }));
  } catch (err) {
    process.stderr.write(`[wuphf-cline] Task start error: ${err instanceof Error ? err.message : String(err)}\n`);
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
