import { useQuery } from "@tanstack/react-query";

import { type AgentRequest, getAllRequests } from "../api/client";

export interface RequestsState {
  all: AgentRequest[];
  pending: AgentRequest[];
  blockingPending: AgentRequest | null;
}

const REQUEST_REFETCH_MS = 5_000;

// Global view of requests across every channel the human can access. The
// broker's blocking gate is CHANNEL-scoped (a blocking request 409s new
// chat in ITS channel only), but the interview bar still renders the
// cross-channel queue so a pending ask in another channel stays visible.
// Consumers that act on `blockingPending` should scope it to their own
// channel (see Composer) rather than treating it as office-wide.
export function useRequests(): RequestsState {
  const { data } = useQuery({
    queryKey: ["requests", "all"],
    queryFn: () => getAllRequests(),
    refetchInterval: REQUEST_REFETCH_MS,
  });

  const all = data?.requests ?? [];
  const pending = all.filter(
    (r) => !r.status || r.status === "open" || r.status === "pending",
  );
  const blockingPending = pending.find((r) => r.blocking) ?? null;

  return { all, pending, blockingPending };
}
