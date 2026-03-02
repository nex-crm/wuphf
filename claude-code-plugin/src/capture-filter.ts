/**
 * Capture filtering for Claude Code — decides what content to send to Nex.
 * Adapted from openclaw-plugin for plain string input.
 */

import { stripNexContext } from "./context-format.js";

const MIN_LENGTH = 20;
const MAX_LENGTH = 50_000;

export interface CaptureResult {
  text: string;
  skipped: false;
}

export interface CaptureSkip {
  reason: string;
  skipped: true;
}

/**
 * Filter text for capture eligibility.
 * Works with plain strings (Claude Code messages).
 *
 * Note: Deduplication is handled server-side by the Nex extraction pipeline.
 * Since hooks are short-lived processes (new process per invocation), in-memory
 * dedup caches would be empty on each run and provide no value.
 */
export function captureFilter(text: string): CaptureResult | CaptureSkip {
  if (!text || text.trim().length === 0) {
    return { skipped: true, reason: "empty text" };
  }

  // Strip injected context blocks (prevent feedback loop)
  const cleaned = stripNexContext(text);

  // Skip too short
  if (cleaned.length < MIN_LENGTH) {
    return { skipped: true, reason: `too short (${cleaned.length} chars)` };
  }

  // Skip too long
  if (cleaned.length > MAX_LENGTH) {
    return { skipped: true, reason: `too long (${cleaned.length} chars)` };
  }

  return { skipped: false, text: cleaned };
}
