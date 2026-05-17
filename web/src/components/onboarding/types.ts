/**
 * Wire shapes for Phase 2 deterministic CEO conversation cards.
 *
 * These types mirror the Go-side `Suggestion` struct in
 * internal/onboarding/state.go. The backend agent is responsible for
 * sanitizing payload strings through sanitizeContextValue before writing;
 * the frontend treats every string field as plain text (never innerHTML).
 *
 * Spec: docs/specs/onboarding-into-office.md — "## State and resumption"
 */

// ── Onboarding state from /onboarding/state ────────────────────────────────

export interface OnboardingFormAnswers {
  company_name?: string;
  description?: string;
  priority?: string;
  website_url?: string;
  owner_name?: string;
  owner_role?: string;
  blueprint_id?: string;
  picked_agents?: string[];
  scan_complete?: boolean;
}

export interface OnboardingState {
  onboarded?: boolean;
  phase?: string;
  ceo_dm_channel_id?: string;
  pending_suggestion?: CeoSuggestion | null;
  form_answers?: OnboardingFormAnswers;
  first_issue_id?: string;
  first_issue_approved_at?: string;
  company_name?: string;
  completed_at?: string;
}

// ── Suggestion wire shapes ─────────────────────────────────────────────────

/** Discriminated union of all CEO suggestion kinds. */
export type CeoSuggestion =
  | CeoFormFieldSuggestion
  | CeoChipRowSuggestion
  | CeoChecklistSuggestion
  | CeoTeamTrimSuggestion
  | CeoScanChipSuggestion;

interface SuggestionBase {
  /** Stable per (phase, options-hash) — used for idempotent re-emit dedup. */
  id: string;
  phase: string;
}

export interface CeoFormFieldSuggestion extends SuggestionBase {
  kind: "ceo_form_field";
  payload: CeoFormFieldPayload;
}

export interface CeoChipRowSuggestion extends SuggestionBase {
  kind: "ceo_chip_row";
  payload: CeoChipRowPayload;
}

export interface CeoChecklistSuggestion extends SuggestionBase {
  kind: "ceo_checklist";
  payload: CeoChecklistPayload;
}

export interface CeoTeamTrimSuggestion extends SuggestionBase {
  kind: "ceo_team_trim";
  payload: CeoTeamTrimPayload;
}

export interface CeoScanChipSuggestion extends SuggestionBase {
  kind: "ceo_scan_chip";
  payload: CeoScanChipPayload;
}

// ── Per-kind payload shapes ────────────────────────────────────────────────

export interface CeoFormFieldPayload {
  /** Field identifier sent back to /onboarding/answer */
  field: string;
  /** Human-readable label shown above the input */
  label: string;
  /** Whether the field can be skipped */
  optional?: boolean;
  /** Input placeholder */
  placeholder?: string;
  /** Default value pre-filled in the input */
  default?: string;
}

export interface CeoChipOption {
  id: string;
  label: string;
}

export interface CeoChipRowPayload {
  field: string;
  label: string;
  options: CeoChipOption[];
}

export interface CeoChecklistItem {
  id: string;
  label: string;
  /** Whether the item is pre-checked */
  default_checked?: boolean;
}

export interface CeoChecklistPayload {
  field: string;
  label: string;
  items: CeoChecklistItem[];
  submit_label?: string;
}

/** Alias of checklist with team-specific framing. Same wire shape. */
export interface CeoTeamTrimPayload extends CeoChecklistPayload {
  /** Agent slugs that are currently in the blueprint team roster */
  team_agents?: string[];
}

export type ScanStatus = "scanning" | "done" | "failed";

export interface CeoScanChipPayload {
  field: string;
  url: string;
  /** Current scan status; updated via SSE */
  status: ScanStatus;
  /** Message shown while scanning */
  scanning_label?: string;
  /** Message shown on success */
  done_label?: string;
  /** Message shown on failure */
  failed_label?: string;
}

// ── Card lifecycle state ───────────────────────────────────────────────────

/** Universal three-stage card lifecycle per design review decisions. */
export type CardStage = "pending" | "submitting" | "committed";
