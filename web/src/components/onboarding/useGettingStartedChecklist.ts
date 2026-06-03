/**
 * useGettingStartedChecklist — reads the post-onboarding "Settle into your
 * office" checklist and exposes mutations to mark an item done or dismiss
 * the whole panel.
 *
 * Wire surface (all already shipped on the Go side — see
 * internal/onboarding/handlers.go):
 *   GET  /onboarding/state                       -> { checklist, checklist_dismissed, ... }
 *   POST /onboarding/checklist/{id}/done         -> { status: "ok" }
 *   POST /onboarding/checklist/dismiss           -> { status: "ok" }
 *
 * The query reuses the shared ["onboarding-state"] key (same key as
 * useOnboardingState) so a single fetch backs both consumers and a mutation
 * invalidation refreshes the sidebar overlay + this panel together.
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { get, post } from "../../api/client";
import type { OnboardingChecklistItem, OnboardingState } from "./types";

/** Shared with useOnboardingState — keep in lockstep so caches coalesce. */
export const ONBOARDING_STATE_QUERY_KEY = ["onboarding-state"] as const;

const CHECKLIST_STALE_MS = 3_000;

export interface GettingStartedChecklistState {
  /** Checklist rows from the server (id + done; label overlaid in the UI). */
  items: OnboardingChecklistItem[];
  /** True once the user has closed the panel. */
  dismissed: boolean;
  /** True while the underlying state query is still loading for the first time. */
  isLoading: boolean;
  /** Mark a single item complete by id. Idempotent on the server. */
  markItemDone: (id: string) => void;
  /** Dismiss the whole panel ("I am settled in"). */
  dismiss: () => void;
}

export function useGettingStartedChecklist(): GettingStartedChecklistState {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: ONBOARDING_STATE_QUERY_KEY,
    queryFn: () => get<OnboardingState>("/onboarding/state"),
    staleTime: CHECKLIST_STALE_MS,
  });

  const invalidate = () =>
    void queryClient.invalidateQueries({
      queryKey: ONBOARDING_STATE_QUERY_KEY,
    });

  const markDoneMutation = useMutation<{ status: string }, Error, string>({
    mutationFn: (id: string) =>
      post<{ status: string }>(
        `/onboarding/checklist/${encodeURIComponent(id)}/done`,
      ),
    onSuccess: invalidate,
  });

  const dismissMutation = useMutation<{ status: string }, Error, void>({
    mutationFn: () => post<{ status: string }>("/onboarding/checklist/dismiss"),
    onSuccess: invalidate,
  });

  return {
    items: query.data?.checklist ?? [],
    dismissed: query.data?.checklist_dismissed ?? false,
    isLoading: query.isLoading,
    markItemDone: (id: string) => markDoneMutation.mutate(id),
    dismiss: () => dismissMutation.mutate(),
  };
}
