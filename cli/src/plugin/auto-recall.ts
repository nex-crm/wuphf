#!/usr/bin/env node
/**
 * Claude Code UserPromptSubmit hook — auto-recall from WUPHF.
 *
 * Reads the user's prompt from stdin, runs it through the recall filter
 * to decide if recall is needed, queries WUPHF for relevant context,
 * and outputs { additionalContext: "..." } to inject into the conversation.
 *
 * On ANY error: outputs {} and exits 0 (graceful degradation).
 */

import { doRecall, readStdin, claudeCodeOutput } from "./shared.js";

interface HookInput {
  prompt?: string;
  session_id?: string;
}

async function main(): Promise<void> {
  try {
    const raw = await readStdin();

    let input: HookInput;
    try {
      input = JSON.parse(raw) as HookInput;
    } catch {
      process.stderr.write("[wuphf-recall] Failed to parse stdin JSON\n");
      process.stdout.write("{}");
      return;
    }

    const result = await doRecall(input.prompt ?? "", input.session_id);

    if (!result) {
      process.stdout.write("{}");
      return;
    }

    process.stdout.write(claudeCodeOutput("UserPromptSubmit", result.context));
  } catch (err) {
    process.stderr.write(
      `[wuphf-recall] Unexpected error: ${err instanceof Error ? err.message : String(err)}\n`
    );
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
