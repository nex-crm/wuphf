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

// `/tasks` is the first-class human work-item surface. The list lives at
// `/tasks`, with `/tasks/new` (creation) and `/tasks/$taskId` (detail) as
// child routes. (Reverses the prior consolidation that redirected /tasks
// to /issues — Task is the primary primitive now.)
export const tasksRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.tasks,
});

// /tasks/new — Task creation form. Must be listed BEFORE taskDetailRoute in
// the route tree so the static `new` segment wins over the dynamic
// `$taskId` placeholder.
export const taskNewRoute = createRoute({
  getParentRoute: () => tasksRoute,
  path: "new",
});

export const taskDetailRoute = createRoute({
  getParentRoute: () => tasksRoute,
  path: "$taskId",
});

// `/apps/tasks/$taskId` redirects to the canonical `/tasks/$taskId` detail
// route so old bookmarks keep working.
export const appTaskDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.appTaskDetail,
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/tasks/$taskId",
      params: { taskId: params.taskId },
      replace: true,
    });
  },
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

// /articles/$articleId — full-screen HTML article viewer.
// Renders a rich artifact (ra_xxx) at full page size via the shadow-DOM
// RichArtifactEmbed. Linked from chat artifact cards.
export const articleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.article,
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

// / — index route. v3 MVP: default landing is the CEO's subspace
// (Chat tab) instead of #general, so the operator's first surface is
// the strategy-and-intent chat with the CEO. Channels are demoted to
// "Legacy". Uses redirect() from beforeLoad so the URL→store race
// can't observe the index match.
export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.index,
  beforeLoad: () => {
    throw redirect({
      to: "/agents/$agentSlug",
      params: { agentSlug: "ceo" },
      replace: true,
    });
  },
});

// Back-compat redirects for the legacy /issues surface. Task is the
// primary primitive now; these forward old bookmarks and chat links to
// the canonical /tasks routes. legacyIssueNewRoute must be listed BEFORE
// legacyIssueDetailRoute so the static `new` segment wins over `$issueId`.
export const legacyIssuesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.legacyIssues,
  beforeLoad: () => {
    throw redirect({ to: "/tasks", replace: true });
  },
});

export const legacyIssueNewRoute = createRoute({
  getParentRoute: () => legacyIssuesRoute,
  path: "new",
  beforeLoad: () => {
    throw redirect({ to: "/tasks/new", replace: true });
  },
});

export const legacyIssueDetailRoute = createRoute({
  getParentRoute: () => legacyIssuesRoute,
  path: "$issueId",
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/tasks/$taskId",
      params: { taskId: params.issueId },
      replace: true,
    });
  },
});

// /routines — convenience alias that forwards to the /apps/routines panel.
// Lets users type `/routines` in the URL bar (or share it) and land on the
// canonical app surface without hitting "Page not found".
export const routinesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.routines,
  beforeLoad: () => {
    throw redirect({
      to: "/apps/$appId",
      params: { appId: "routines" },
      replace: true,
    });
  },
});

// /routines/new — composer page for creating a routine. Must be listed
// BEFORE routineDetailRoute in the route tree so the static `new`
// segment wins over the dynamic `$routineSlug` placeholder.
export const routineNewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.routineNew,
});

// /routines/$routineSlug — full-page routine detail surface. Routine
// slugs can contain `:` separators (e.g. `task-follow-up:general:task-1`);
// TanStack Router decodes the param value before it reaches the consumer.
export const routineDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.routineDetail,
});

// /skills/$skillName — full-screen skill SKILL.md detail editor + viewer.
// Renders SkillDetailRoute which lets the operator edit the body in raw
// markdown or read it as rendered HTML via a toggle.
export const skillDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.skillDetail,
});

// /agents/$agentSlug — v3 MVP per-agent subspace shell.
// Renders the uniform Chat | App | Notebooks | Calendar | Settings tabs.
// Empty tab segment lands on the Chat tab by default.
export const agentSubspaceRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.agentSubspace,
});

// /agents/$agentSlug/$tab — explicit tab segment.
// Nested under agentSubspaceRoute so $agentSlug is shared.
export const agentSubspaceTabRoute = createRoute({
  getParentRoute: () => agentSubspaceRoute,
  path: "$tab",
});

// Route tree
export const routeTree = rootRoute.addChildren([
  indexRoute,
  channelRoute,
  dmRoute,
  // /tasks list with static `new` before dynamic `$taskId`.
  tasksRoute.addChildren([taskNewRoute, taskDetailRoute]),
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
  articleRoute,
  inboxRoute,
  taskDecisionRoute,
  // Back-compat redirects for the legacy /issues surface.
  // legacyIssueNewRoute must be listed BEFORE legacyIssueDetailRoute so the
  // static segment "new" wins over the dynamic "$issueId" catch-all.
  legacyIssuesRoute.addChildren([legacyIssueNewRoute, legacyIssueDetailRoute]),
  routinesRoute,
  routineNewRoute,
  routineDetailRoute,
  // v3 MVP — per-agent subspace.
  agentSubspaceRoute.addChildren([agentSubspaceTabRoute]),
  // Skill detail (full-screen edit + render with raw/preview toggle).
  skillDetailRoute,
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
