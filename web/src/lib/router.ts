import {
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";

// Root route — the app shell wraps everything
export const rootRoute = createRootRoute();

// /channels/$channelSlug — main message view
export const channelRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/channels/$channelSlug",
});

// /apps/$appId — app panel view (tasks, policies, calendar, etc.)
export const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/apps/$appId",
});

// /agents/$agentSlug — agent detail panel
export const agentRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/agents/$agentSlug",
});

// / — index route (defaults to #general)
export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
});

// Route tree
const routeTree = rootRoute.addChildren([
  indexRoute,
  channelRoute,
  appRoute,
  agentRoute,
]);

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  // Use hash-based routing to match the legacy behavior and work with Go's FileServer
  // which always serves index.html for all routes
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
