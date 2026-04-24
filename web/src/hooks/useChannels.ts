import { useQuery } from "@tanstack/react-query";

import type { Channel } from "../api/client";
import { getChannels } from "../api/client";

export function useChannels() {
  return useQuery({
    queryKey: ["channels"],
    queryFn: () => getChannels(),
    refetchInterval: 10000,
    select: (data) => data.channels ?? [],
  });
}

export type { Channel };
