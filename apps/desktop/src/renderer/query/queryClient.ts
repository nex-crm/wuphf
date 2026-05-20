import { QueryClient } from "@tanstack/react-query";

export function createDesktopQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 2_000,
        retry: 1,
        // SSE invalidations are the desktop freshness source; individual queries can opt in.
        refetchOnReconnect: false,
        refetchOnWindowFocus: false,
      },
    },
  });
}

export const desktopQueryClient = createDesktopQueryClient();
