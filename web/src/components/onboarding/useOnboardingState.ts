/**
 * useOnboardingState — loads /onboarding/state on mount and keeps it fresh.
 *
 * This hook is used by:
 *   - OnboardingDMRoute (for PendingSuggestion + Phase)
 *   - Sidebar preview overlay (for FormAnswers)
 *
 * The broker publishes phase transitions as SSE events. Since useBrokerEvents
 * already subscribes to SSE and the query cache refetches on focus/interval,
 * we use a 3-second poll interval matching the messages hook pattern. A
 * dedicated SSE event type for onboarding state would be Phase 2B backend work.
 */

import { useQuery } from "@tanstack/react-query";

import { get } from "../../api/client";
import type { OnboardingState } from "./types";

const ONBOARDING_STATE_STALE_MS = 3_000;

export function useOnboardingState() {
  return useQuery({
    queryKey: ["onboarding-state"],
    queryFn: () => get<OnboardingState>("/onboarding/state"),
    staleTime: ONBOARDING_STATE_STALE_MS,
    refetchInterval: ONBOARDING_STATE_STALE_MS,
  });
}
