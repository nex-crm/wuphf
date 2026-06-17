export const APP_PANEL_IDS = [
  "activity",
  "graph",
  "health-check",
  "integrations",
  "policies",
  "requests",
  "routines",
  "settings",
  "skills",
  "workflows",
] as const;

export type AppPanelId = (typeof APP_PANEL_IDS)[number];

export const FIRST_CLASS_APP_IDS = [
  "wiki",
  "inbox",
  "tasks",
  "agents",
] as const;
export type FirstClassAppId = (typeof FIRST_CLASS_APP_IDS)[number];

export const WIKI_SURFACE_APP_IDS = ["wiki", "notebooks", "reviews"] as const;

const APP_PANEL_ID_SET = new Set<string>(APP_PANEL_IDS);
const FIRST_CLASS_APP_ID_SET = new Set<string>(FIRST_CLASS_APP_IDS);

export function isAppPanelId(value: string): value is AppPanelId {
  return APP_PANEL_ID_SET.has(value);
}

export function isFirstClassAppId(value: string): value is FirstClassAppId {
  return FIRST_CLASS_APP_ID_SET.has(value);
}

export function sidebarAppRouteKind(
  id: string,
): "app-panel" | "first-class" | null {
  if (APP_PANEL_ID_SET.has(id)) return "app-panel";
  if (FIRST_CLASS_APP_ID_SET.has(id)) return "first-class";
  return null;
}

/**
 * Human-readable labels for every routed sidebar surface. Keyed by every
 * `AppPanelId` and every `FirstClassAppId`, so the sidebar TOOLS list and
 * any other route-driven UI can resolve a label from a single source of
 * truth instead of duplicating the string in a sidebar constant.
 */
export const APP_LABELS: Record<AppPanelId | FirstClassAppId, string> = {
  // First-class surfaces (live at dedicated routes, not `/apps/$id`).
  wiki: "Wiki",
  inbox: "Inbox",
  tasks: "Tasks",
  agents: "Agents",
  // Routed app panels under `/apps/$appId`. The `activity` id keeps its
  // historical slug so existing /apps/activity URLs still resolve; the
  // human-facing label is "Dashboard" (renamed in #1002). The `calendar`
  // entry is intentionally dropped — Routines replaces it.
  activity: "Dashboard",
  graph: "Graph",
  "health-check": "Access & Health",
  integrations: "Integrations",
  policies: "Policies",
  requests: "Requests",
  routines: "Scheduled Tasks",
  settings: "Settings",
  skills: "Skills",
  workflows: "Workflows",
};

/**
 * Emoji fallback icons rendered when a `SidebarTool` has no iconoir-react
 * mapping in the rendering component. Lookups for unknown ids fall back
 * to a generic glyph at the call site.
 */
const SIDEBAR_TOOL_EMOJIS: Partial<
  Record<AppPanelId | FirstClassAppId, string>
> = {
  activity: "🏠",
  tasks: "✓",
  agents: "🤖",
  wiki: "📖",
  graph: "🕸",
  policies: "🛡",
  routines: "🔁",
  skills: "⚡",
  workflows: "🔎",
  "health-check": "📶",
  integrations: "🔌",
  settings: "⚙",
};

export interface SidebarTool {
  /** Either an `AppPanelId` or a `FirstClassAppId`. */
  id: AppPanelId | FirstClassAppId;
  /** Display label resolved from `APP_LABELS`. */
  label: string;
  /** Whether this entry routes to `/apps/$id` or to a dedicated path. */
  kind: "app-panel" | "first-class";
  /** Optional emoji glyph for callers without an icon mapping. */
  icon?: string;
}

/**
 * Ordered list of sidebar TOOLS entries. The renderer (`AppList`,
 * `CollapsedSidebar`) maps over this array directly — labels and order
 * are owned here, not duplicated in `lib/constants.ts`.
 *
 * `settings` lives at the bottom because `AppList`/`CollapsedSidebar`
 * render it separately (it's filtered out of the TOOLS section and
 * mounted in a fixed slot). It is kept in the array so the route
 * registry stays the single source of truth for which ids are valid
 * sidebar destinations.
 */
export const SIDEBAR_TOOLS: readonly SidebarTool[] = [
  { id: "activity", kind: "app-panel" },
  { id: "tasks", kind: "first-class" },
  { id: "agents", kind: "first-class" },
  { id: "wiki", kind: "first-class" },
  { id: "graph", kind: "app-panel" },
  { id: "policies", kind: "app-panel" },
  { id: "routines", kind: "app-panel" },
  { id: "skills", kind: "app-panel" },
  { id: "workflows", kind: "app-panel" },
  { id: "health-check", kind: "app-panel" },
  { id: "integrations", kind: "app-panel" },
  { id: "settings", kind: "app-panel" },
].map((entry) => ({
  ...entry,
  label: APP_LABELS[entry.id as AppPanelId | FirstClassAppId],
  icon: SIDEBAR_TOOL_EMOJIS[entry.id as AppPanelId | FirstClassAppId],
})) as readonly SidebarTool[];

export const ROUTE_PATHS = {
  index: "/",
  channel: "/channels/$channelSlug",
  app: "/apps/$appId",
  /** Task list surface (first-class; all human work-items render as Tasks). */
  tasks: "/tasks",
  /** Task detail surface (renders TaskDocument). */
  taskDetail: "/tasks/$taskId",
  /** New Task creation form. Listed before taskDetail so `new` wins. */
  taskNew: "/tasks/new",
  /** Back-compat redirect: `/apps/tasks/$taskId` → `/tasks/$taskId`. */
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
  /** Full-screen HTML article viewer. id is an ra_... artifact id. */
  article: "/articles/$articleId",
  inbox: "/inbox",
  taskDecision: "/task/$taskId",
  /** Routines surface index — alias for /apps/routines. */
  routines: "/routines",
  /** Composer for creating a routine from scratch. */
  routineNew: "/routines/new",
  /** Routine detail page (full-screen, not the legacy drawer). */
  routineDetail: "/routines/$routineSlug",
  /** Agents tool — roster grid of every agent (CEO, Librarian, specialists). */
  agents: "/agents",
  /** Agent detail/config page — provider, role, persona, skills. */
  agentDetail: "/agents/$agentSlug",
  /** Full-screen skill SKILL.md detail editor + viewer. */
  skillDetail: "/skills/$skillName",
} as const;

export type RouteKey = keyof typeof ROUTE_PATHS;

/** Surface IDs for the Tasks section (list + detail + new). */
export const TASKS_SURFACE_IDS = ["tasks", "taskDetail", "taskNew"] as const;
export type TasksSurfaceId = (typeof TASKS_SURFACE_IDS)[number];

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
  { key: "app", path: ROUTE_PATHS.app, params: ["appId"], search: [] },
  { key: "tasks", path: ROUTE_PATHS.tasks, params: [], search: [] },
  {
    key: "taskDetail",
    path: ROUTE_PATHS.taskDetail,
    params: ["taskId"],
    search: [],
  },
  { key: "taskNew", path: ROUTE_PATHS.taskNew, params: [], search: [] },
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
  {
    key: "article",
    path: ROUTE_PATHS.article,
    params: ["articleId"],
    search: [],
  },
  { key: "inbox", path: ROUTE_PATHS.inbox, params: [], search: [] },
  {
    key: "taskDecision",
    path: ROUTE_PATHS.taskDecision,
    params: ["taskId"],
    search: [],
  },
  { key: "routines", path: ROUTE_PATHS.routines, params: [], search: [] },
  { key: "routineNew", path: ROUTE_PATHS.routineNew, params: [], search: [] },
  {
    key: "routineDetail",
    path: ROUTE_PATHS.routineDetail,
    params: ["routineSlug"],
    search: [],
  },
  { key: "agents", path: ROUTE_PATHS.agents, params: [], search: [] },
  {
    key: "agentDetail",
    path: ROUTE_PATHS.agentDetail,
    params: ["agentSlug"],
    search: [],
  },
  {
    key: "skillDetail",
    path: ROUTE_PATHS.skillDetail,
    params: ["skillName"],
    search: [],
  },
] as const;

export const SIDEBAR_APP_IDS: readonly string[] = SIDEBAR_TOOLS.map(
  (tool) => tool.id,
);
