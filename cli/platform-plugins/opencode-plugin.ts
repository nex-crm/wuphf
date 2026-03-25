/**
 * WUPHF plugin for OpenCode — organizational context & memory.
 *
 * This file is copied to .opencode/plugins/wuphf.ts by `wuphf setup`.
 * OpenCode's bun runtime resolves imports at load time.
 *
 * Provides:
 * - Session lifecycle hooks (context loading on session create)
 * - Context preservation during compaction
 */

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

// Read API key from shared WUPHF config
function loadApiKey(): string | null {
  try {
    const raw = readFileSync(join(homedir(), ".wuphf-mcp.json"), "utf-8");
    const config = JSON.parse(raw);
    return config.api_key ?? null;
  } catch {
    return null;
  }
}

async function nexAsk(query: string, apiKey: string): Promise<string> {
  const baseUrl = process.env.WUPHF_API_BASE_URL ?? "https://app.nex.ai";
  const res = await fetch(`${baseUrl}/api/developers/v1/context/ask`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${apiKey}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ query }),
    signal: AbortSignal.timeout(10_000),
  });
  if (!res.ok) return "";
  const data = await res.json() as { answer?: string };
  return data.answer ?? "";
}

async function nexIngest(content: string, context: string, apiKey: string): Promise<void> {
  const baseUrl = process.env.WUPHF_API_BASE_URL ?? "https://app.nex.ai";
  await fetch(`${baseUrl}/api/developers/v1/context/text`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${apiKey}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ content, context }),
    signal: AbortSignal.timeout(5_000),
  });
}

let cachedContext = "";

export default {
  name: "wuphf",

  async "session.created"() {
    const apiKey = loadApiKey();
    if (!apiKey) return;

    try {
      const answer = await nexAsk(
        "Summarize the key active context, recent interactions, and important updates for this user.",
        apiKey,
      );
      if (answer) {
        cachedContext = `<wuphf-context>\n${answer}\n</wuphf-context>`;
      }
    } catch {
      // graceful degradation
    }

    return cachedContext ? { context: cachedContext } : undefined;
  },

  async "experimental.session.compacting"(_input: unknown, output: { context: string[] }) {
    if (cachedContext) {
      output.context.push(cachedContext);
    }
  },

  async "session.completed"(_input: { messages?: Array<{ role: string; content: string }> }) {
    const apiKey = loadApiKey();
    if (!apiKey) return;

    // Capture last assistant message
    const messages = (_input as any)?.messages;
    if (!Array.isArray(messages)) return;

    const lastAssistant = [...messages].reverse().find((m) => m.role === "assistant");
    if (!lastAssistant?.content || lastAssistant.content.length < 20) return;

    try {
      await nexIngest(lastAssistant.content.slice(0, 50_000), "opencode-conversation", apiKey);
    } catch {
      // graceful degradation
    }
  },
};
