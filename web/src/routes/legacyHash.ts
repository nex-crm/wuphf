import {
  type ChannelMeta,
  directChannelSlug,
  isDMChannel,
} from "../stores/app";

export type LegacyRoute =
  | { view: "channel"; channel: string }
  | { view: "dm"; agent: string }
  | { view: "app"; app: string }
  | { view: "wiki"; articlePath: string | null }
  | { view: "wiki-lookup"; query: string }
  | { view: "notebooks"; agentSlug: string | null; entrySlug: string | null }
  | { view: "reviews" };

export interface LegacyRouteState {
  currentApp: string | null;
  currentChannel: string;
  channelMeta: Record<string, ChannelMeta>;
  wikiPath: string | null;
  wikiLookupQuery: string | null;
  notebookAgentSlug: string | null;
  notebookEntrySlug: string | null;
}

function searchWithoutQuestion(search: string): string {
  return search.replace(/^\?/, "");
}

function defaultRoute(): LegacyRoute {
  return { view: "channel", channel: "general" };
}

function parseWikiRoute(
  parts: string[],
  hashQuery: string,
  search: string,
): LegacyRoute {
  if (parts[1] === "lookup") {
    const params = new URLSearchParams(
      searchWithoutQuestion(search) || hashQuery,
    );
    const q = params.get("q") || "";
    return { view: "wiki-lookup", query: decodeURIComponent(q) };
  }

  const rest = parts.slice(1).map(decodeURIComponent).join("/");
  return { view: "wiki", articlePath: rest || null };
}

function parseNotebooksRoute(parts: string[]): LegacyRoute {
  const agent = parts[1] ? decodeURIComponent(parts[1]) : null;
  const entry = parts[2] ? decodeURIComponent(parts[2]) : null;
  return { view: "notebooks", agentSlug: agent, entrySlug: entry };
}

/**
 * Parse today's hash URL contract into the route shape the current Zustand
 * navigation store expects. Kept pure so Phase 0 can pin behavior before the
 * TanStack Router migration changes the source of truth.
 */
export function parseLegacyHash(hash: string, search = ""): LegacyRoute {
  const cleaned = hash.replace(/^#\/?/, "");
  const [pathPart, hashQuery = ""] = cleaned.split("?");
  const parts = pathPart.split("/").filter(Boolean);

  switch (parts[0]) {
    case "channels":
      return parts[1]
        ? { view: "channel", channel: decodeURIComponent(parts[1]) }
        : defaultRoute();
    case "dm":
      return parts[1]
        ? { view: "dm", agent: decodeURIComponent(parts[1]) }
        : defaultRoute();
    case "apps":
      return parts[1]
        ? { view: "app", app: decodeURIComponent(parts[1]) }
        : defaultRoute();
    case "console":
      return { view: "app", app: "console" };
    case "threads":
      return { view: "app", app: "threads" };
    case "wiki":
      return parseWikiRoute(parts, hashQuery, search);
    case "notebooks":
      return parseNotebooksRoute(parts);
    case "reviews":
      return { view: "reviews" };
    default:
      return defaultRoute();
  }
}

export function legacyRouteToStatePatch(route: LegacyRoute):
  | {
      kind: "dm";
      agentSlug: string;
      channelSlug: string;
    }
  | {
      kind: "app";
      app: string;
    }
  | {
      kind: "wiki-lookup";
      query: string;
    }
  | {
      kind: "wiki";
      articlePath: string | null;
    }
  | {
      kind: "notebooks";
      agentSlug: string | null;
      entrySlug: string | null;
    }
  | {
      kind: "reviews";
    }
  | {
      kind: "channel";
      channel: string;
    } {
  if (route.view === "dm") {
    return {
      kind: "dm",
      agentSlug: route.agent,
      channelSlug: directChannelSlug(route.agent),
    };
  }
  if (route.view === "app") {
    return { kind: "app", app: route.app };
  }
  if (route.view === "wiki-lookup") {
    return { kind: "wiki-lookup", query: route.query };
  }
  if (route.view === "wiki") {
    return { kind: "wiki", articlePath: route.articlePath };
  }
  if (route.view === "notebooks") {
    return {
      kind: "notebooks",
      agentSlug: route.agentSlug,
      entrySlug: route.entrySlug,
    };
  }
  if (route.view === "reviews") {
    return { kind: "reviews" };
  }
  return { kind: "channel", channel: route.channel };
}

/**
 * Serialize today's Zustand route state back into the current hash URL
 * contract. This is intentionally behavior-preserving until TanStack Router
 * becomes the source of truth.
 */
export function legacyStateToHash(state: LegacyRouteState): string {
  switch (state.currentApp) {
    case "wiki-lookup":
      return wikiLookupStateToHash(state);
    case "wiki":
      return wikiStateToHash(state);
    case "notebooks":
      return notebookStateToHash(state);
    case "reviews":
      return "#/reviews";
    case null:
      return channelStateToHash(state);
    default:
      return `#/apps/${encodeURIComponent(state.currentApp)}`;
  }
}

function wikiLookupStateToHash(state: LegacyRouteState): string {
  return state.wikiLookupQuery
    ? `#/wiki/lookup?q=${encodeURIComponent(state.wikiLookupQuery)}`
    : "#/wiki/lookup";
}

function wikiStateToHash(state: LegacyRouteState): string {
  return state.wikiPath
    ? `#/wiki/${state.wikiPath.split("/").map(encodeURIComponent).join("/")}`
    : "#/wiki";
}

function notebookStateToHash(state: LegacyRouteState): string {
  const parts: string[] = ["notebooks"];
  if (state.notebookAgentSlug) {
    parts.push(encodeURIComponent(state.notebookAgentSlug));
  }
  if (state.notebookAgentSlug && state.notebookEntrySlug) {
    parts.push(encodeURIComponent(state.notebookEntrySlug));
  }
  return `#/${parts.join("/")}`;
}

function channelStateToHash(state: LegacyRouteState): string {
  const dm = isDMChannel(state.currentChannel, state.channelMeta);
  if (dm) {
    return `#/dm/${encodeURIComponent(dm.agentSlug)}`;
  }
  return `#/channels/${encodeURIComponent(state.currentChannel || "general")}`;
}
