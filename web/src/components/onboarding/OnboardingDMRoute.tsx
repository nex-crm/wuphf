/**
 * OnboardingDMRoute — Phase 2 entry point for the deterministic CEO conversation.
 *
 * This is a thin wrapper around DMView that:
 *   1. Points DMView at the reserved channel `dm:ceo:onboarding`
 *   2. Exposes the PendingSuggestion context so InterviewBar can render
 *      CEO cards (ceo_form_field, ceo_chip_row, ceo_checklist, etc.)
 *   3. Reads onboarding state on mount and subscribes to updates
 *
 * The spec hard-rule (docs/specs/onboarding-into-office.md "Eng review
 * decisions"):
 *   "Frontend reuses DMView for the CEO chat. A small OnboardingDMRoute
 *    wrapper provides the preview-overlay context and points DMView at
 *    the reserved dm:ceo:onboarding channel."
 *
 * No new chat shell, no new composer, no duplicated SSE/scroll/optimistic-post
 * code. CEO DM inherits all of that from DMView.
 */

import { createContext, useContext } from "react";

import { DMView } from "../messages/DMView";
import type { CeoSuggestion } from "./types";
import { useOnboardingState } from "./useOnboardingState";

// ── Onboarding context ─────────────────────────────────────────────────────

interface OnboardingDMContextValue {
  phase: string | undefined;
  pendingSuggestion: CeoSuggestion | null;
}

const OnboardingDMContext = createContext<OnboardingDMContextValue>({
  phase: undefined,
  pendingSuggestion: null,
});

/** Read the current onboarding phase + pending suggestion from within the CEO DM. */
export function useOnboardingDMContext(): OnboardingDMContextValue {
  return useContext(OnboardingDMContext);
}

/**
 * Exported for test use only: wrap children with a custom context value
 * without needing to mount the full OnboardingDMRoute + DMView stack.
 */
export const OnboardingDMContextProvider = OnboardingDMContext.Provider;

// ── Reserved channel slug ──────────────────────────────────────────────────

/** The broker reserves this slug for the onboarding CEO DM. */
const CEO_ONBOARDING_CHANNEL = "dm:ceo:onboarding";

/** The agent slug for the CEO (matches existing broker configuration). */
const CEO_AGENT_SLUG = "ceo";

// ── Component ─────────────────────────────────────────────────────────────

/**
 * Renders the CEO DM inside the Shell body during Phase 2 onboarding.
 * Wraps DMView with the OnboardingDMContext so child components (e.g.
 * InterviewBar) can access the current phase and pending suggestion.
 */
export function OnboardingDMRoute() {
  const { data: state } = useOnboardingState();

  const pendingSuggestion = parsePendingSuggestion(state?.pending_suggestion);

  return (
    <OnboardingDMContext.Provider
      value={{
        phase: state?.phase,
        pendingSuggestion,
      }}
    >
      <div
        className="onboarding-dm-route"
        data-testid="onboarding-dm-route"
        data-phase={state?.phase ?? "loading"}
      >
        <DMView
          agentSlug={CEO_AGENT_SLUG}
          channelSlug={CEO_ONBOARDING_CHANNEL}
        />
      </div>
    </OnboardingDMContext.Provider>
  );
}

// ── Helpers ────────────────────────────────────────────────────────────────

/**
 * Safely parse the pending_suggestion field from the API response.
 * The backend stores it as json.RawMessage; the fetch gives us `unknown`.
 * We narrow to CeoSuggestion only if the shape is correct.
 */
function parsePendingSuggestion(raw: unknown): CeoSuggestion | null {
  if (!raw || typeof raw !== "object") return null;
  const obj = raw as Record<string, unknown>;
  if (typeof obj.id !== "string" || typeof obj.kind !== "string") return null;
  // Minimal shape check — the kind dispatcher in InterviewBar will validate
  // the payload before rendering.
  return raw as CeoSuggestion;
}
