import { SIDEBAR_APPS } from "../lib/constants";

export const APP_PANEL_IDS = [
  "activity",
  "calendar",
  "console",
  "graph",
  "health-check",
  "overview",
  "policies",
  "receipts",
  "requests",
  "settings",
  "skills",
  "tasks",
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
  tasks: "/tasks",
  taskDetail: "/tasks/$taskId",
  appTaskDetail: "/apps/tasks/$taskId",
  legacyWorkbench: "/apps/workbench",
  legacyWorkbenchAgent: "/apps/workbench/$agentSlug",
  legacyWorkbenchTask: "/apps/workbench/$agentSlug/tasks/$taskId",
  wiki: "/wiki",
  wikiLookup: "/wiki/lookup",
  wikiArticle: "/wiki/$",
  notebooks: "/notebooks",
  notebookAgent: "/notebooks/$agentSlug",
  notebookEntry: "/notebooks/$agentSlug/$entrySlug",
  reviews: "/reviews",
  inbox: "/inbox",
  taskDecision: "/task/$taskId",
} as const;

export type RouteKey = keyof typeof ROUTE_PATHS;

/**
 * Route → URL params it carries. Used to document the URL contract for
 * each route so a contributor reading the registry can see at a glance
 * what a route's URL is responsible for. Replaces the old `owns` field
 * which referenced Zustand store slots that no longer exist.
 */
export interface RouteContract {
  key: RouteKey;
  path: (typeof ROUTE_PATHS)[RouteKey];
  /** URL-derived params for this route; empty when the route has none. */
  params: readonly string[];
  /** URL-derived search params for this route; empty when none. */
  search: readonly string[];
}

export const ROUTE_CONTRACTS: readonly RouteContract[] = [
  { key: "index", path: ROUTE_PATHS.index, params: [], search: [] },
  {
    key: "channel",
    path: ROUTE_PATHS.channel,
    params: ["channelSlug"],
    search: [],
  },
  { key: "dm", path: ROUTE_PATHS.dm, params: ["agentSlug"], search: [] },
  { key: "app", path: ROUTE_PATHS.app, params: ["appId"], search: [] },
  { key: "tasks", path: ROUTE_PATHS.tasks, params: [], search: [] },
  {
    key: "taskDetail",
    path: ROUTE_PATHS.taskDetail,
    params: ["taskId"],
    search: [],
  },
  {
    key: "appTaskDetail",
    path: ROUTE_PATHS.appTaskDetail,
    params: ["taskId"],
    search: [],
  },
  {
    key: "legacyWorkbench",
    path: ROUTE_PATHS.legacyWorkbench,
    params: [],
    search: [],
  },
  {
    key: "legacyWorkbenchAgent",
    path: ROUTE_PATHS.legacyWorkbenchAgent,
    params: ["agentSlug"],
    search: [],
  },
  {
    key: "legacyWorkbenchTask",
    path: ROUTE_PATHS.legacyWorkbenchTask,
    params: ["agentSlug", "taskId"],
    search: [],
  },
  { key: "wiki", path: ROUTE_PATHS.wiki, params: [], search: [] },
  {
    key: "wikiLookup",
    path: ROUTE_PATHS.wikiLookup,
    params: [],
    search: ["q"],
  },
  {
    key: "wikiArticle",
    path: ROUTE_PATHS.wikiArticle,
    params: ["_splat"],
    search: [],
  },
  { key: "notebooks", path: ROUTE_PATHS.notebooks, params: [], search: [] },
  {
    key: "notebookAgent",
    path: ROUTE_PATHS.notebookAgent,
    params: ["agentSlug"],
    search: [],
  },
  {
    key: "notebookEntry",
    path: ROUTE_PATHS.notebookEntry,
    params: ["agentSlug", "entrySlug"],
    search: [],
  },
  { key: "reviews", path: ROUTE_PATHS.reviews, params: [], search: [] },
  { key: "inbox", path: ROUTE_PATHS.inbox, params: [], search: [] },
  {
    key: "taskDecision",
    path: ROUTE_PATHS.taskDecision,
    params: ["taskId"],
    search: [],
  },
] as const;

export const SIDEBAR_APP_IDS = SIDEBAR_APPS.map((app) => app.id);
