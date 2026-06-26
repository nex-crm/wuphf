import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  GOVERNOR_QUERY_KEY,
  type GovernorAction,
  type GovernorActionOptions,
  type GovernorStatus,
  getGovernor,
  postGovernor,
} from "../api/governor";
import { showNotice } from "../components/ui/Toast";
import { useAppStore } from "../stores/app";

function actionVerb(action: GovernorAction): string {
  switch (action) {
    case "pause":
      return "pause";
    case "stop":
      return "stop";
    default:
      return "resume";
  }
}

/**
 * useGovernor reads the session run-control state (budget/turn checkpoints and
 * pause/stop). The broker pushes a "governor" SSE event on every change, which
 * useBrokerEvents invalidates GOVERNOR_QUERY_KEY for — so the slow polling
 * interval is only a safety net for a missed event.
 */
export function useGovernor() {
  const brokerConnected = useAppStore((s) => s.brokerConnected);
  return useQuery<GovernorStatus>({
    queryKey: GOVERNOR_QUERY_KEY,
    queryFn: () => getGovernor(),
    enabled: brokerConnected,
    refetchInterval: 30_000,
  });
}

interface GovernorActionInput {
  action: GovernorAction;
  options?: GovernorActionOptions;
}

/**
 * useGovernorAction sends a pause/stop/resume command and seeds the cache with
 * the returned status so the UI reflects the new state without waiting for the
 * SSE round-trip.
 */
export function useGovernorAction() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ action, options }: GovernorActionInput) =>
      postGovernor(action, options),
    onSuccess: (status) => {
      queryClient.setQueryData(GOVERNOR_QUERY_KEY, status);
    },
    onError: (error: unknown, { action }) => {
      // A control action (Pause/Stop/Resume) failing silently is dangerous —
      // the user may believe the team stopped when it is still running.
      const detail = error instanceof Error ? error.message : "request failed";
      showNotice(
        `Could not ${actionVerb(action)} the team: ${detail}`,
        "error",
      );
    },
  });
}
