import {
  createHashHistory,
  createRootRoute,
  createRoute,
  createRouter,
  type RouterHistory,
  redirect,
} from "@tanstack/react-router";

import { ROUTE_PATHS } from "../routes/routeRegistry";

// Root route - the app shell will wrap everything once RouterProvider mounts.
export const rootRoute = createRootRoute();

// /channels/$channelSlug — main message view
export const channelRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.channel,
});

// /dm/$agentSlug — direct-message view
export const dmRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.dm,
});

// /apps/$appId — app panel view (tasks, policies, calendar, etc.)
export const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.app,
});

export const tasksRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.tasks,
});

export const taskDetailRoute = createRoute({
  getParentRoute: () => tasksRoute,
  path: "$taskId",
});

export const appTaskDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.appTaskDetail,
});

export const legacyWorkbenchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.legacyWorkbench,
  beforeLoad: () => {
    throw redirect({ to: "/tasks", replace: true });
  },
});

export const legacyWorkbenchAgentRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.legacyWorkbenchAgent,
  beforeLoad: () => {
    throw redirect({ to: "/tasks", replace: true });
  },
});

export const legacyWorkbenchTaskRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.legacyWorkbenchTask,
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/tasks/$taskId",
      params: { taskId: params.taskId },
      replace: true,
    });
  },
});

// Wiki, notebook, and review routes.
export const wikiRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.wiki,
});

export const wikiIndexRoute = createRoute({
  getParentRoute: () => wikiRoute,
  path: "/",
});

export const wikiLookupRoute = createRoute({
  getParentRoute: () => wikiRoute,
  path: "lookup",
  // Validate the `q` search param at the route boundary so consumers
  // don't each hand-narrow `unknown`. TanStack lifts the inferred type
  // through useSearch / useMatch / useMatches.
  validateSearch: (search: Record<string, unknown>): { q?: string } => ({
    q: typeof search.q === "string" ? search.q : undefined,
  }),
});

export const wikiArticleRoute = createRoute({
  getParentRoute: () => wikiRoute,
  path: "$",
});

export const notebooksRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.notebooks,
});

export const notebookAgentRoute = createRoute({
  getParentRoute: () => notebooksRoute,
  path: "$agentSlug",
});

export const notebookEntryRoute = createRoute({
  getParentRoute: () => notebookAgentRoute,
  path: "$entrySlug",
});

export const reviewsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.reviews,
});

// /inbox — Decision Inbox (Lane G, multi-agent control loop)
export const inboxRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.inbox,
});

// /task/$taskId — Decision Packet view
export const taskDecisionRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.taskDecision,
});

// / — index route. Always redirects to /channels/general at the route
// level so the root never has to render an empty body. Uses redirect()
// from beforeLoad: this fires before the route mounts, so URL→store
// race conditions can't observe the index match.
export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.index,
  beforeLoad: () => {
    throw redirect({
      to: "/channels/$channelSlug",
      params: { channelSlug: "general" },
      replace: true,
    });
  },
});

// Route tree
export const routeTree = rootRoute.addChildren([
  indexRoute,
  channelRoute,
  dmRoute,
  tasksRoute.addChildren([taskDetailRoute]),
  appTaskDetailRoute,
  legacyWorkbenchRoute,
  legacyWorkbenchAgentRoute,
  legacyWorkbenchTaskRoute,
  appRoute,
  wikiRoute.addChildren([wikiIndexRoute, wikiLookupRoute, wikiArticleRoute]),
  notebooksRoute.addChildren([
    notebookAgentRoute.addChildren([notebookEntryRoute]),
  ]),
  reviewsRoute,
  inboxRoute,
  taskDecisionRoute,
]);

export function createAppRouter(history: RouterHistory = createHashHistory()) {
  // No `defaultPreload`: route panels are React.lazy-loaded and
  // TanStack's preload-on-intent only preloads route loaders, of which
  // we have none. Including it would imply hover-preload of the lazy
  // chunks, which it does not do — chunks load on render.
  return createRouter({ routeTree, history });
}

export const router = createAppRouter();

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
