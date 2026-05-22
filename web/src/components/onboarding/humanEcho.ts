/**
 * humanEcho — format a CEO card answer as a chat-bubble friendly string.
 *
 * After the user commits a wizard answer (form field, chip, checklist), we
 * mirror that answer back into the CEO DM as a "from: human" chat bubble so
 * the transcript reads like an actual conversation rather than a one-sided
 * monologue from the CEO (#978).
 *
 * Returns null when the field/value pair should NOT be echoed:
 *   - bridge_choice: the terminal "Start an issue" / "Look around" action
 *     transitions the user out of onboarding; echoing creates a stray bubble
 *     in the new office shell.
 *   - scan_complete: internal boolean flip on the website-scan handshake; no
 *     human action to mirror.
 *   - empty values: e.g. an optional field skipped via the "Skip" chip.
 *
 * The returned string is plain text — the caller persists it via the normal
 * /messages endpoint which already sanitises. We never embed payload markup.
 */

import type { CeoSuggestion } from "./types";

/**
 * Returns the chat-bubble text for the human's just-committed answer, or
 * null if the answer should not be echoed.
 */
export function humanEchoForCeoAnswer(
  suggestion: CeoSuggestion | null | undefined,
  field: string,
  value: unknown,
): string | null {
  if (!suggestion) return null;

  // Skip non-user-facing transitions.
  if (field === "bridge_choice" || field === "scan_complete") return null;

  if (suggestion.kind === "ceo_scan_chip") {
    // The scan chip is read-only; the broker drives its committed state.
    return null;
  }

  if (suggestion.kind === "ceo_chip_row") {
    if (typeof value !== "string") return null;
    const trimmed = value.trim();
    if (trimmed === "") {
      // Empty id (e.g. "Start from scratch") still has a meaningful label.
      const match = suggestion.payload.options.find((o) => o.id === "");
      return match ? match.label : null;
    }
    const match = suggestion.payload.options.find((o) => o.id === trimmed);
    return match ? match.label : trimmed;
  }

  if (
    suggestion.kind === "ceo_checklist" ||
    suggestion.kind === "ceo_team_trim"
  ) {
    if (!Array.isArray(value)) return null;
    const labels = value
      .map((id) => {
        if (typeof id !== "string") return "";
        const match = suggestion.payload.items.find((item) => item.id === id);
        return match ? match.label : id;
      })
      .filter((label) => label.trim() !== "");
    if (labels.length === 0) return "(nobody)";
    return labels.join(", ");
  }

  if (suggestion.kind === "ceo_execution_lineup") {
    // The execution lineup is a confirmation card; the broker handles the
    // resulting roster mutation. No human-typed value to mirror.
    return null;
  }

  // ceo_form_field — the canonical text-bubble case.
  if (suggestion.kind === "ceo_form_field") {
    if (typeof value !== "string") return null;
    const trimmed = value.trim();
    return trimmed === "" ? null : trimmed;
  }

  return null;
}
