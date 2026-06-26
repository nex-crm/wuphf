/**
 * humanizeActivity — the render-boundary humanizer (ten-out-of-ten E1).
 *
 * Agent runtime strings leak engineering internals into human surfaces:
 * MCP tool ids ("mcp__wuphf-office__team_task"), raw tool-call JSON
 * ('[{"tool_name":…,"type":"tool_reference"}]') in the typing strip,
 * process exhaust ("signal: killed", "exit status 1") in the activity
 * feed, and lifecycle enums ("blocked") on cards. A person
 * scanning chat, activity, or typing surfaces should never see code.
 *
 * This module is the single place those strings get cleaned. Every
 * human-facing surface that renders broker/runtime strings routes
 * through one of these helpers instead of rolling its own filter.
 */

/** Plain-language labels for lifecycle states (raw enums never render). */
const LIFECYCLE_STATE_LABELS: Record<string, string> = {
  drafting: "Parked",
  intake: "Intake",
  ready: "Ready",
  running: "Running",
  review: "In review",
  decision: "Needs decision",
  blocked: "Blocked",
  queued_behind_owner: "Queued behind owner",
  changes_requested: "Changes requested",
  approved: "Approved",
  rejected: "Rejected",
  archived: "Archived",
};

/**
 * Map a lifecycle state (or any snake_case status token) to a plain
 * label. Known states use the curated table; unknown ones degrade to
 * capitalized words so a new enum still never renders raw snake_case.
 */
export function humanizeLifecycleState(state: string): string {
  const s = state.trim().toLowerCase();
  if (!s) return "";
  const known = LIFECYCLE_STATE_LABELS[s];
  if (known) return known;
  const words = s.replaceAll(/_+/g, " ").trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/**
 * Replace raw lifecycle enum tokens inside a prose string with their
 * plain labels ("running → blocked" → "Running → Blocked"). Used on
 * broker-written summary lines that embed state names in free text.
 */
export function humanizeStateTokens(text: string): string {
  let out = text;
  for (const [token, label] of Object.entries(LIFECYCLE_STATE_LABELS)) {
    // Only rewrite snake_case tokens — single plain words ("review",
    // "running") read fine in prose and rewriting them would mangle
    // ordinary sentences.
    if (!token.includes("_")) continue;
    out = out.replaceAll(token, label);
  }
  return out;
}

/**
 * True when a runtime string is raw machinery rather than prose: a JSON
 * payload, an MCP/snake_case tool id, or process exhaust.
 */
export function looksLikeRawToolPayload(raw: string): boolean {
  const s = raw.trim();
  if (!s) return false;
  return (
    s.startsWith("[") ||
    s.startsWith("{") ||
    s.includes('"tool') ||
    s.includes("mcp__") ||
    /signal:\s*killed/i.test(s) ||
    /\bexit status \d+/i.test(s) ||
    // optional "running"/"using" verb + a snake_case identifier and nothing else
    /^(?:running|using)?\s*[a-z0-9]+(?:_[a-z0-9]+)+$/i.test(s)
  );
}

/**
 * Clean a LIVE activity string ("what is this agent doing right now") for
 * the participants rail and the typing strip. Genuine prose passes
 * through; anything machine-shaped collapses to "Working…" so the user
 * still sees activity without seeing code.
 */
export function humanizeActivity(raw: string): string {
  const s = raw.trim();
  if (!s) return s;
  return looksLikeRawToolPayload(s) ? "Working…" : s;
}

/**
 * Clean a SETTLED turn outcome for the activity feed. Process exhaust
 * ("signal: killed: signal: killed", "exit status 1") becomes an honest
 * one-liner; raw JSON/tool payloads are dropped rather than rendered raw;
 * prose passes through with state tokens humanized. Unlike
 * humanizeActivity, a lone snake_case word is NOT treated as machinery —
 * audit summaries legitimately contain identifiers.
 */
export function humanizeTurnOutcome(raw: string): string {
  const s = raw.trim();
  if (!s) return "";
  if (/signal:\s*killed/i.test(s) || /\bexit status \d+/i.test(s)) {
    return "Turn was interrupted before finishing.";
  }
  if (
    s.startsWith("[") ||
    s.startsWith("{") ||
    s.includes('"tool') ||
    s.includes("mcp__")
  ) {
    return "";
  }
  return humanizeStateTokens(s);
}
