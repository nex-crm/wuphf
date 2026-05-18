/**
 * usePreviewOffice — derives sidebar preview rows from onboarding FormAnswers.
 *
 * During Phase 2 onboarding (phase != undefined && !onboarded), the sidebar
 * shows "preview rows" — dashed lavender-tinted rows for channels/agents that
 * are about to be seeded. These are driven by the staged FormAnswers the user
 * is filling in via the CEO conversation.
 *
 * Returns null when onboarding is complete (preview overlay should be hidden).
 *
 * Spec: docs/specs/onboarding-into-office.md "Sidebar preview overlay"
 */

import { useOnboardingState } from "./useOnboardingState";

export interface PreviewRow {
  kind: "channel" | "agent";
  label: string;
}

export interface PreviewOfficeState {
  /** Rows to render as dashed preview items in the sidebar. */
  rows: PreviewRow[];
  /** Current workspace label to preview (company name being typed). */
  workspaceLabel: string | undefined;
  /** Whether the preview overlay is active (non-seeded, non-complete phase). */
  active: boolean;
  /** Whether the seeding animation should play (phase just reached "seed"). */
  seeding: boolean;
}

/** Phases where the sidebar preview overlay should be shown. */
const PREVIEW_PHASES = new Set([
  "greet",
  "identity",
  "website",
  "scan",
  "blueprint",
  "team",
]);

export function usePreviewOffice(): PreviewOfficeState {
  const { data: state } = useOnboardingState();

  if (!state || state.onboarded) {
    return {
      rows: [],
      workspaceLabel: undefined,
      active: false,
      seeding: false,
    };
  }

  const phase = state.phase;
  if (!phase) {
    return {
      rows: [],
      workspaceLabel: undefined,
      active: false,
      seeding: false,
    };
  }

  const active = PREVIEW_PHASES.has(phase);
  const seeding = phase === "seed";

  if (!(active || seeding)) {
    return {
      rows: [],
      workspaceLabel: undefined,
      active: false,
      seeding: false,
    };
  }

  const answers = state.form_answers ?? {};
  const rows: PreviewRow[] = [];

  // Channel preview: derive from blueprint_id if chosen
  const blueprintId = answers.blueprint_id;
  if (blueprintId && blueprintId !== "scratch") {
    // Blueprint channels are known ahead of time; we preview the canonical
    // ones matching the blueprint slug.
    const blueprintChannels = BLUEPRINT_CHANNEL_PREVIEW[blueprintId];
    if (blueprintChannels) {
      for (const ch of blueprintChannels) {
        rows.push({ kind: "channel", label: ch });
      }
    }
  } else {
    // Scratch path: always has #general
    rows.push({ kind: "channel", label: "#general" });
  }

  // Agent preview: picked_agents from team trim
  const pickedAgents = answers.picked_agents ?? [];
  for (const agent of pickedAgents) {
    rows.push({ kind: "agent", label: agent });
  }

  return {
    rows,
    workspaceLabel: answers.company_name || undefined,
    active,
    seeding,
  };
}

/**
 * Known blueprint → preview channel names.
 * These mirror the blueprint scaffold definitions in the broker.
 * Updated when blueprints change; de-syncs are benign (preview vs real).
 */
const BLUEPRINT_CHANNEL_PREVIEW: Record<string, string[]> = {
  bookkeeping: ["#billing", "#reports"],
  "content-ops": ["#editorial", "#assets"],
  "engineering-team": ["#engineering", "#standup"],
};
