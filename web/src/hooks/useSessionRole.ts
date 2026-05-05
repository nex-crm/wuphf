import { useQuery } from "@tanstack/react-query";

import { getHumanMe, type HumanMe } from "../api/platform";

// Shared key + cadence with HealthCheckApp so TanStack dedupes the
// /humans/me request. If you change either side, change both.
const HUMAN_ME_QUERY_KEY = ["humans", "me"] as const;
const HUMAN_ME_REFETCH_MS = 30_000;

export type SessionRole = "host" | "member" | "unknown";

export interface SessionInfo {
  role: SessionRole;
  human: HumanMe["human"] | undefined;
  isLoading: boolean;
}

export function useSessionRole(): SessionInfo {
  const { data, isLoading } = useQuery({
    queryKey: HUMAN_ME_QUERY_KEY,
    queryFn: () => getHumanMe(),
    refetchInterval: HUMAN_ME_REFETCH_MS,
  });
  const human = data?.human;
  const role: SessionRole =
    human?.role === "host"
      ? "host"
      : human?.role === "member"
        ? "member"
        : "unknown";
  return { role, human, isLoading };
}
