import { type UseQueryResult, useQuery } from "@tanstack/react-query";

import { getOfficeStats, type OfficeStats } from "../api/platform";
import { useAppStore } from "../stores/app";

/**
 * Shared poll cadence for the derived-stats endpoint. Matches the board
 * cadence (OFFICE_TASKS_REFETCH_MS) so the lane headers and the cards
 * they sit above never lag each other by more than one tick.
 */
export const OFFICE_STATS_REFETCH_MS = 10_000;

/**
 * Cache key shared by every consumer of /office/stats. One query, one
 * payload — the header strip, board lane headers, dashboard tiles,
 * inbox badge, and wiki home count all read the same numbers.
 */
export const OFFICE_STATS_QUERY_KEY = ["office-stats"] as const;

/**
 * Single source for every surface-level count in the shell (C1 fix for
 * the count-drift family: "header blocked=1 vs board 0", "wiki 0
 * articles vs 19", "6 active while all waiting"). The broker computes
 * the payload from the same indexes its list endpoints serve, so any
 * two surfaces consuming this hook are consistent by construction.
 */
export function useOfficeStats(): UseQueryResult<OfficeStats> {
  const brokerConnected = useAppStore((s) => s.brokerConnected);
  return useQuery<OfficeStats>({
    queryKey: OFFICE_STATS_QUERY_KEY,
    queryFn: () => getOfficeStats(),
    refetchInterval: OFFICE_STATS_REFETCH_MS,
    enabled: brokerConnected,
  });
}
