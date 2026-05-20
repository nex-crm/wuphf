import { QueryClient } from "@tanstack/react-query";

export function createDesktopQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 2_000,
        retry: 1,
      },
    },
  });
}

export const desktopQueryClient = createDesktopQueryClient();
