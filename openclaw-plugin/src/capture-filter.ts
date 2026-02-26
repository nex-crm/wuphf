/**
 * Smart capture filtering — decides what content to send to Nex for ingestion.
 * Cherry-picked from Supermemory (provider skip), Engram (dedup), MemOS Cloud (capture modes).
 */

import { stripNexContext } from "./context-format.js";
import type { NexPluginConfig } from "./config.js";

/** Message shape from OpenClaw agent_end event. */
export interface AgentMessage {
  role: string;
  content?: string | Array<{ type: string; text?: string }>;
}

/** Providers whose messages should never be auto-captured. */
const SKIP_PROVIDERS = new Set(["exec-event", "cron-event"]);

const MIN_LENGTH = 10;
const MAX_LENGTH = 50_000;

/**
 * Simple content hash for deduplication.
 * Uses a fast string hash — not cryptographic, just collision-resistant enough for dedup.
 */
function hashContent(text: string): string {
  let hash = 0;
  for (let i = 0; i < text.length; i++) {
    const ch = text.charCodeAt(i);
    hash = ((hash << 5) - hash + ch) | 0;
  }
  return hash.toString(36);
}

/** LRU dedup cache entry. */
interface CacheEntry {
  hash: string;
  timestamp: number;
}

const DEDUP_MAX = 50;
const DEDUP_TTL_MS = 60 * 60 * 1000; // 1 hour

/** In-memory LRU cache for content hash dedup. */
const dedupCache: CacheEntry[] = [];

function isDuplicate(hash: string): boolean {
  const now = Date.now();
  // Evict expired entries
  while (dedupCache.length > 0 && now - dedupCache[0].timestamp > DEDUP_TTL_MS) {
    dedupCache.shift();
  }
  // Check for match
  if (dedupCache.some((e) => e.hash === hash)) {
    return true;
  }
  // Add to cache
  dedupCache.push({ hash, timestamp: now });
  // LIFO eviction if over max
  if (dedupCache.length > DEDUP_MAX) {
    dedupCache.shift();
  }
  return false;
}

/** Extract text content from an AgentMessage. */
function extractText(msg: AgentMessage): string {
  if (typeof msg.content === "string") return msg.content;
  if (Array.isArray(msg.content)) {
    return msg.content
      .filter((p) => p.type === "text" && p.text)
      .map((p) => p.text!)
      .join("\n");
  }
  return "";
}

export interface CaptureFilterResult {
  text: string;
  skipped: false;
}

export interface CaptureFilterSkip {
  reason: string;
  skipped: true;
}

/**
 * Process agent_end event and decide what to capture.
 * Returns cleaned text or a skip reason.
 */
export function captureFilter(
  messages: AgentMessage[],
  config: NexPluginConfig,
  opts?: { messageProvider?: string; success?: boolean }
): CaptureFilterResult | CaptureFilterSkip {
  // Skip failed agent runs
  if (opts?.success === false) {
    return { skipped: true, reason: "agent run failed" };
  }

  // Skip system event providers
  if (opts?.messageProvider && SKIP_PROVIDERS.has(opts.messageProvider)) {
    return { skipped: true, reason: `provider "${opts.messageProvider}" is in skip list` };
  }

  if (!messages || messages.length === 0) {
    return { skipped: true, reason: "no messages" };
  }

  let text: string;

  if (config.captureMode === "last_turn") {
    // Extract last user + last assistant message
    const parts: string[] = [];
    const reversed = [...messages].reverse();

    const lastAssistant = reversed.find((m) => m.role === "assistant");
    const lastUser = reversed.find((m) => m.role === "user");

    if (lastUser) parts.push(`User: ${extractText(lastUser)}`);
    if (lastAssistant) parts.push(`Assistant: ${extractText(lastAssistant)}`);

    text = parts.join("\n\n");
  } else {
    // full_session: capture all user and assistant messages
    text = messages
      .filter((m) => m.role === "user" || m.role === "assistant")
      .map((m) => `${m.role === "user" ? "User" : "Assistant"}: ${extractText(m)}`)
      .join("\n\n");
  }

  // Strip injected context blocks (prevent feedback loop)
  text = stripNexContext(text);

  // Skip slash commands
  if (text.startsWith("User: /")) {
    return { skipped: true, reason: "slash command" };
  }

  // Skip too short
  if (text.length < MIN_LENGTH) {
    return { skipped: true, reason: `too short (${text.length} chars)` };
  }

  // Skip too long
  if (text.length > MAX_LENGTH) {
    return { skipped: true, reason: `too long (${text.length} chars)` };
  }

  // Dedup check
  const hash = hashContent(text);
  if (isDuplicate(hash)) {
    return { skipped: true, reason: "duplicate content" };
  }

  return { skipped: false, text };
}

/** Reset dedup cache (for testing). */
export function resetDedupCache(): void {
  dedupCache.length = 0;
}
