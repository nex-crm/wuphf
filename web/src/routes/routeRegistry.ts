import { SIDEBAR_APPS } from "../lib/constants";

export const APP_PANEL_IDS = [
  "activity",
  "calendar",
  "console",
  "graph",
  "health-check",
  "policies",
  "receipts",
  "requests",
  "settings",
  "skills",
  "tasks",
  "threads",
] as const;

export type AppPanelId = (typeof APP_PANEL_IDS)[number];

export const FIRST_CLASS_APP_IDS = ["wiki"] as const;
export type FirstClassAppId = (typeof FIRST_CLASS_APP_IDS)[number];

export const WIKI_SURFACE_APP_IDS = ["wiki", "notebooks", "reviews"] as const;

const APP_PANEL_ID_SET = new Set<string>(APP_PANEL_IDS);
const FIRST_CLASS_APP_ID_SET = new Set<string>(FIRST_CLASS_APP_IDS);

export function isAppPanelId(value: string): value is AppPanelId {
  return APP_PANEL_ID_SET.has(value);
}

export function sidebarAppRouteKind(
  id: string,
): "app-panel" | "first-class" | null {
  if (APP_PANEL_ID_SET.has(id)) return "app-panel";
  if (FIRST_CLASS_APP_ID_SET.has(id)) return "first-class";
  return null;
}

export const ROUTE_PATHS = {
  index: "/",
  channel: "/channels/$channelSlug",
  dm: "/dm/$agentSlug",
  app: "/apps/$appId",
  wiki: "/wiki",
  wikiLookup: "/wiki/lookup",
  wikiArticle: "/wiki/$",
  notebooks: "/notebooks",
  notebookAgent: "/notebooks/$agentSlug",
  notebookEntry: "/notebooks/$agentSlug/$entrySlug",
  reviews: "/reviews",
} as const;

export type RouteKey = keyof typeof ROUTE_PATHS;

export interface RouteContract {
  key: RouteKey;
  path: (typeof ROUTE_PATHS)[RouteKey];
  owns: readonly string[];
  legacyHashExamples: readonly string[];
}

export const ROUTE_CONTRACTS: readonly RouteContract[] = [
  {
    key: "index",
    path: ROUTE_PATHS.index,
    owns: ["default-route"],
    legacyHashExamples: ["#/channels/general"],
  },
  {
    key: "channel",
    path: ROUTE_PATHS.channel,
    owns: ["currentChannel"],
    legacyHashExamples: ["#/channels/general", "#/channels/launch"],
  },
  {
    key: "dm",
    path: ROUTE_PATHS.dm,
    owns: ["dmAgentSlug"],
    legacyHashExamples: ["#/dm/pm"],
  },
  {
    key: "app",
    path: ROUTE_PATHS.app,
    owns: ["currentApp"],
    legacyHashExamples: ["#/apps/tasks", "#/apps/settings"],
  },
  {
    key: "wiki",
    path: ROUTE_PATHS.wiki,
    owns: ["wikiPath"],
    legacyHashExamples: ["#/wiki"],
  },
  {
    key: "wikiLookup",
    path: ROUTE_PATHS.wikiLookup,
    owns: ["wikiLookupQuery"],
    legacyHashExamples: ["#/wiki/lookup?q=renewal"],
  },
  {
    key: "wikiArticle",
    path: ROUTE_PATHS.wikiArticle,
    owns: ["wikiPath"],
    legacyHashExamples: ["#/wiki/companies/acme"],
  },
  {
    key: "notebooks",
    path: ROUTE_PATHS.notebooks,
    owns: ["notebookAgentSlug", "notebookEntrySlug"],
    legacyHashExamples: ["#/notebooks"],
  },
  {
    key: "notebookAgent",
    path: ROUTE_PATHS.notebookAgent,
    owns: ["notebookAgentSlug"],
    legacyHashExamples: ["#/notebooks/pm"],
  },
  {
    key: "notebookEntry",
    path: ROUTE_PATHS.notebookEntry,
    owns: ["notebookAgentSlug", "notebookEntrySlug"],
    legacyHashExamples: ["#/notebooks/pm/handoff"],
  },
  {
    key: "reviews",
    path: ROUTE_PATHS.reviews,
    owns: ["currentApp"],
    legacyHashExamples: ["#/reviews"],
  },
] as const;

export const SIDEBAR_APP_IDS = SIDEBAR_APPS.map((app) => app.id);
