/**
 * Claude Code provider — runs `claude -p` via Worker thread.
 *
 * Bun's event loop stalls when child processes are awaited inside Ink's
 * React render context. Worker threads have their own event loop and
 * don't suffer from this. The worker spawns claude, collects output,
 * and posts the result back via postMessage.
 */

import { Worker } from "node:worker_threads";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import type { StreamFn } from "../types.js";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const sessionStore = new Map<string, string>();

export function createClaudeCodeStreamFn(
  agentSlug = "default",
  cwd?: string,
  model?: string,
): StreamFn {
  return async function* claudeCodeStream(
    messages: Array<{ role: string; content: string }>,
    _tools,
  ) {
    const lastMsg = messages[messages.length - 1];
    const prompt = lastMsg?.content ?? "";

    const context = messages.slice(0, -1)
      .map(m => `${m.role}: ${m.content}`)
      .join("\n")
      .slice(-4000);

    const fullPrompt = context
      ? `Context:\n${context}\n\nUser: ${prompt}`
      : prompt;

    const args: string[] = [
      "-p", fullPrompt,
      "--output-format", "stream-json",
      "--verbose",
      "--max-turns", "5",
      "--no-session-persistence",
      "--allowedTools", "Read,Glob,Grep,WebSearch,WebFetch",
    ];

    const existingSession = sessionStore.get(agentSlug);
    if (existingSession) args.push("--resume", existingSession);
    if (cwd) args.push("--cwd", cwd);
    if (model) args.push("--model", model);

    // Run claude in a Worker thread (own event loop, no Ink interference)
    const workerPath = join(__dirname, "claude-worker.ts");
    const result = await runInWorker(workerPath, { prompt: fullPrompt, args });

    if (result.ok) {
      const { text, sid } = parseClaudeOutput(result.stdout);
      if (sid) sessionStore.set(agentSlug, sid);
      yield { type: "text" as const, content: text || "[Claude Code returned empty response]" };
    } else {
      yield { type: "text" as const, content: `Claude Code error: ${result.error}` };
    }
  };
}

function runInWorker(
  workerPath: string,
  msg: { prompt: string; args: string[] },
): Promise<{ ok: boolean; stdout: string; error?: string }> {
  return new Promise((resolve) => {
    const worker = new Worker(workerPath);

    const timeout = setTimeout(() => {
      worker.terminate();
      resolve({ ok: false, stdout: "", error: "Timed out after 120s" });
    }, 125_000);

    worker.on("message", (result: { ok: boolean; stdout?: string; error?: string }) => {
      clearTimeout(timeout);
      worker.terminate();
      resolve({ ok: result.ok, stdout: result.stdout ?? "", error: result.error });
    });

    worker.on("error", (err) => {
      clearTimeout(timeout);
      resolve({ ok: false, stdout: "", error: err.message });
    });

    worker.postMessage(msg);
  });
}

function parseClaudeOutput(stdout: string): { text: string; sid?: string } {
  const textChunks: string[] = [];
  let sessionId: string | undefined;

  for (const line of stdout.split("\n")) {
    if (!line.trim()) continue;
    let event: Record<string, unknown>;
    try { event = JSON.parse(line); } catch { continue; }

    if (!sessionId && event.session_id) sessionId = event.session_id as string;

    if (event.type === "assistant" && event.message) {
      const msg = event.message as Record<string, unknown>;
      const content = msg.content as Array<Record<string, unknown>> | undefined;
      if (content) {
        for (const part of content) {
          if (part.type === "text" && part.text) textChunks.push(part.text as string);
        }
      }
    }

    if (event.type === "result" && textChunks.length === 0) {
      const resultText = event.result as string | undefined;
      if (resultText) textChunks.push(resultText);
    }
  }

  return { text: textChunks.join("").trim(), sid: sessionId };
}

export function clearClaudeSession(agentSlug: string): void {
  sessionStore.delete(agentSlug);
}
