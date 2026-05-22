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

import type {
  CeoChecklistSuggestion,
  CeoChipRowSuggestion,
  CeoFormFieldSuggestion,
  CeoSuggestion,
  CeoTeamTrimSuggestion,
} from "./types";

const NON_CONVERSATIONAL_FIELDS = new Set(["bridge_choice", "scan_complete"]);

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
  if (NON_CONVERSATIONAL_FIELDS.has(field)) return null;

  switch (suggestion.kind) {
    case "ceo_form_field":
      return echoFormField(suggestion, value);
    case "ceo_chip_row":
      return echoChipRow(suggestion, value);
    case "ceo_checklist":
    case "ceo_team_trim":
      return echoChecklist(suggestion, value);
    case "ceo_scan_chip":
    case "ceo_execution_lineup":
      // Read-only / confirmation cards — the broker drives the committed
      // state; there is no human text or chip to mirror.
      return null;
  }
}

function echoFormField(
  _suggestion: CeoFormFieldSuggestion,
  value: unknown,
): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  return trimmed === "" ? null : trimmed;
}

function echoChipRow(
  suggestion: CeoChipRowSuggestion,
  value: unknown,
): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  if (trimmed === "") {
    // The "Start from scratch" chip carries id="" but still has a label.
    const match = suggestion.payload.options.find((o) => o.id === "");
    return match ? match.label : null;
  }
  const match = suggestion.payload.options.find((o) => o.id === trimmed);
  return match ? match.label : trimmed;
}

function echoChecklist(
  suggestion: CeoChecklistSuggestion | CeoTeamTrimSuggestion,
  value: unknown,
): string | null {
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
