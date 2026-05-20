import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createHashHistory,
  createRouter,
  RouterProvider,
  type RouterHistory,
} from "@tanstack/react-router";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";

import { BrokerBootstrapProvider } from "../bootstrap/BrokerBootstrapProvider.tsx";
import { desktopQueryClient } from "../query/queryClient.ts";
import { BrokerStreamStateProvider } from "../sse/useBrokerEvents.ts";
import { rootRoute } from "./routes/__root.tsx";
import { indexRoute } from "./routes/index.tsx";

const routeTree = rootRoute.addChildren([indexRoute]);

export function createAppRouter(history: RouterHistory = createHashHistory()) {
  return createRouter({ routeTree, history });
}

export const router = createAppRouter();

export interface AppProps {
  readonly queryClient?: QueryClient;
  readonly routerInstance?: typeof router;
}

export function App({
  queryClient = desktopQueryClient,
  routerInstance = router,
}: AppProps) {
  return (
    <QueryClientProvider client={queryClient}>
      <BrokerStreamStateProvider>
        <BrokerBootstrapProvider>
          <RouterProvider router={routerInstance} />
          {import.meta.env.DEV && <ReactQueryDevtools buttonPosition="bottom-left" />}
        </BrokerBootstrapProvider>
      </BrokerStreamStateProvider>
    </QueryClientProvider>
  );
}

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
