/**
 * Formatting helpers for Task titles surfaced in the UI.
 *
 * The broker may persist a self-heal title with a "[@<slug>] " provenance
 * prefix — that prefix is for backend recognition + per-agent overflow
 * lookups. Humans don't need to see it: the assigned-agent chip already
 * shows ownership.
 */

const SELF_HEAL_TITLE_PREFIX_RE = /^\[@([a-z0-9_-]+)\]\s+/i;

/**
 * Strips the "[@<slug>] " self-heal provenance prefix so the title reads
 * cleanly in human-facing surfaces (Task header, kanban cards,
 * sub-task rows). Returns the original title unchanged when no prefix
 * is present.
 *
 *   "[@ceo] Agent stuck on: Send VC outreach" → "Agent stuck on: Send VC outreach"
 *   "Send VC outreach email"                   → "Send VC outreach email"
 */
export function formatTaskTitleForDisplay(
  title: string | undefined | null,
): string {
  if (!title) return "";
  return title.replace(SELF_HEAL_TITLE_PREFIX_RE, "");
}

/**
 * True when the title carries the self-heal provenance prefix — useful
 * for surfaces that want to render a "self-heal" badge instead of just
 * dropping the prefix.
 */
export function isSelfHealTaskTitle(title: string | undefined | null): boolean {
  if (!title) return false;
  return SELF_HEAL_TITLE_PREFIX_RE.test(title);
}

/**
 * Extracts the agent slug from a self-heal title's "[@<slug>] " prefix.
 * Returns null when the prefix isn't present.
 */
export function selfHealAgentFromTitle(
  title: string | undefined | null,
): string | null {
  if (!title) return null;
  const match = title.match(SELF_HEAL_TITLE_PREFIX_RE);
  return match ? match[1] : null;
}
