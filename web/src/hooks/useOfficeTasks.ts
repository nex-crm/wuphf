import { type UseQueryResult, useQuery } from "@tanstack/react-query";

import { getOfficeTasks, type Task } from "../api/tasks";

/**
 * Shared poll interval for office tasks. One source of truth so future surfaces
 * (calendar, profile, overview, palette) do not each ship their own.
 *
 * 10s mirrors the legacy `TasksApp` board cadence — the most aggressive of the
 * three intervals previously in tree (10s board / 15s runtime strip / 30s
 * sidebar summary). Picking the most aggressive is intentional: those slower
 * surfaces shared the same React Query cache key already, so they were
 * implicitly inheriting whichever query mounted first. Standardising upward
 * preserves the snappy board behaviour without slowing the others down.
 */
export const OFFICE_TASKS_REFETCH_MS = 10_000;

/**
 * Cache key used by every consumer of the office task list. Existing
 * invalidators (`TaskDetailModal`, `useBrokerEvents`) already target this key,
 * so reusing it keeps refetch-on-write behaviour intact.
 */
export const OFFICE_TASKS_QUERY_KEY = ["office-tasks"] as const;

/**
 * Shared query hook for the office-wide task list. Centralises:
 *
 *   - the cache key (`OFFICE_TASKS_QUERY_KEY`)
 *   - the refetch interval (`OFFICE_TASKS_REFETCH_MS`)
 *   - the underlying API call (`getOfficeTasks` with `includeDone: true`)
 *
 * Returning `Task[]` (rather than the raw `TaskListResponse`) keeps the
 * contract aligned with the projection helpers in `lib/taskProjections`.
 *
 * Other Phase 5 surfaces (calendar, agent profile, office overview, command
 * palette) will consume this hook directly. This PR only migrates one
 * grouping path inside `TasksApp` to prove the shape works end-to-end.
 */
export function useOfficeTasks(): UseQueryResult<Task[]> {
  return useQuery<Task[]>({
    queryKey: OFFICE_TASKS_QUERY_KEY,
    queryFn: async () => {
      const response = await getOfficeTasks({ includeDone: true });
      return response.tasks ?? [];
    },
    refetchInterval: OFFICE_TASKS_REFETCH_MS,
  });
}
