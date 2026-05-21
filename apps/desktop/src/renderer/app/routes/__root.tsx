import { createRootRoute, Outlet } from "@tanstack/react-router";

import { AppShell } from "../AppShell.tsx";
import { useBrokerEvents } from "../../sse/useBrokerEvents.ts";

function RootLayout() {
  useBrokerEvents();
  return (
    <AppShell>
      <Outlet />
    </AppShell>
  );
}

export const rootRoute = createRootRoute({
  component: RootLayout,
});
