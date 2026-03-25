/**
 * Proactive recall filter — surfaces knowledge graph context on every meaningful prompt.
 *
 * Philosophy: err heavily on the side of recalling. The moments where context
 * feels most "magical" are when the user DIDN'T ask for it — they're fixing a
 * migration, deploying a service, or refactoring code, and suddenly relevant
 * insights, patterns, or entity context from their knowledge graph appears.
 *
 * Only skip on truly trivial inputs (confirmations, single words) or explicit opt-out.
 * Debounce prevents hammering the API on rapid-fire prompts.
 *
 * Persistence: last-recall timestamp stored in ~/.wuphf/recall-state.json
 */

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

const DATA_DIR = join(homedir(), ".wuphf");
const STATE_FILE = join(DATA_DIR, "recall-state.json");
const DEBOUNCE_MS = 10_000; // 10 seconds

// --- Trivial inputs that never benefit from context ---
const TRIVIAL = /^\s*(y(es)?|no?|ok(ay)?|sure|done|thanks?|k|lgtm|approved?|merge|ship\s*it|looks?\s*good|\+1|👍)\s*[.!?]*\s*$/i;

export interface RecallDecision {
  shouldRecall: boolean;
  reason: string;
}

interface RecallState {
  lastRecallAt: number; // epoch ms
  lastRecallSessionId?: string;
}

function readState(): RecallState {
  try {
    const raw = readFileSync(STATE_FILE, "utf-8");
    return JSON.parse(raw) as RecallState;
  } catch {
    return { lastRecallAt: 0 };
  }
}

function writeState(state: RecallState): void {
  try {
    mkdirSync(DATA_DIR, { recursive: true });
    writeFileSync(STATE_FILE, JSON.stringify(state), "utf-8");
  } catch {
    // best-effort
  }
}

/**
 * Record that a successful recall just happened.
 * Call this after a non-empty /ask response.
 */
export function recordRecall(sessionId?: string): void {
  writeState({ lastRecallAt: Date.now(), lastRecallSessionId: sessionId });
}

/**
 * Determine whether this prompt should trigger a WUPHF /ask recall.
 *
 * Default: ALWAYS recall. Only skip on:
 * - Explicit opt-out (prompt starts with !)
 * - Trivial confirmations (yes, ok, done, lgtm)
 * - Very short inputs (< 10 chars)
 * - Debounce (< 10s since last recall, unless first prompt)
 */
export function shouldRecall(prompt: string, isFirstPrompt: boolean): RecallDecision {
  const trimmed = prompt.trim();

  // Explicit opt-out: prompt starts with ! (overrides everything)
  if (trimmed.startsWith("!")) {
    return { shouldRecall: false, reason: "opt-out" };
  }

  // Always recall on first prompt of session
  if (isFirstPrompt) {
    return { shouldRecall: true, reason: "first-prompt" };
  }

  // Trivial confirmations
  if (TRIVIAL.test(trimmed)) {
    return { shouldRecall: false, reason: "trivial" };
  }

  // Too short to be meaningful
  if (trimmed.length < 10) {
    return { shouldRecall: false, reason: "too-short" };
  }

  // Debounce: skip if recent successful recall
  const state = readState();
  if (state.lastRecallAt > 0 && Date.now() - state.lastRecallAt < DEBOUNCE_MS) {
    return { shouldRecall: false, reason: "debounce" };
  }

  // Default: always recall — proactive context is the goal
  return { shouldRecall: true, reason: "proactive" };
}
