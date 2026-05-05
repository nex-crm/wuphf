import { useQuery } from "@tanstack/react-query";

import {
  getHumanMe,
  HUMAN_ME_QUERY_KEY,
  HUMAN_ME_REFETCH_MS,
  type HumanMe,
} from "../api/platform";

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
