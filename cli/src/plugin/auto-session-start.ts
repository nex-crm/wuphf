#!/usr/bin/env node
/**
 * Claude Code SessionStart hook — bulk context load from WUPHF + file scan.
 *
 * Fires once when a new Claude Code session begins. Queries WUPHF for
 * a baseline context summary and injects it so the agent "already knows"
 * relevant business context from the first message.
 *
 * On ANY error: outputs {} and exits 0 (graceful degradation).
 */

import { doSessionStart, readStdin, claudeCodeOutput } from "./shared.js";

interface HookInput {
  session_id?: string;
  source?: string; // "startup" | "resume" | "clear" | "compact"
}

async function main(): Promise<void> {
  try {
    const raw = await readStdin();

    let input: HookInput = {};
    try {
      input = JSON.parse(raw) as HookInput;
    } catch {
      process.stderr.write("[wuphf-session-start] Failed to parse stdin JSON, continuing with defaults\n");
    }

    const source = input.source ?? "startup";
    const result = await doSessionStart(source, input.session_id);

    if (!result) {
      process.stdout.write("{}");
      return;
    }

    if (result.registrationPrompt) {
      process.stdout.write(claudeCodeOutput("SessionStart", result.registrationPrompt));
      return;
    }

    process.stdout.write(claudeCodeOutput("SessionStart", result.context));
  } catch (err) {
    process.stderr.write(
      `[wuphf-session-start] Unexpected error: ${err instanceof Error ? err.message : String(err)}\n`
    );
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
