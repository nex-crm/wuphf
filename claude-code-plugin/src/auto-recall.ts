#!/usr/bin/env node
/**
 * Claude Code UserPromptSubmit hook — auto-recall from Nex.
 *
 * Reads the user's prompt from stdin, runs it through the recall filter
 * to decide if recall is needed, queries Nex for relevant context,
 * and outputs { additionalContext: "..." } to inject into the conversation.
 *
 * On ANY error: outputs {} and exits 0 (graceful degradation).
 */

import { loadConfig } from "./config.js";
import { NexClient } from "./nex-client.js";
import { formatNexContext } from "./context-format.js";
import { SessionStore } from "./session-store.js";
import { shouldRecall, recordRecall } from "./recall-filter.js";

const sessions = new SessionStore();

interface HookInput {
  prompt?: string;
  session_id?: string;
}

/**
 * Check if this is the first prompt for this session.
 * A session with no stored Nex session ID is considered "first prompt"
 * (SessionStart may have already set one, but that's fine — it means
 * baseline context was loaded, and first user prompt still gets recall).
 */
function isFirstPrompt(sessionKey: string | undefined): boolean {
  if (!sessionKey) return true;
  return !sessions.get(sessionKey);
}

async function main(): Promise<void> {
  try {
    // Read stdin
    const chunks: Buffer[] = [];
    for await (const chunk of process.stdin) {
      chunks.push(chunk as Buffer);
    }
    const raw = Buffer.concat(chunks).toString("utf-8");

    let input: HookInput;
    try {
      input = JSON.parse(raw) as HookInput;
    } catch {
      process.stdout.write("{}");
      return;
    }

    const prompt = input.prompt?.trim();
    if (!prompt || prompt.length < 5) {
      process.stdout.write("{}");
      return;
    }

    // Skip slash commands
    if (prompt.startsWith("/")) {
      process.stdout.write("{}");
      return;
    }

    // --- Recall filter: decide if this prompt needs memory recall ---
    const decision = shouldRecall(prompt, isFirstPrompt(input.session_id));
    if (!decision.shouldRecall) {
      process.stdout.write("{}");
      return;
    }

    let cfg;
    try {
      cfg = loadConfig();
    } catch {
      process.stdout.write("{}");
      return;
    }

    const client = new NexClient(cfg.apiKey, cfg.baseUrl);

    // Resolve session ID for multi-turn continuity
    const sessionKey = input.session_id;
    const nexSessionId = sessionKey ? sessions.get(sessionKey) : undefined;

    const result = await client.ask(prompt, nexSessionId, 10_000);

    if (!result.answer) {
      process.stdout.write("{}");
      return;
    }

    // Store session ID for future turns
    if (result.session_id && sessionKey) {
      sessions.set(sessionKey, result.session_id);
    }

    // Record successful recall for debounce
    recordRecall(result.session_id);

    const entityCount = result.entity_references?.length ?? 0;
    const context = formatNexContext({
      answer: result.answer,
      entityCount,
      sessionId: result.session_id,
    });

    // Use hookSpecificOutput for discrete context injection (not shown in transcript)
    const output = JSON.stringify({
      hookSpecificOutput: {
        hookEventName: "UserPromptSubmit",
        additionalContext: context,
      },
    });
    process.stdout.write(output);
  } catch {
    // Graceful degradation — never block Claude Code on recall failure
    process.stdout.write("{}");
  }
}

main().then(() => process.exit(0)).catch(() => process.exit(0));
