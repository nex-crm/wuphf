/**
 * StructuredMessageCard — single-dispatch point for all structured message
 * card kinds.
 *
 * Phase 5 consolidation (docs/specs/onboarding-into-office.md §"Phase 5 —
 * Polish and cleanups", TODO-D1): InterviewBar previously dispatched CEO kinds
 * inline; this module is the single sanitization audit point for both interview
 * kinds (approval) and CEO kinds.
 *
 * Security invariant: every string that enters a card payload passes through
 * sanitizeStructuredPayload before any component renders it. All card
 * sub-components render strings as plain text nodes — no raw HTML injection.
 * This is defense-in-depth on top of the Go-side sanitizeContextValue (PR #684).
 *
 * Usage:
 *   <StructuredMessageCard suggestion={suggestion} ... />
 */

import type { ReactNode } from "react";

import type { CardStage, CeoSuggestion } from "../../onboarding/types";
import { CeoChecklist } from "./CeoChecklist";
import { CeoChipRow } from "./CeoChipRow";
import { CeoExecutionLineup } from "./CeoExecutionLineup";
import { CeoFormField } from "./CeoFormField";
import { CeoScanChip } from "./CeoScanChip";

// ── Sanitization ────────────────────────────────────────────────────────────

/**
 * SanitizedPayload is the output type of sanitizeStructuredPayload.
 * It is a deep copy of the input confirmed to be a plain JSON-serializable
 * object before cards render it.
 *
 * The server is the authoritative sanitizer (sanitizeContextValue in
 * broker_onboarding.go). This is the frontend line of defense.
 */
export type SanitizedPayload = Record<string, unknown>;

/**
 * Sanitize a structured card payload before rendering.
 *
 * Deep-clones the payload so mutations after sanitization do not affect the
 * original. If the payload is not a plain object, returns an empty object so
 * the card renders a safe fallback.
 *
 * Cards must render all string values as plain text nodes. This function
 * confirms the payload is well-formed and makes a defensive copy.
 */
export function sanitizeStructuredPayload(payload: unknown): SanitizedPayload {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    return {};
  }
  return deepClone(payload as Record<string, unknown>);
}

function deepClone(obj: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(obj)) {
    const val = obj[key];
    if (val && typeof val === "object" && !Array.isArray(val)) {
      out[key] = deepClone(val as Record<string, unknown>);
    } else if (Array.isArray(val)) {
      out[key] = val.map((item) =>
        item && typeof item === "object" && !Array.isArray(item)
          ? deepClone(item as Record<string, unknown>)
          : item,
      );
    } else {
      out[key] = val;
    }
  }
  return out;
}

// ── Props ───────────────────────────────────────────────────────────────────

export interface StructuredMessageCardProps {
  suggestion: CeoSuggestion;
  stage: CardStage;
  committedValue?: string | string[];
  onSubmit: (field: string, value: unknown) => void;
  onSkip?: (field: string) => void;
  onStageChange?: (next: CardStage) => void;
}

// ── Dispatcher ──────────────────────────────────────────────────────────────

/**
 * StructuredMessageCard dispatches a CeoSuggestion to the appropriate
 * sub-component after running sanitizeStructuredPayload on its payload.
 *
 * Unknown kinds render a safe fallback span with no executable content.
 */
export function StructuredMessageCard({
  suggestion,
  stage,
  committedValue,
  onSubmit,
  onSkip,
  onStageChange,
}: StructuredMessageCardProps): ReactNode {
  // Sanitize the payload: deep-clone before cards render it.
  // Cast through unknown to map the sanitized Record back to the typed shape —
  // the sanitizer guarantees structural identity, only adds the immutability
  // guarantee of a deep copy.
  const safePayload = sanitizeStructuredPayload(suggestion.payload) as unknown;

  switch (suggestion.kind) {
    case "ceo_form_field": {
      const p = safePayload as Parameters<typeof CeoFormField>[0]["payload"];
      return (
        <CeoFormField
          payload={p}
          stage={stage}
          committedValue={
            typeof committedValue === "string" ? committedValue : undefined
          }
          onSubmit={(field, value) => onSubmit(field, value)}
          onSkip={p.optional ? (field) => onSkip?.(field) : undefined}
        />
      );
    }
    case "ceo_chip_row":
      return (
        <CeoChipRow
          payload={safePayload as Parameters<typeof CeoChipRow>[0]["payload"]}
          stage={stage}
          committedValue={
            typeof committedValue === "string" ? committedValue : undefined
          }
          onSubmit={(field, value) => onSubmit(field, value)}
        />
      );
    case "ceo_checklist":
    case "ceo_team_trim":
      return (
        <CeoChecklist
          payload={safePayload as Parameters<typeof CeoChecklist>[0]["payload"]}
          stage={stage}
          committedValue={
            Array.isArray(committedValue) ? committedValue : undefined
          }
          onSubmit={(field, value) => onSubmit(field, value)}
        />
      );
    case "ceo_scan_chip":
      return (
        <CeoScanChip
          payload={safePayload as Parameters<typeof CeoScanChip>[0]["payload"]}
        />
      );
    case "ceo_execution_lineup":
      return (
        <CeoExecutionLineup
          payload={
            safePayload as Parameters<typeof CeoExecutionLineup>[0]["payload"]
          }
          stage={stage}
          onStageChange={
            onStageChange ??
            (() => {
              /* no-op fallback */
            })
          }
        />
      );
    default: {
      // Exhaustiveness guard: TypeScript will flag unhandled CeoSuggestion
      // kinds here, forcing this dispatcher to stay in sync with the union.
      const _exhaustive: never = suggestion;
      void _exhaustive;
      // Safe fallback: render nothing executable.
      return <span data-testid="structured-card-unknown-kind" />;
    }
  }
}
