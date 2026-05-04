import { useMatches } from "@tanstack/react-router";

import {
  appRoute,
  channelRoute,
  dmRoute,
  notebookAgentRoute,
  notebookEntryRoute,
  notebooksRoute,
  reviewsRoute,
  wikiArticleRoute,
  wikiIndexRoute,
  wikiLookupRoute,
  workbenchAgentRoute,
  workbenchRoute,
  workbenchTaskRoute,
} from "../lib/router";
import { directChannelSlug } from "../stores/app";

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
  | { kind: "dm"; agentSlug: string; channelSlug: string }
  | { kind: "app"; appId: string }
  | { kind: "workbench"; agentSlug: string | null; taskId: string | null }
  | { kind: "wiki" }
  | { kind: "wiki-article"; articlePath: string }
  | { kind: "wiki-lookup"; query: string | null }
  | { kind: "notebook-catalog" }
  | { kind: "notebook-agent"; agentSlug: string }
  | { kind: "notebook-entry"; agentSlug: string; entrySlug: string }
  | { kind: "reviews" }
  | { kind: "unknown" };

interface ParamsShape {
  channelSlug?: string;
  agentSlug?: string;
  appId?: string;
  entrySlug?: string;
  taskId?: string;
  _splat?: string;
}

interface SearchShape {
  q?: unknown;
}

type RouteDeriver = (params: ParamsShape, search: SearchShape) => CurrentRoute;
type CurrentRouteId =
  | typeof channelRoute.id
  | typeof dmRoute.id
  | typeof appRoute.id
  | typeof workbenchRoute.id
  | typeof workbenchAgentRoute.id
  | typeof workbenchTaskRoute.id
  | typeof wikiIndexRoute.id
  | typeof wikiLookupRoute.id
  | typeof wikiArticleRoute.id
  | typeof notebooksRoute.id
  | typeof notebookAgentRoute.id
  | typeof notebookEntryRoute.id
  | typeof reviewsRoute.id;

const CURRENT_ROUTE_IDS = [
  channelRoute.id,
  dmRoute.id,
  appRoute.id,
  workbenchRoute.id,
  workbenchAgentRoute.id,
  workbenchTaskRoute.id,
  wikiIndexRoute.id,
  wikiLookupRoute.id,
  wikiArticleRoute.id,
  notebooksRoute.id,
  notebookAgentRoute.id,
  notebookEntryRoute.id,
  reviewsRoute.id,
] as const satisfies readonly CurrentRouteId[];

const CURRENT_ROUTE_ID_SET = new Set<string>(CURRENT_ROUTE_IDS);

function isCurrentRouteId(routeId: string): routeId is CurrentRouteId {
  return CURRENT_ROUTE_ID_SET.has(routeId);
}

const ROUTE_DERIVERS = {
  [channelRoute.id]: (params) => ({
    kind: "channel",
    channelSlug: params.channelSlug ?? "general",
  }),
  [dmRoute.id]: (params) => {
    const agentSlug = params.agentSlug ?? "";
    return {
      kind: "dm",
      agentSlug,
      channelSlug: directChannelSlug(agentSlug),
    };
  },
  [appRoute.id]: (params) => ({ kind: "app", appId: params.appId ?? "" }),
  [workbenchRoute.id]: () => ({
    kind: "workbench",
    agentSlug: null,
    taskId: null,
  }),
  [workbenchAgentRoute.id]: (params) => ({
    kind: "workbench",
    agentSlug: params.agentSlug ?? null,
    taskId: null,
  }),
  [workbenchTaskRoute.id]: (params) => ({
    kind: "workbench",
    agentSlug: params.agentSlug ?? null,
    taskId: params.taskId ?? null,
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
  if (route.kind === "dm") return route.channelSlug;
  return null;
}

/**
 * Compatibility shape for code that previously read `currentApp` from
 * the store. Returns:
 *   - an app panel id for /apps/$appId,
 *   - "workbench" for /apps/workbench route variants,
 *   - "wiki" for any wiki article or catalog route,
 *   - "wiki-lookup" for /wiki/lookup,
 *   - "notebooks" for any notebook route,
 *   - "reviews" for /reviews,
 *   - null when the matched route is a conversation (channel/dm) or
 *     unknown.
 */
export function useCurrentApp(): string | null {
  const route = useCurrentRoute();
  switch (route.kind) {
    case "app":
      return route.appId;
    case "workbench":
      return "workbench";
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
    case "channel":
    case "dm":
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
