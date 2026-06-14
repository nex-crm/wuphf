import { useMatches } from "@tanstack/react-router";

import {
  agentDetailRoute,
  agentDetailTabRoute,
  agentsRoute,
  appRoute,
  appTaskDetailRoute,
  articleRoute,
  channelRoute,
  inboxRoute,
  indexRoute,
  notebookAgentRoute,
  notebookEntryRoute,
  notebooksRoute,
  reviewsRoute,
  routineDetailRoute,
  routineNewRoute,
  skillDetailRoute,
  taskDecisionRoute,
  taskDetailRoute,
  taskNewRoute,
  tasksRoute,
  wikiArticleRoute,
  wikiIndexRoute,
  wikiLookupRoute,
} from "../lib/router";
import { useAppStore } from "../stores/app";

/**
 * Discriminated union describing the matched leaf route. Replaces the
 * legacy `currentApp` / `currentChannel` / `wikiPath` / `wikiLookupQuery` /
 * `notebookAgentSlug` / `notebookEntrySlug` scattered across the Zustand
 * store with one URL-driven shape that components can pattern-match on.
 *
 * Step 4 of the route migration deletes those store fields; everything
 * downstream reads its identifiers via this hook (or the convenience
 * helpers below) instead.
 */
export type CurrentRoute =
  | { kind: "channel"; channelSlug: string }
  | { kind: "app"; appId: string }
  // New-task home composer (index route).
  | { kind: "home" }
  | { kind: "task-board" }
  | { kind: "task-detail"; taskId: string }
  | { kind: "task-new" }
  | { kind: "wiki" }
  | { kind: "wiki-article"; articlePath: string }
  | { kind: "wiki-lookup"; query: string | null }
  | { kind: "notebook-catalog" }
  | { kind: "notebook-agent"; agentSlug: string }
  | { kind: "notebook-entry"; agentSlug: string; entrySlug: string }
  | { kind: "reviews" }
  | { kind: "article"; articleId: string }
  | { kind: "inbox" }
  | { kind: "task-decision"; taskId: string }
  // Agents tool — roster grid + per-agent config/detail page.
  | { kind: "agents" }
  | { kind: "agent-detail"; agentSlug: string; tab?: string }
  // Full-screen skill detail editor + viewer.
  | { kind: "skill-detail"; skillName: string }
  | { kind: "routine-detail"; routineSlug: string }
  | { kind: "routine-new" }
  | { kind: "unknown" };

interface ParamsShape {
  channelSlug?: string;
  agentSlug?: string;
  appId?: string;
  entrySlug?: string;
  taskId?: string;
  _splat?: string;
  articleId?: string;
  tab?: string;
  skillName?: string;
  routineSlug?: string;
}

interface SearchShape {
  q?: unknown;
}

type RouteDeriver = (params: ParamsShape, search: SearchShape) => CurrentRoute;
type CurrentRouteId =
  | typeof indexRoute.id
  | typeof channelRoute.id
  | typeof appRoute.id
  | typeof tasksRoute.id
  | typeof taskDetailRoute.id
  | typeof taskNewRoute.id
  | typeof appTaskDetailRoute.id
  | typeof wikiIndexRoute.id
  | typeof wikiLookupRoute.id
  | typeof wikiArticleRoute.id
  | typeof notebooksRoute.id
  | typeof notebookAgentRoute.id
  | typeof notebookEntryRoute.id
  | typeof reviewsRoute.id
  | typeof articleRoute.id
  | typeof inboxRoute.id
  | typeof taskDecisionRoute.id
  | typeof agentsRoute.id
  | typeof agentDetailRoute.id
  | typeof agentDetailTabRoute.id
  | typeof skillDetailRoute.id
  | typeof routineDetailRoute.id
  | typeof routineNewRoute.id;

const CURRENT_ROUTE_IDS = [
  indexRoute.id,
  channelRoute.id,
  appRoute.id,
  tasksRoute.id,
  taskDetailRoute.id,
  taskNewRoute.id,
  appTaskDetailRoute.id,
  wikiIndexRoute.id,
  wikiLookupRoute.id,
  wikiArticleRoute.id,
  notebooksRoute.id,
  notebookAgentRoute.id,
  notebookEntryRoute.id,
  reviewsRoute.id,
  articleRoute.id,
  inboxRoute.id,
  taskDecisionRoute.id,
  agentsRoute.id,
  agentDetailRoute.id,
  agentDetailTabRoute.id,
  skillDetailRoute.id,
  routineDetailRoute.id,
  routineNewRoute.id,
] as const satisfies readonly CurrentRouteId[];

const CURRENT_ROUTE_ID_SET = new Set<string>(CURRENT_ROUTE_IDS);

function isCurrentRouteId(routeId: string): routeId is CurrentRouteId {
  return CURRENT_ROUTE_ID_SET.has(routeId);
}

const ROUTE_DERIVERS = {
  [indexRoute.id]: () => ({ kind: "home" }),
  [channelRoute.id]: (params) => ({
    kind: "channel",
    channelSlug: params.channelSlug ?? "general",
  }),
  [appRoute.id]: (params) => ({ kind: "app", appId: params.appId ?? "" }),
  [tasksRoute.id]: () => ({ kind: "task-board" }),
  [taskDetailRoute.id]: (params) => ({
    kind: "task-detail",
    taskId: params.taskId ?? "",
  }),
  [taskNewRoute.id]: () => ({ kind: "task-new" }),
  // `/apps/tasks/$taskId` redirects before matching; the deriver mirrors
  // taskDetail for completeness so the registry type stays exhaustive.
  [appTaskDetailRoute.id]: (params) => ({
    kind: "task-detail",
    taskId: params.taskId ?? "",
  }),
  [wikiIndexRoute.id]: () => ({ kind: "wiki" }),
  [wikiLookupRoute.id]: (_params, search) => ({
    kind: "wiki-lookup",
    query: typeof search.q === "string" ? search.q : null,
  }),
  [wikiArticleRoute.id]: (params) => {
    // Empty splat (e.g. legacy `#/wiki/` URLs that landed on the
    // article route) renders the catalog rather than a `wiki-article`
    // surface with an empty path that would fetch the empty article.
    const splat = typeof params._splat === "string" ? params._splat : "";
    if (splat.length === 0) return { kind: "wiki" };
    return { kind: "wiki-article", articlePath: splat };
  },
  [notebooksRoute.id]: () => ({ kind: "notebook-catalog" }),
  [notebookAgentRoute.id]: (params) => ({
    kind: "notebook-agent",
    agentSlug: params.agentSlug ?? "",
  }),
  [notebookEntryRoute.id]: (params) => ({
    kind: "notebook-entry",
    agentSlug: params.agentSlug ?? "",
    entrySlug: params.entrySlug ?? "",
  }),
  [reviewsRoute.id]: () => ({ kind: "reviews" }),
  [articleRoute.id]: (params) => ({
    kind: "article",
    articleId: params.articleId ?? "",
  }),
  [inboxRoute.id]: () => ({ kind: "inbox" }),
  [taskDecisionRoute.id]: (params) => ({
    kind: "task-decision",
    taskId: params.taskId ?? "",
  }),
  // Agents tool — roster grid (/agents) + per-agent config (/agents/$slug)
  // + tabbed subspace (/agents/$slug/$tab).
  [agentsRoute.id]: () => ({ kind: "agents" }),
  [agentDetailRoute.id]: (params) => ({
    kind: "agent-detail",
    agentSlug: params.agentSlug ?? "",
    tab: undefined,
  }),
  [agentDetailTabRoute.id]: (params) => ({
    kind: "agent-detail",
    agentSlug: params.agentSlug ?? "",
    tab: params.tab,
  }),
  [skillDetailRoute.id]: (params) => ({
    kind: "skill-detail",
    skillName: params.skillName ?? "",
  }),
  [routineDetailRoute.id]: (params) => ({
    kind: "routine-detail",
    routineSlug: params.routineSlug ?? "",
  }),
  [routineNewRoute.id]: () => ({ kind: "routine-new" }),
} satisfies Record<CurrentRouteId, RouteDeriver>;

/**
 * Pure URL→state dispatch. Exported for unit tests so we can pin the
 * shape per-route without spinning up a full RouterProvider.
 */
export function deriveCurrentRoute(
  routeId: string,
  params: ParamsShape,
  search: SearchShape,
): CurrentRoute {
  if (!isCurrentRouteId(routeId)) return { kind: "unknown" };
  return ROUTE_DERIVERS[routeId](params, search);
}

export function useCurrentRoute(): CurrentRoute {
  const matches = useMatches();
  const leaf = matches.at(-1);
  return deriveCurrentRoute(
    leaf?.routeId ?? "",
    (leaf?.params as ParamsShape) ?? {},
    (leaf?.search as SearchShape) ?? {},
  );
}

/**
 * Resolve the broker channel slug for the matched conversation route.
 * Returns null for non-conversation routes (apps, wiki, notebooks, etc.),
 * so callers that need a slug can fall back to "general" or skip work.
 */
export function useChannelSlug(): string | null {
  const route = useCurrentRoute();
  if (route.kind === "channel") return route.channelSlug;
  return null;
}

/**
 * Resolve the task id for the currently-viewed task-detail route, or null
 * when the matched route is anything else. Chat cards use this to detect
 * when a Task pointer (created / lifecycle card) refers to the very task
 * whose channel is already on screen, so a self-referential "Open →" can
 * be suppressed (created card) or rendered inert (lifecycle card).
 */
export function useCurrentTaskId(): string | null {
  const route = useCurrentRoute();
  if (route.kind === "task-detail" && route.taskId) return route.taskId;
  return null;
}

/**
 * Channel slug consumers that work outside conversation routes (thread
 * panels, request badges, cross-surface actions) should use this hook so
 * they keep pointing at the user's last-visited channel rather than silently
 * collapsing to `"general"` whenever the URL is on `/apps/...` or
 * `/wiki/...`. Falls through to `"general"` only on a cold start where
 * the user has not yet visited any conversation route.
 */
export function useFallbackChannelSlug(): string {
  const route = useCurrentRoute();
  const lastChannel = useAppStore((s) => s.lastConversationalChannel);
  if (route.kind === "channel") return route.channelSlug;
  return lastChannel ?? "general";
}

/**
 * Compatibility shape for code that previously read `currentApp` from
 * the store. Returns:
 *   - an app panel id for /apps/$appId,
 *   - "tasks" for /tasks route variants,
 *   - "wiki" for any wiki article or catalog route,
 *   - "wiki-lookup" for /wiki/lookup,
 *   - "notebooks" for any notebook route,
 *   - "reviews" for /reviews,
 *   - null when the matched route is a conversation (channel) or
 *     unknown.
 */
export function useCurrentApp(): string | null {
  const route = useCurrentRoute();
  switch (route.kind) {
    case "app":
      return route.appId;
    case "task-board":
    case "task-detail":
    case "task-new":
      return "tasks";
    case "wiki":
    case "wiki-article":
      return "wiki";
    case "wiki-lookup":
      return "wiki-lookup";
    case "notebook-catalog":
    case "notebook-agent":
    case "notebook-entry":
      return "notebooks";
    case "reviews":
      return "reviews";
    case "article":
      return "article";
    case "inbox":
      return "inbox";
    case "task-decision":
      return "inbox";
    case "routine-detail":
    case "routine-new":
      return "routines";
    case "agents":
    case "agent-detail":
      return "agents";
    case "home":
    case "channel":
    case "skill-detail":
    case "unknown":
      return null;
    default: {
      // Exhaustiveness check — see MainContent's matching switch.
      const _exhaustive: never = route;
      void _exhaustive;
      return null;
    }
  }
}
