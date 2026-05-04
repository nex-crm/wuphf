import {
  appRoute,
  channelRoute,
  dmRoute,
  notebookAgentRoute,
  notebookEntryRoute,
  notebooksRoute,
  reviewsRoute,
  rootRoute,
  wikiArticleRoute,
  wikiIndexRoute,
  wikiLookupRoute,
} from "../lib/router";
import {
  type AppStore,
  type ChannelMeta,
  directChannelSlug,
  isDMChannel,
  useAppStore,
} from "../stores/app";

// Sentinel routeId for the root match — TanStack Router exposes this as
// `__root__`. Imported via `rootRoute.id` so a future TanStack rename
// surfaces as a single broken reference instead of a silent string-match
// drift.
export const ROOT_ROUTE_ID = rootRoute.id;

// Subset of the navigation slice the URL↔store adapter cares about.
// Mirrored separately from AppStore so the bridge derivation function
// stays pure and testable without dragging in setters or unrelated UI
// state.
export interface NavSlice {
  currentApp: string | null;
  currentChannel: string;
  channelMeta: Record<string, ChannelMeta>;
  wikiPath: string | null;
  wikiLookupQuery: string | null;
  notebookAgentSlug: string | null;
  notebookEntrySlug: string | null;
}

export function pickNavSlice(s: AppStore): NavSlice {
  return {
    currentApp: s.currentApp,
    currentChannel: s.currentChannel,
    channelMeta: s.channelMeta,
    wikiPath: s.wikiPath,
    wikiLookupQuery: s.wikiLookupQuery,
    notebookAgentSlug: s.notebookAgentSlug,
    notebookEntrySlug: s.notebookEntrySlug,
  };
}

export function navSliceEquals(a: NavSlice, b: NavSlice): boolean {
  return (
    a.currentApp === b.currentApp &&
    a.currentChannel === b.currentChannel &&
    a.channelMeta === b.channelMeta &&
    a.wikiPath === b.wikiPath &&
    a.wikiLookupQuery === b.wikiLookupQuery &&
    a.notebookAgentSlug === b.notebookAgentSlug &&
    a.notebookEntrySlug === b.notebookEntrySlug
  );
}

// ── URL → store ────────────────────────────────────────────────
//
// Each route id maps to a patcher: a function that takes the matched
// route's params + search and returns a partial AppStore patch (or null
// to skip the write). The hydrator applies the patch via a single
// `useAppStore.setState` call so the bridge never observes an
// intermediate state — for example, `currentApp` set without
// `wikiLookupQuery` set, which would derive the wrong URL.

export type RoutePatch = Partial<AppStore>;

type RoutePatcher = (
  state: AppStore,
  params: Record<string, string | undefined>,
  search: Record<string, unknown>,
) => RoutePatch | null;

function patchChannel(
  _state: AppStore,
  params: Record<string, string | undefined>,
): RoutePatch {
  return {
    currentApp: null,
    currentChannel: params.channelSlug ?? "general",
    lastMessageId: null,
  };
}

function patchDm(
  state: AppStore,
  params: Record<string, string | undefined>,
): RoutePatch | null {
  const agentSlug = params.agentSlug ?? "";
  if (!agentSlug) return null;
  const channelSlug = directChannelSlug(agentSlug);
  return {
    currentApp: null,
    currentChannel: channelSlug,
    lastMessageId: null,
    channelMeta: {
      ...state.channelMeta,
      [channelSlug]: {
        ...state.channelMeta[channelSlug],
        type: "D",
        agentSlug,
      },
    },
    unreadByChannel: { ...state.unreadByChannel, [channelSlug]: 0 },
  };
}

function patchApp(
  _state: AppStore,
  params: Record<string, string | undefined>,
): RoutePatch | null {
  const appId = params.appId ?? "";
  return appId ? { currentApp: appId } : null;
}

function patchWikiIndex(): RoutePatch {
  return { currentApp: "wiki", wikiPath: null };
}

function patchWikiLookup(
  _state: AppStore,
  _params: Record<string, string | undefined>,
  search: Record<string, unknown>,
): RoutePatch {
  return {
    currentApp: "wiki-lookup",
    wikiLookupQuery: typeof search.q === "string" ? search.q : null,
  };
}

function patchWikiArticle(
  _state: AppStore,
  params: Record<string, string | undefined>,
): RoutePatch {
  const splat =
    typeof params._splat === "string" && params._splat.length > 0
      ? params._splat
      : null;
  return { currentApp: "wiki", wikiPath: splat };
}

function patchNotebooks(): RoutePatch {
  return {
    currentApp: "notebooks",
    notebookAgentSlug: null,
    notebookEntrySlug: null,
  };
}

function patchNotebookAgent(
  _state: AppStore,
  params: Record<string, string | undefined>,
): RoutePatch {
  return {
    currentApp: "notebooks",
    notebookAgentSlug: params.agentSlug ?? null,
    notebookEntrySlug: null,
  };
}

function patchNotebookEntry(
  _state: AppStore,
  params: Record<string, string | undefined>,
): RoutePatch {
  return {
    currentApp: "notebooks",
    notebookAgentSlug: params.agentSlug ?? null,
    notebookEntrySlug: params.entrySlug ?? null,
  };
}

function patchReviews(): RoutePatch {
  return { currentApp: "reviews" };
}

const ROUTE_PATCHERS: Record<string, RoutePatcher> = {
  [channelRoute.id]: patchChannel,
  [dmRoute.id]: patchDm,
  [appRoute.id]: patchApp,
  [wikiIndexRoute.id]: patchWikiIndex,
  [wikiLookupRoute.id]: patchWikiLookup,
  [wikiArticleRoute.id]: patchWikiArticle,
  [notebooksRoute.id]: patchNotebooks,
  [notebookAgentRoute.id]: patchNotebookAgent,
  [notebookEntryRoute.id]: patchNotebookEntry,
  [reviewsRoute.id]: patchReviews,
};

/**
 * Apply the matched route's params/search to the Zustand store as a single
 * atomic state update. Unknown routeIds (e.g. /console after legacy alias
 * removal, or any URL that resolves only to `__root__`) are no-ops; the
 * caller renders a not-found surface.
 */
export function applyMatchToStore(
  routeId: string,
  params: Record<string, string | undefined>,
  search: Record<string, unknown>,
): void {
  const patcher = ROUTE_PATCHERS[routeId];
  if (!patcher) return;
  useAppStore.setState((state) => {
    const patch = patcher(state, params, search);
    return patch ?? state;
  });
}

/** True for unknown URLs — the kind a not-found surface should catch. */
export function isUnmatchedRoute(routeId: string | undefined): boolean {
  return !routeId || routeId === ROOT_ROUTE_ID;
}

// ── Store → URL ────────────────────────────────────────────────
//
// Pure derivation from the navigation slice to a TanStack Router
// navigate target. Used by StoreToRouterBridge during step 2; the
// bridge itself goes away when step 3 converts call sites to typed
// navigation, but this function stays useful as the canonical
// store→URL mapping for tests.

export type NavTargetTo =
  | "/channels/$channelSlug"
  | "/dm/$agentSlug"
  | "/apps/$appId"
  | "/wiki"
  | "/wiki/lookup"
  | "/wiki/$"
  | "/notebooks"
  | "/notebooks/$agentSlug"
  | "/notebooks/$agentSlug/$entrySlug"
  | "/reviews";

export interface NavTarget {
  to: NavTargetTo;
  params?: Record<string, string>;
  search?: Record<string, string>;
}

function deriveNotebookTarget(
  agentSlug: string | null,
  entrySlug: string | null,
): NavTarget {
  if (agentSlug && entrySlug) {
    return {
      to: "/notebooks/$agentSlug/$entrySlug",
      params: { agentSlug, entrySlug },
    };
  }
  if (agentSlug) {
    return { to: "/notebooks/$agentSlug", params: { agentSlug } };
  }
  return { to: "/notebooks" };
}

function deriveChannelTarget(
  currentChannel: string,
  channelMeta: Record<string, ChannelMeta>,
): NavTarget {
  const dm = isDMChannel(currentChannel, channelMeta);
  if (dm) {
    return { to: "/dm/$agentSlug", params: { agentSlug: dm.agentSlug } };
  }
  return {
    to: "/channels/$channelSlug",
    params: { channelSlug: currentChannel || "general" },
  };
}

export function deriveNavTarget(slice: NavSlice): NavTarget {
  if (slice.currentApp === "wiki-lookup") {
    return {
      to: "/wiki/lookup",
      search: slice.wikiLookupQuery ? { q: slice.wikiLookupQuery } : {},
    };
  }
  if (slice.currentApp === "wiki") {
    return slice.wikiPath
      ? { to: "/wiki/$", params: { _splat: slice.wikiPath } }
      : { to: "/wiki" };
  }
  if (slice.currentApp === "notebooks") {
    return deriveNotebookTarget(
      slice.notebookAgentSlug,
      slice.notebookEntrySlug,
    );
  }
  if (slice.currentApp === "reviews") {
    return { to: "/reviews" };
  }
  if (slice.currentApp) {
    return { to: "/apps/$appId", params: { appId: slice.currentApp } };
  }
  return deriveChannelTarget(slice.currentChannel, slice.channelMeta);
}

export function fillPath(target: NavTarget): string {
  let path: string = target.to;
  if (target.params) {
    for (const [key, value] of Object.entries(target.params)) {
      const placeholder = key === "_splat" ? "$" : `$${key}`;
      path = path.replace(placeholder, encodeURIComponent(value));
    }
  }
  return path;
}

export function navTargetSearchString(target: NavTarget): string {
  return target.search && Object.keys(target.search).length > 0
    ? `?${new URLSearchParams(target.search).toString()}`
    : "";
}
