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

// Wiki routes.
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

// / — index route. Tasks are the primary primitive, so the default
// landing is the new-task composer: a centered chatbox where the operator
// describes an outcome and chooses how it runs. Their work board lives one
// click away at /tasks. The route renders (no redirect) so the composer is
// the literal home; useCurrentRoute derives {kind: "home"} for it.
export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.index,
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
  // Optional prefill carried from the new-task composer's "Routine" action so
  // the user does not retype what they just described. Both keys are optional;
  // a bare /routines/new still renders an empty composer.
  validateSearch: (
    search: Record<string, unknown>,
  ): { label?: string; instructions?: string } => ({
    label: typeof search.label === "string" ? search.label : undefined,
    instructions:
      typeof search.instructions === "string" ? search.instructions : undefined,
  }),
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

// /agents — Agents tool. Roster grid of every agent (CEO, Librarian,
// specialists). Replaces the per-agent chat subspace: agents are
// first-class, configured here, but they are not chat surfaces. The
// detail page (/agents/$agentSlug) is mounted as a child so the static
// index and the dynamic detail share the same `/agents` prefix.
export const agentsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: ROUTE_PATHS.agents,
});

// /agents/$agentSlug — agent detail / config (provider, role, persona,
// skills). Child of agentsRoute.
export const agentDetailRoute = createRoute({
  getParentRoute: () => agentsRoute,
  path: "$agentSlug",
});

// /agents/$agentSlug/$tab — tabbed subspace view. Child of agentDetailRoute.
// Tab values: chat | tasks | skills | policies | live-stream | config.
export const agentDetailTabRoute = createRoute({
  getParentRoute: () => agentDetailRoute,
  path: "$tab",
});

// Route tree
export const routeTree = rootRoute.addChildren([
  indexRoute,
  channelRoute,
  // /tasks list with static `new` before dynamic `$taskId`.
  tasksRoute.addChildren([taskNewRoute, taskDetailRoute]),
  appTaskDetailRoute,
  legacyWorkbenchRoute,
  legacyWorkbenchAgentRoute,
  legacyWorkbenchTaskRoute,
  appRoute,
  wikiRoute.addChildren([wikiIndexRoute, wikiLookupRoute, wikiArticleRoute]),
  articleRoute,
  inboxRoute,
  taskDecisionRoute,
  routinesRoute,
  routineNewRoute,
  routineDetailRoute,
  // Agents tool: roster (/agents) with the detail/config page as a child,
  // and the tabbed subspace (/agents/$agentSlug/$tab) as a grandchild.
  agentsRoute.addChildren([
    agentDetailRoute.addChildren([agentDetailTabRoute]),
  ]),
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
