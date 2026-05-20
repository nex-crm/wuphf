import { type UseQueryResult, useQuery } from "@tanstack/react-query";
import type { ThreadBoardColumn, ThreadListResponse, ThreadView } from "@wuphf/protocol/browser";
import { useMemo } from "react";

import { useBrokerApiClient } from "../api/client.ts";
import { threadListQuery } from "../query/queries.ts";

export interface UseThreadListResult {
  readonly query: UseQueryResult<ThreadListResponse, Error>;
  readonly threadsByColumn: Readonly<Record<ThreadBoardColumn, readonly ThreadView[]>>;
  readonly totalCount: number;
}

// Reads the current folded thread list and partitions by
// `boardColumn`. SSE-driven invalidation is already wired up at
// `useBrokerEvents`; this hook does not need to subscribe explicitly.
export function useThreadList(): UseThreadListResult {
  const client = useBrokerApiClient();
  const query = useQuery<ThreadListResponse, Error>(threadListQuery(client));

  const threadsByColumn = useMemo(
    () => partitionByColumn(query.data?.threads ?? []),
    [query.data?.threads],
  );

  const totalCount = query.data?.threads.length ?? 0;
  return { query, threadsByColumn, totalCount };
}

export function partitionByColumn(
  threads: readonly ThreadView[],
): Readonly<Record<ThreadBoardColumn, readonly ThreadView[]>> {
  const buckets: Record<ThreadBoardColumn, ThreadView[]> = {
    needs_me: [],
    running: [],
    review: [],
    done: [],
  };
  for (const thread of threads) {
    buckets[thread.boardColumn].push(thread);
  }
  return buckets;
}
