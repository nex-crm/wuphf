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
] as const;

export type AppPanelId = (typeof APP_PANEL_IDS)[number];

export const FIRST_CLASS_APP_IDS = ["wiki", "inbox", "issues"] as const;
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
  issues: "Issues",
  // Routed app panels under `/apps/$appId`. The `activity` id keeps its
  // historical slug so existing /apps/activity URLs still resolve; the
  // human-facing label is "Dashboard".
  activity: "Dashboard",
  calendar: "Calendar",
  console: "Console",
  graph: "Graph",
  "health-check": "Access & Health",
  policies: "Policies",
  receipts: "Receipts",
  requests: "Requests",
  settings: "Settings",
  skills: "Skills",
  tasks: "Tasks",
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
  issues: "#",
  tasks: "✓",
  wiki: "📖",
  console: ">",
  graph: "🕸",
  policies: "🛡",
  calendar: "📅",
  skills: "⚡",
  receipts: "🧾",
  "health-check": "📶",
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
  { id: "issues", kind: "first-class" },
  // `tasks` retired — every unit of work is an Issue or Sub-Issue now.
  // Legacy /tasks routes redirect to /issues; keep the registry entries
  // for backwards-compatible URL parsing but no sidebar slot.
  { id: "wiki", kind: "first-class" },
  { id: "console", kind: "app-panel" },
  { id: "graph", kind: "app-panel" },
  { id: "policies", kind: "app-panel" },
  { id: "calendar", kind: "app-panel" },
  { id: "skills", kind: "app-panel" },
  { id: "receipts", kind: "app-panel" },
  { id: "health-check", kind: "app-panel" },
  { id: "settings", kind: "app-panel" },
].map((entry) => ({
  ...entry,
  label: APP_LABELS[entry.id as AppPanelId | FirstClassAppId],
  icon: SIDEBAR_TOOL_EMOJIS[entry.id as AppPanelId | FirstClassAppId],
})) as readonly SidebarTool[];

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
  /** Full-screen HTML article viewer. id is an ra_... artifact id. */
  article: "/articles/$articleId",
  inbox: "/inbox",
  taskDecision: "/task/$taskId",
  /** Phase 3 — Issue list surface (all tasks rendered as Issues). */
  issues: "/issues",
  /** Phase 3 — Issue detail surface (renders IssueDocument). */
  issueDetail: "/issues/$issueId",
  /**
   * Phase 4 stub — new issue creation. Returns 501 in Phase 3.
   * Wired so `+ New issue` can navigate here without a 404.
   */
  issueNew: "/issues/new",
  /**
   * v3 MVP — per-agent subspace shell.
   * Renders the uniform Chat | App | Notebooks | Calendar | Settings tabs.
   */
  agentSubspace: "/agents/$agentSlug",
  agentSubspaceTab: "/agents/$agentSlug/$tab",
  /** Full-screen skill SKILL.md detail editor + viewer. */
  skillDetail: "/skills/$skillName",
} as const;

export type RouteKey = keyof typeof ROUTE_PATHS;

/** Phase 3 — surface IDs for the Issues section (list + detail + new). */
export const ISSUES_SURFACE_IDS = [
  "issues",
  "issueDetail",
  "issueNew",
] as const;
export type IssuesSurfaceId = (typeof ISSUES_SURFACE_IDS)[number];

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
  // Phase 3 — Issues surface
  { key: "issues", path: ROUTE_PATHS.issues, params: [], search: [] },
  {
    key: "issueDetail",
    path: ROUTE_PATHS.issueDetail,
    params: ["issueId"],
    search: [],
  },
  { key: "issueNew", path: ROUTE_PATHS.issueNew, params: [], search: [] },
  {
    key: "agentSubspace",
    path: ROUTE_PATHS.agentSubspace,
    params: ["agentSlug"],
    search: [],
  },
  {
    key: "agentSubspaceTab",
    path: ROUTE_PATHS.agentSubspaceTab,
    params: ["agentSlug", "tab"],
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
