import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createHashHistory,
  createRouter,
  RouterProvider,
  type RouterHistory,
} from "@tanstack/react-router";
import { lazy, Suspense } from "react";

import { BrokerBootstrapProvider } from "../bootstrap/BrokerBootstrapProvider.tsx";
import { desktopQueryClient } from "../query/queryClient.ts";
import { BrokerStreamStateProvider } from "../sse/useBrokerEvents.ts";
import { rootRoute } from "./routes/__root.tsx";
import { indexRoute } from "./routes/index.tsx";
import { threadsRoute } from "./routes/threads.tsx";

const routeTree = rootRoute.addChildren([indexRoute, threadsRoute]);
const ReactQueryDevtools = import.meta.env.DEV
  ? lazy(() =>
      import("@tanstack/react-query-devtools").then((module) => ({
        default: module.ReactQueryDevtools,
      })),
    )
  : null;

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
          {ReactQueryDevtools !== null && (
            <Suspense fallback={null}>
              <ReactQueryDevtools buttonPosition="bottom-left" />
            </Suspense>
          )}
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
