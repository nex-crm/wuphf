import {
  Component,
  type ComponentType,
  lazy,
  type ReactNode,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  Link,
  Outlet,
  useMatches,
  useRouterState,
} from "@tanstack/react-router";

import {
  ApiError,
  get,
  getInjectedAnalyticsConfig,
  initApi,
} from "../api/client";
import { TelegramConnectHost } from "../components/integrations/TelegramConnectModal";
import { Shell } from "../components/layout/Shell";
import { UpgradeBanner } from "../components/layout/UpgradeBanner";
import { ChannelParticipants } from "../components/messages/ChannelParticipants";
import { Composer } from "../components/messages/Composer";
import { MessageFeed } from "../components/messages/MessageFeed";
import { PrePickScreen } from "../components/onboarding/PrePickScreen";
import { OfficeTour } from "../components/onboarding/tour/OfficeTour";
import { useOfficeTour } from "../components/onboarding/tour/useOfficeTour";
import { OnboardingWizard } from "../components/onboarding/wizard/OnboardingWizard";
import { ConfirmHost } from "../components/ui/ConfirmDialog";
import { ProviderSwitcherHost } from "../components/ui/ProviderSwitcher";
import { ToastContainer } from "../components/ui/Toast";
import type { WikiTab } from "../components/wiki/WikiTabs";
import WikiTabs from "../components/wiki/WikiTabs";
import { useBrokerEvents } from "../hooks/useBrokerEvents";
import { useKeyboardShortcuts } from "../hooks/useKeyboardShortcuts";
import { useOfficeTasks } from "../hooks/useOfficeTasks";
import {
  configureAnalytics,
  identifyWorkspace,
  track,
  trackPageview,
} from "../lib/analytics";
import { rootRoute, router } from "../lib/router";
import { getTheme } from "../lib/themes";
import { directChannelSlug, useAppStore } from "../stores/app";
import {
  type AppPanelId,
  type FirstClassAppId,
  isAppPanelId,
  isFirstClassAppId,
} from "./routeRegistry";
import {
  type CurrentRoute,
  useChannelSlug,
  useCurrentRoute,
} from "./useCurrentRoute";

// Sentinel routeId for the root match — TanStack Router exposes this as
// `__root__`. Imported via `rootRoute.id` so a future TanStack rename
// surfaces as a single broken reference instead of a silent string-match
// drift. Used only by isUnmatchedRoute below.
const ROOT_ROUTE_ID = rootRoute.id;

/** True for unknown URLs — the kind a not-found surface should catch. */
function isUnmatchedRoute(routeId: string | undefined): boolean {
  return !routeId || routeId === ROOT_ROUTE_ID;
}

// ── Lazy-loaded panels ─────────────────────────────────────────
//
// Step 5 of the route migration plan. Each app surface is a route-only
// component, so it's safe to defer loading until the user actually
// navigates to that route. App panels carry the bulk of the bundle
// (three.js in Graph, the markdown stack in Wiki and Notebook);
// deferring them keeps the conversation surface — which is what every
// user lands on — small and fast.
//
// The named-export panels need a `.then(m => ({ default: m.X }))` shim
// because React.lazy's contract requires a module with a default
// export. Default-exported components (Wiki, Notebook, etc.) load
// directly.

const ArtifactsApp = lazy(() =>
  import("../components/apps/ArtifactsApp").then((m) => ({
    default: m.ArtifactsApp,
  })),
);
const RoutinesApp = lazy(() =>
  import("../components/apps/RoutinesApp").then((m) => ({
    default: m.RoutinesApp,
  })),
);
const RoutineDetailRoute = lazy(() =>
  import("../components/apps/routines/RoutineDetailRoute").then((m) => ({
    default: m.RoutineDetailRoute,
  })),
);
const RoutineComposer = lazy(() =>
  import("../components/apps/routines/RoutineComposer").then((m) => ({
    default: m.RoutineComposer,
  })),
);
const GraphApp = lazy(() => import("../components/apps/GraphApp"));
const HealthCheckApp = lazy(() =>
  import("../components/apps/HealthCheckApp").then((m) => ({
    default: m.HealthCheckApp,
  })),
);
const PoliciesApp = lazy(() =>
  import("../components/apps/PoliciesApp").then((m) => ({
    default: m.PoliciesApp,
  })),
);
const SettingsApp = lazy(() =>
  import("../components/apps/SettingsApp").then((m) => ({
    default: m.SettingsApp,
  })),
);
const IntegrationsApp = lazy(() =>
  import("../components/apps/IntegrationsApp").then((m) => ({
    default: m.IntegrationsApp,
  })),
);
const WorkflowsApp = lazy(() => import("../components/apps/WorkflowsApp"));
const SkillsApp = lazy(() =>
  import("../components/apps/SkillsApp").then((m) => ({
    default: m.SkillsApp,
  })),
);
const Notebook = lazy(() => import("../components/notebook/Notebook"));
const DecisionPacketRoute = lazy(() =>
  import("../components/lifecycle/DecisionPacketRoute").then((m) => ({
    default: m.DecisionPacketRoute,
  })),
);
const CitedAnswer = lazy(() => import("../components/wiki/CitedAnswer"));
const Wiki = lazy(() => import("../components/wiki/Wiki"));
const ArticleView = lazy(() =>
  import("../components/rich-artifacts/ArticleView").then((m) => ({
    default: m.ArticleView,
  })),
);
const ReviewQueueKanban = lazy(
  () => import("../components/review/ReviewQueueKanban"),
);
// Tasks surface (list + detail + new).
const TasksList = lazy(() =>
  import("../components/lifecycle/TasksList").then((m) => ({
    default: m.TasksList,
  })),
);
const TaskDocumentRoute = lazy(() =>
  import("../components/lifecycle/TaskDocumentRoute").then((m) => ({
    default: m.TaskDocumentRoute,
  })),
);
const TaskNewForm = lazy(() =>
  import("../components/lifecycle/TaskNewForm").then((m) => ({
    default: m.TaskNewForm,
  })),
);
// New-task home composer — the app's landing surface (index route).
const TaskComposer = lazy(() =>
  import("../components/tasks/TaskComposer").then((m) => ({
    default: m.TaskComposer,
  })),
);
// Agents tool — roster grid (/agents) + per-agent config (/agents/$slug).
const AgentsTool = lazy(() =>
  import("../components/agents/AgentsTool").then((m) => ({
    default: m.AgentsTool,
  })),
);
const AgentDetail = lazy(() =>
  import("../components/agents/AgentsTool").then((m) => ({
    default: m.AgentDetail,
  })),
);
// Full-screen skill SKILL.md editor + preview.
const SkillDetailRoute = lazy(() =>
  import("./SkillDetailRoute").then((m) => ({
    default: m.SkillDetailRoute,
  })),
);

function LazyPanelFallback() {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        flex: 1,
        color: "var(--text-tertiary)",
        fontSize: 13,
      }}
    >
      Loading…
    </div>
  );
}

// ── Error boundary ─────────────────────────────────────────────

interface ErrorBoundaryState {
  error: Error | null;
}

class ErrorBoundary extends Component<
  { children: ReactNode },
  ErrorBoundaryState
> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: { componentStack?: string | null }) {
    // eslint-disable-next-line no-console
    console.error("[WUPHF ErrorBoundary]", error, info);

    // Auto-recover from stale lazy-chunk hashes after a FE rebuild.
    // The browser holds an old index.html that points at deleted hashed
    // bundles; the only correct fix is to refetch index.html.
    const message = String(error?.message ?? "");
    const isChunkError =
      /Failed to fetch dynamically imported module/i.test(message) ||
      /Importing a module script failed/i.test(message) ||
      error?.name === "ChunkLoadError";
    // Record the failure shape (class name + chunk flag) — never the raw
    // message, which can contain content.
    track("app_error", {
      boundary: "root",
      error_name: error?.name || "Error",
      is_chunk_error: isChunkError,
    });
    if (isChunkError && typeof window !== "undefined") {
      const key = "wuphf:chunk-reload-attempted";
      if (!window.sessionStorage.getItem(key)) {
        window.sessionStorage.setItem(key, String(Date.now()));
        window.location.reload();
      }
    }
  }

  render() {
    if (this.state.error) {
      return (
        <div
          data-testid="error-boundary"
          style={{
            position: "fixed",
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            background: "#fee",
            color: "#900",
            padding: 20,
            fontFamily: "-apple-system, BlinkMacSystemFont, sans-serif",
            fontSize: 13,
            overflowY: "auto",
            zIndex: 9999,
          }}
        >
          <h2 style={{ margin: "0 0 8px 0", fontSize: 14 }}>
            Something broke in the UI
          </h2>
          <pre
            style={{
              margin: "8px 0 0",
              fontFamily: "SFMono-Regular, Menlo, monospace",
              fontSize: 11,
              whiteSpace: "pre-wrap",
            }}
          >
            {this.state.error.message}
            {"\n\n"}
            {this.state.error.stack}
          </pre>
          <button
            type="button"
            onClick={() => this.setState({ error: null })}
            style={{
              marginTop: 12,
              padding: "6px 12px",
              fontSize: 12,
              cursor: "pointer",
            }}
          >
            Try again
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}

// ── MainContent ─────────────────────────────────────────────────
//
// Reads the current TanStack route and renders the matching panel. The URL is
// the single navigation source; route-owned Zustand fields were removed in
// step 4 of the migration.

function navigateWikiTab(tab: WikiTab): void {
  if (tab === "wiki") {
    void router.navigate({ to: "/wiki" });
  } else if (tab === "notebooks") {
    void router.navigate({ to: "/notebooks" });
  } else {
    void router.navigate({ to: "/reviews" });
  }
}

function navigateWikiArticle(path: string | null): void {
  if (path) {
    void router.navigate({ to: "/wiki/$", params: { _splat: path } });
  } else {
    void router.navigate({ to: "/wiki" });
  }
}

function navigateNotebookCatalog(): void {
  void router.navigate({ to: "/notebooks" });
}

function navigateNotebookAgent(slug: string): void {
  void router.navigate({
    to: "/notebooks/$agentSlug",
    params: { agentSlug: slug },
  });
}

function navigateNotebookEntry(
  agentSlug: string,
  entrySlug: string | null,
): void {
  if (!entrySlug) {
    void router.navigate({
      to: "/notebooks/$agentSlug",
      params: { agentSlug },
    });
    return;
  }
  void router.navigate({
    to: "/notebooks/$agentSlug/$entrySlug",
    params: { agentSlug, entrySlug },
  });
}

const APP_PANELS = {
  requests: TasksRedirect,
  graph: GraphApp,
  policies: PoliciesApp,
  routines: RoutinesApp,
  skills: SkillsApp,
  workflows: WorkflowsApp,
  activity: ArtifactsApp,
  "health-check": HealthCheckApp,
  integrations: IntegrationsApp,
  settings: SettingsApp,
} satisfies Record<AppPanelId, ComponentType>;

function ConversationView() {
  const channelSlug = useChannelSlug() ?? "general";

  return (
    <div className="conversation-shell">
      <div className="conversation-chat">
        <MessageFeed />
        <Composer />
      </div>
      <ChannelParticipants channelSlug={channelSlug} />
    </div>
  );
}

/**
 * In the task-scoped model a business task's channel is reached through the
 * task, not as a parallel chat surface. A direct `/channels/$slug` visit
 * (typed URL, search hit, command palette) for a channel owned by a business
 * task redirects to that task's detail, closing the dual-surface gap.
 *
 * System-managed channels are deliberately NOT redirected: #general (owned by
 * the archived Backup & Migration task) and folded legacy channels stay
 * directly readable via the conversation view, because redirecting the
 * coordination channel to an archived system task is worse than the dual
 * surface. A channel with no owning task also falls through to the view, so a
 * channel with history is never dead-ended.
 */
function ChannelRedirect({ channelSlug }: { channelSlug: string }) {
  const { data: tasks, isPending } = useOfficeTasks();
  const owningTaskId = useMemo(() => {
    const slug = channelSlug.trim().toLowerCase();
    const owner = (tasks ?? []).find(
      (t) => (t.channel ?? "").trim().toLowerCase() === slug,
    );
    return owner && !owner.system ? owner.id : undefined;
  }, [tasks, channelSlug]);

  useEffect(() => {
    if (owningTaskId) {
      void router.navigate({
        to: "/tasks/$taskId",
        params: { taskId: owningTaskId },
        replace: true,
      });
    }
  }, [owningTaskId]);

  // Redirecting (owning task found) or still resolving the task list → render
  // nothing so the conversation surface never flashes. Resolved with no owning
  // task → fall back to the conversation view so the channel stays reachable.
  if (owningTaskId || isPending) return null;
  return <ConversationView />;
}

interface WikiSurfaceProps {
  current: "wiki" | "notebooks" | "reviews";
  route: CurrentRoute;
}

/**
 * When a wiki splat path collides with a sibling Wiki-surface tab
 * (`/wiki/notebooks`, `/wiki/reviews`), redirect to the canonical
 * top-level route instead of trying to load a non-existent article.
 * `/wiki/notebooks` used to leave the right pane stuck on
 * "Loading article…" because fetchArticle would 404 across all
 * candidate paths and the loader never reconciled — see issue #935.
 */
function wikiTabRedirectTarget(
  articlePath: string | null,
): "/notebooks" | "/reviews" | null {
  if (articlePath === "notebooks" || articlePath === "notebooks/") {
    return "/notebooks";
  }
  if (articlePath === "reviews" || articlePath === "reviews/") {
    return "/reviews";
  }
  return null;
}

function WikiSurface({ current, route }: WikiSurfaceProps) {
  // Pam's onActionDone bumps this; Wiki re-fetches article + history when
  // the prop changes. Lifted to the surface so Pam (in the tab bar) can
  // tell Wiki to re-render without remounting it.
  const [articleRefreshNonce, setArticleRefreshNonce] = useState(0);

  const articlePath = route.kind === "wiki-article" ? route.articlePath : null;
  const tabRedirect = wikiTabRedirectTarget(articlePath);
  useEffect(() => {
    if (!tabRedirect) return;
    void router.navigate({ to: tabRedirect, replace: true });
  }, [tabRedirect]);
  if (tabRedirect) {
    return (
      <div className="wiki-shell" data-testid="wiki-tab-redirect">
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            flex: 1,
            color: "var(--text-tertiary)",
            fontSize: 14,
          }}
        >
          Redirecting…
        </div>
      </div>
    );
  }
  const pamArticlePath = current === "wiki" ? articlePath : null;
  const notebookAgentSlug =
    route.kind === "notebook-agent" || route.kind === "notebook-entry"
      ? route.agentSlug
      : null;
  const notebookEntrySlug =
    route.kind === "notebook-entry" ? route.entrySlug : null;

  return (
    <div className="wiki-shell">
      <WikiTabs
        current={current}
        onSelect={navigateWikiTab}
        pamArticlePath={pamArticlePath}
        onPamActionDone={() => setArticleRefreshNonce((n) => n + 1)}
      />
      <div className="wiki-shell-body">
        {current === "wiki" && (
          <Wiki
            articlePath={articlePath}
            externalRefreshNonce={articleRefreshNonce}
            onNavigate={navigateWikiArticle}
          />
        )}
        {current === "notebooks" && (
          <Notebook
            agentSlug={notebookAgentSlug}
            entrySlug={notebookEntrySlug}
            onOpenCatalog={navigateNotebookCatalog}
            onOpenAgent={navigateNotebookAgent}
            onOpenEntry={navigateNotebookEntry}
            onNavigateWiki={(path) => navigateWikiArticle(path || null)}
          />
        )}
        {current === "reviews" && (
          <ReviewQueueKanban onOpenEntry={navigateNotebookEntry} />
        )}
      </div>
    </div>
  );
}

/**
 * TasksRedirect navigates the user to the Task board. It backs both the
 * deprecated `/apps/requests` panel and the retired `/inbox` route: the
 * standalone Inbox was consolidated into the board (its attention items
 * live in the "Needs human input" lane), so every old entry point lands
 * on `/tasks`. Keeps existing `/apps/requests` and `/inbox` bookmarks
 * working without resurrecting the surface.
 */
function TasksRedirect() {
  useEffect(() => {
    void router.navigate({ to: "/tasks", replace: true });
  }, []);
  return (
    <div className="app-panel active" data-testid="legacy-redirect-inbox">
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flex: 1,
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Redirecting to Tasks…
      </div>
    </div>
  );
}

/**
 * FirstClassAppRedirect navigates the user from a `/apps/$id` URL whose
 * `$id` is a first-class app (wiki, inbox, tasks, agents) to that app's
 * canonical dedicated route. Users who type a sidebar-label-style URL by
 * hand (e.g. `/#/apps/wiki`) used to hit "Page not found" because
 * first-class apps live at their own paths, not under `/apps`. Mirrors
 * `InboxRedirect` so the route ↔ sidebar mapping is forgiving without
 * the route registry sprouting alias entries.
 */
const FIRST_CLASS_APP_TARGETS: Record<
  FirstClassAppId,
  "/wiki" | "/tasks" | "/agents"
> = {
  wiki: "/wiki",
  // The Inbox was consolidated into the board — send `/apps/inbox` straight
  // to /tasks rather than double-hopping through the retired /inbox route.
  inbox: "/tasks",
  tasks: "/tasks",
  agents: "/agents",
};

function FirstClassAppRedirect({ appId }: { appId: FirstClassAppId }) {
  useEffect(() => {
    void router.navigate({ to: FIRST_CLASS_APP_TARGETS[appId], replace: true });
  }, [appId]);
  return (
    <div
      className="app-panel active"
      data-testid={`legacy-redirect-first-class-${appId}`}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flex: 1,
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Redirecting…
      </div>
    </div>
  );
}

function UnknownAppPanel({ appId }: { appId: string }) {
  return (
    <div className="app-panel active" data-testid={`app-page-${appId}`}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flex: 1,
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Unknown app: {appId}
      </div>
    </div>
  );
}

function AppPanel({ appId }: { appId: AppPanelId }) {
  const Panel = APP_PANELS[appId];
  return (
    <div className="app-panel active" data-testid={`app-page-${appId}`}>
      <Panel />
    </div>
  );
}

/**
 * On every conversation-route mount: (1) record the channel as the
 * `lastConversationalChannel` fallback so off-conversation surfaces
 * (RequestsApp, sidebar request badge) keep pointing at
 * the user's working channel; (2) clear the unread badge for that
 * channel — the legacy `setCurrentChannel` / `enterDM` callers used to
 * zero `unreadByChannel[ch]` as part of the same atomic write, and
 * removing them without porting the clear left badges sticky until the
 * next inbound message happened to land. The store guards against
 * redundant writes, so the effect is cheap to re-run.
 *
 * The deps narrow on the slug values rather than the wide `route`
 * object identity to avoid re-firing for unrelated route field
 * changes (e.g. wiki splat updates) — the effect only cares about
 * channel/dm transitions, and re-clearing a 0-count unread is a no-op
 * but pointless work.
 */
function useTrackLastConversationalChannel(route: CurrentRoute): void {
  const setLastConversationalChannel = useAppStore(
    (s) => s.setLastConversationalChannel,
  );
  const clearUnread = useAppStore((s) => s.clearUnread);
  const channelSlug = route.kind === "channel" ? route.channelSlug : null;
  useEffect(() => {
    if (channelSlug) {
      setLastConversationalChannel(channelSlug);
      clearUnread(channelSlug);
    }
  }, [channelSlug, setLastConversationalChannel, clearUnread]);
}

function MainContent() {
  const route = useCurrentRoute();
  // Reactive pathname read so the unknown-variant branch below renders
  // the same shape RoutedBody does (`/foo`, no leading `#`) — see LOW 6
  // in PR #634 round-4 review. Reading window.location.hash directly
  // would mix prefixes across the two not-found code paths.
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  useTrackLastConversationalChannel(route);

  switch (route.kind) {
    case "home":
      return <TaskComposer />;
    case "channel":
      return <ChannelRedirect channelSlug={route.channelSlug} />;
    case "app":
      // `/apps/wiki` and `/apps/inbox` are not app-panel routes — the
      // sidebar navigates to `/wiki` and `/inbox` directly — but users
      // who type a sidebar-label-style URL by hand should not hit
      // "Page not found". Forward them to the canonical first-class
      // route instead.
      if (isFirstClassAppId(route.appId)) {
        return <FirstClassAppRedirect appId={route.appId} />;
      }
      if (!isAppPanelId(route.appId)) {
        return <UnknownAppPanel appId={route.appId} />;
      }
      return <AppPanel appId={route.appId} />;
    case "task-board":
      return <TasksList />;
    case "task-detail":
      return <TaskDocumentRoute taskId={route.taskId} />;
    case "task-new":
      return <TaskNewForm />;
    case "wiki":
    case "wiki-article":
      return <WikiSurface current="wiki" route={route} />;
    case "wiki-lookup":
      return (
        <div className="wiki-shell">
          <WikiTabs current="wiki" onSelect={navigateWikiTab} />
          <div className="wiki-shell-body">
            <CitedAnswer query={route.query || ""} />
          </div>
        </div>
      );
    case "notebook-catalog":
    case "notebook-agent":
    case "notebook-entry":
      return <WikiSurface current="notebooks" route={route} />;
    case "reviews":
      return <WikiSurface current="reviews" route={route} />;
    case "article":
      return <ArticleView articleId={route.articleId} />;
    case "inbox":
      // The standalone Inbox was consolidated into the Task board; `/inbox`
      // is kept only as a redirect for old bookmarks.
      return <TasksRedirect />;
    case "task-decision":
      return <DecisionPacketRoute taskId={route.taskId} />;
    case "agents":
      return <AgentsTool />;
    case "agent-detail":
      return <AgentDetail agentSlug={route.agentSlug} tab={route.tab} />;
    case "skill-detail":
      return <SkillDetailRoute skillName={route.skillName} />;
    case "routine-detail":
      return (
        <div className="app-panel active" data-testid="app-page-routines">
          <RoutineDetailRoute routineSlug={route.routineSlug} />
        </div>
      );
    case "routine-new":
      return (
        <div className="app-panel active" data-testid="app-page-routines">
          <RoutineComposer />
        </div>
      );
    case "unknown":
      // RoutedBody catches root-only matches via isUnmatchedRoute, but
      // useCurrentRoute can also return `unknown` for matched leaves that
      // aren't wired into CURRENT_ROUTE_IDS (e.g. a newly-added route a
      // contributor forgot to register). Render the same not-found surface
      // here so the shell doesn't go blank in that case.
      return <NotFoundSurface pathname={pathname} />;
    default: {
      // Exhaustiveness check: if a new CurrentRoute kind is added
      // without a case here, TypeScript flags this assignment as the
      // wider `never` not being assignable, forcing the dispatch to be
      // updated alongside the union.
      const _exhaustive: never = route;
      void _exhaustive;
      return null;
    }
  }
}

// ── Not-found surface ──────────────────────────────────────────
//
// /console, /threads, and any URL that resolves to root-only (no leaf
// match) get this screen instead of stale MainContent. The store is left
// untouched — the user might back-navigate to a real route, and we don't
// want to lose their last-good navigation slice.

function NotFoundSurface({ pathname }: { pathname: string }) {
  return (
    <div
      data-testid="route-not-found"
      style={{
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        flex: 1,
        gap: 8,
        padding: 32,
        color: "var(--text-tertiary)",
        fontSize: 14,
      }}
    >
      <strong style={{ fontSize: 16, color: "var(--text-secondary)" }}>
        Page not found
      </strong>
      <span>
        No route matches <code>{pathname}</code>.
      </span>
      <Link
        to="/channels/$channelSlug"
        params={{ channelSlug: "general" }}
        style={{ color: "var(--text-secondary)" }}
      >
        Go to #general
      </Link>
    </div>
  );
}

// ── Broker-unreachable fallback ────────────────────────────────────────
//
// During a broker wedge (503s / timeouts on the bootstrap queries) the app
// used to render a blank white body. Classify bootstrap failures: a broker
// that RESPONDED with a client error (e.g. 404 — onboarding not mounted on
// a fresh install) falls through to the onboarding gate as before; a 5xx,
// network failure, or timeout means the broker is unreachable/wedged and
// gets this honest full-page state instead of a blank body.

/** Exported for the bootstrap-fallback regression test. */
export function isBrokerUnreachableError(err: unknown): boolean {
  if (err instanceof ApiError) {
    return err.status >= 500;
  }
  // Anything non-HTTP (fetch TypeError, AbortSignal timeout surfaced as
  // "Broker not responding — request timed out.") means we never got an
  // answer from the broker.
  return true;
}

const BOOT_RETRY_MS = 5_000;

function BrokerUnreachableScreen({ onRetry }: { onRetry: () => void }) {
  return (
    <div
      data-testid="broker-unreachable"
      role="alert"
      style={{
        height: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: 12,
        padding: 32,
        textAlign: "center",
        color: "var(--text-secondary)",
        fontSize: 14,
      }}
    >
      <strong style={{ fontSize: 16, color: "var(--text)" }}>
        WUPHF can&rsquo;t reach the office broker — retrying…
      </strong>
      <span style={{ color: "var(--text-tertiary)" }}>
        The broker isn&rsquo;t answering. We retry automatically every few
        seconds; you can also retry now.
      </span>
      <button
        type="button"
        className="btn btn-primary"
        onClick={onRetry}
        data-testid="broker-unreachable-retry"
      >
        Retry now
      </button>
    </div>
  );
}

function RoutedBody() {
  const matches = useMatches();
  const leaf = matches.at(-1);
  // useRouterState makes the pathname read reactive: a future refactor
  // that decouples useMatches from URL changes won't silently leave
  // NotFoundSurface showing a stale path.
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  if (isUnmatchedRoute(leaf?.routeId)) {
    return <NotFoundSurface pathname={pathname} />;
  }
  return (
    <Suspense fallback={<LazyPanelFallback />}>
      <MainContent />
    </Suspense>
  );
}

// ── Root route component ────────────────────────────────────────
//
// Owns the boot lifecycle (api-init, onboarding gate, theme css), the app
// shell, and the global host components. Rendered as the root of the
// TanStack route tree; matched leaf routes mount via <Outlet />, but their
// components are intentionally absent. RoutedBody reads the matched route and
// renders the concrete surface, keeping route state centralized here.
//
// Critical rules (violations caused the blank-page regression):
// 1. ALL hooks are called unconditionally at the top. No early returns
//    before hook calls.
// 2. initApi() runs in an effect, but we render the shell immediately so
//    the user sees something even while init is pending.
// 3. ErrorBoundary wraps the whole tree so render errors are visible.

export default function RootRoute() {
  const [apiReady, setApiReady] = useState(false);
  const theme = useAppStore((s) => s.theme);
  const onboardingComplete = useAppStore((s) => s.onboardingComplete);
  const setBrokerConnected = useAppStore((s) => s.setBrokerConnected);
  const setOnboardingComplete = useAppStore((s) => s.setOnboardingComplete);

  // After PrePickScreen, track whether the backend CEO phase machine is
  // running. When true, Shell renders OnboardingDMRoute instead of RoutedBody.
  // Flips false when the onboarding state returns onboarded=true.
  //
  // Flow: PrePickScreen → inCeoOnboarding=true → Shell+OnboardingDMRoute
  //       → broker sets phase="bridge" done → onboarded=true → normal Shell
  const [inCeoOnboarding, setInCeoOnboarding] = useState(false);
  // onboarding phase from /onboarding/state — set once on boot if available.
  const [bootPhase, setBootPhase] = useState<string | undefined>(undefined);
  // Broker-unreachable bootstrap failure (5xx / network / timeout). While
  // set, the shell renders BrokerUnreachableScreen — never a blank body.
  // bootAttempt re-runs the bootstrap effect (Retry button + auto-retry).
  const [bootError, setBootError] = useState(false);
  const [bootAttempt, setBootAttempt] = useState(0);

  // Manual SPA pageviews (autocapture is off). We subscribe to the router
  // singleton rather than useRouterState so this works even where RootRoute is
  // rendered without a RouterProvider (the bootstrap-fallback tests). Fires the
  // matched route pattern plus pathname on every navigation; a no-op until
  // analytics is configured.
  useEffect(() => {
    const emit = () => {
      const loc = router.state.location;
      const { matches } = router.state;
      const routeId = matches[matches.length - 1]?.routeId ?? "";
      if (loc?.pathname) trackPageview(routeId || loc.pathname, loc.pathname);
    };
    emit();
    return router.subscribe("onResolved", emit);
  }, []);

  // Group events by workspace, keyed by a hashed id (never the raw id or name)
  // so cohort analysis is possible without identifying the workspace. Gated on
  // apiReady so it only runs after a successful boot — that keeps it out of the
  // bootstrap-fallback path (and its mocked get() sequence) entirely. Reads via
  // get() (not a hook) so it needs no provider context. No-op until analytics
  // is configured.
  useEffect(() => {
    if (!apiReady) return;
    let cancelled = false;
    void get<{
      workspace_id?: string;
      blueprint?: string;
      company_size?: string;
    }>("/config")
      .then((cfg) => {
        if (!cancelled && cfg?.workspace_id) {
          identifyWorkspace(cfg.workspace_id, {
            blueprint: cfg.blueprint,
            company_size: cfg.company_size,
          });
        }
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [apiReady]);

  // When CEO onboarding is active (phase set, not "complete"), pin a
  // generic landing URL (#general) to the home composer (index `/`) so the
  // URL is sensible the moment the broker flips onboarded=true and the
  // office Shell mounts. The new-task composer is the app's landing surface,
  // so completing onboarding drops the user there. The onboarding
  // conversation itself renders full-screen via OnboardingChat regardless of
  // the URL, so this only governs where the user lands once onboarding
  // completes. Already-onboarded users (onboardingComplete === true) skip the
  // whole inCeoOnboarding branch and never hit this redirect.
  useEffect(() => {
    if (!(inCeoOnboarding || bootPhase)) return;
    // Only redirect when the user is on a non-home generic destination
    // (#general). Root (`#/`, `""`) already renders the composer, and
    // explicit deep-links (e.g. a specific /tasks/$id) should be preserved.
    const hash = typeof window !== "undefined" ? window.location.hash : "";
    const onGenericRoute =
      hash === "#/channels/general" || hash.startsWith("#/?");
    if (onGenericRoute) {
      void router.navigate({ to: "/", replace: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inCeoOnboarding, bootPhase]);

  useKeyboardShortcuts();
  useBrokerEvents(apiReady);

  // Guided office tour. The first-run auto-open is now OWNED BY THE VISUAL
  // ONBOARDING WIZARD (the four education slides moved into the pre-office
  // wizard steps), so we pass `enabled={false}` to suppress the post-office
  // auto-open. The replay path is independent of `enabled` in useOfficeTour:
  // the `requestShowOfficeTour()` window-event listener stays bound, so Help →
  // "Replay the office tour" still overlays the tour on the live office.
  const officeTour = useOfficeTour(false);

  // Finish handoff (spec section 4): drop the user mid-action in the CEO DM
  // with an example first issue already typed into the composer, instead of
  // dead-ending on "Done". We seed a one-shot, channel-scoped draft in the
  // app store; the Composer consumes and clears it on mount/when its channel
  // matches (see stores/app.ts pendingComposerDraft + Composer.tsx). This is
  // the controlled-state-safe alternative to writing the textarea imperatively.
  const handleTourFinish = useCallback(() => {
    const ceoChannel = directChannelSlug("ceo");
    useAppStore
      .getState()
      .setPendingComposerDraft(
        ceoChannel,
        "Audit our CRM for duplicate accounts, deals missing an owner, and opportunities with no activity in 30 days, then propose a cleanup plan",
      );
    // The legacy `/dm/$agentSlug` route was removed in the task-scoped
    // restructure (DMs fold into task channels). It was only sugar over
    // `/channels/<directChannelSlug(agentSlug)>`, so navigate to the same
    // destination directly — the channel the draft was just seeded into.
    void router.navigate({
      to: "/channels/$channelSlug",
      params: { channelSlug: ceoChannel },
    });
    // Deps intentionally empty: router and directChannelSlug are module-level
    // imports and useAppStore.getState() is Zustand's imperative escape hatch,
    // so nothing from render scope is captured.
  }, []);

  useEffect(() => {
    const href = getTheme(theme).cssPath;
    const existing = document.getElementById(
      "theme-css",
    ) as HTMLLinkElement | null;
    if (existing) {
      existing.href = href;
    } else {
      const el = document.createElement("link");
      el.id = "theme-css";
      el.rel = "stylesheet";
      el.href = href;
      document.head.appendChild(el);
    }
  }, [theme]);

  useEffect(() => {
    let cancelled = false;
    let unreachable = false;
    initApi()
      .then(() => {
        if (cancelled) return;
        setBrokerConnected(true);
        // Configure product analytics from the broker's runtime injection
        // (dormant unless a PostHog key resolves; respects the consent
        // toggles). Best-effort and non-blocking — never gates boot.
        configureAnalytics(getInjectedAnalyticsConfig() ?? undefined);
        return get<{ onboarded?: boolean; phase?: string }>(
          "/onboarding/state",
        );
      })
      .then((s) => {
        if (cancelled || !s) return;
        setBootError(false);
        if (s.onboarded === true) {
          setOnboardingComplete(true);
        } else if (typeof s.phase === "string" && s.phase) {
          // Resume mid-onboarding: broker already started the CEO phase machine.
          setBootPhase(s.phase);
          setInCeoOnboarding(true);
        }
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (isBrokerUnreachableError(err)) {
          // Broker wedged (5xx) or never answered (network failure /
          // timeout): render the honest full-page fallback instead of a
          // blank body or a misleading fresh-install screen.
          // eslint-disable-next-line no-console
          console.warn(
            `[WUPHF boot] broker unreachable (attempt ${bootAttempt + 1})`,
            err,
          );
          unreachable = true;
          setBootError(true);
          return;
        }
        // Broker answered with a client error (e.g. onboarding endpoint
        // not mounted on a fresh install) — clear any prior wedge state
        // and fall through to PrePickScreen.
        setBootError(false);
      })
      .finally(() => {
        // Keep apiReady false while unreachable so SSE hooks stay idle and
        // the fallback owns the page until a retry succeeds.
        if (!(cancelled || unreachable)) setApiReady(true);
      });
    return () => {
      cancelled = true;
    };
  }, [bootAttempt, setBrokerConnected, setOnboardingComplete]);

  // Auto-retry while the broker is unreachable — the fallback copy promises
  // "retrying…", so keep that promise without requiring a click. Reads
  // bootAttempt (not a functional update) so a failed retry — which leaves
  // bootError true but bumps the attempt — re-arms the timer.
  useEffect(() => {
    if (!bootError) return;
    const next = bootAttempt + 1;
    const timer = setTimeout(() => {
      setBootAttempt(next);
    }, BOOT_RETRY_MS);
    return () => clearTimeout(timer);
  }, [bootError, bootAttempt]);

  let body: ReactNode;
  if (bootError) {
    body = (
      <BrokerUnreachableScreen onRetry={() => setBootAttempt((a) => a + 1)} />
    );
  } else if (!apiReady) {
    body = (
      <div
        style={{
          height: "100vh",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          color: "var(--text-tertiary)",
          fontSize: 14,
        }}
      >
        Connecting to broker...
      </div>
    );
  } else if (!onboardingComplete) {
    if (inCeoOnboarding || bootPhase) {
      // Visual stepped wizard — full-screen, NOT inside the office Shell. The
      // user is not "in the office" yet. The wizard educates with a persistent
      // mock office and creates the team (pick a blueprint, brief the first
      // agent, write the first issue), then POSTs /onboarding/complete to seed
      // the office and flip onboarded=true. Its onComplete fires after the seed
      // succeeds, so we flip onboardingComplete here and the office Shell mounts
      // via the branch below with the first issue already seeded into the CEO
      // DM composer (pendingComposerDraft, set inside the wizard hook).
      body = (
        <OnboardingWizard onComplete={() => setOnboardingComplete(true)} />
      );
    } else {
      // Provider picker. No phase set yet — user hasn't picked a runtime.
      // After they pick, setInCeoOnboarding(true) to enter the wizard.
      body = (
        <PrePickScreen
          onComplete={(info) => {
            // Issue #979: when the broker reports phase=complete at pick time
            // (session-loss recovery), skip the wizard hand-off entirely and
            // route straight into the office. Otherwise enter the normal
            // onboarding hand-off: PrePickScreen has just POSTed
            // /onboarding/transition phase=greet, so RootRoute renders the
            // visual OnboardingWizard until it seeds the office and calls
            // onComplete.
            if (info?.phaseAlreadyComplete) {
              setOnboardingComplete(true);
              return;
            }
            setInCeoOnboarding(true);
          }}
        />
      );
    }
  } else {
    body = (
      <Shell>
        <RoutedBody />
        {/* Outlet renders the matched leaf route's default component (an
            empty <Outlet />), which is invisible. The route is the
            single source of truth for navigation state — RoutedBody
            reads it via useCurrentRoute. */}
        <Outlet />
        {/* Replay only: an already-onboarded user reopened the tour from Help,
            so it overlays the live office at --z-modal. First-run shows the
            tour as the surface above (no Shell behind), per the converged arc. */}
        {officeTour.open && officeTour.replay ? (
          <OfficeTour
            open={true}
            onClose={officeTour.skip}
            onFinish={handleTourFinish}
          />
        ) : null}
      </Shell>
    );
  }

  return (
    <ErrorBoundary>
      <UpgradeBanner />
      {body}
      <ToastContainer />
      <ConfirmHost />
      <ProviderSwitcherHost />
      <TelegramConnectHost />
    </ErrorBoundary>
  );
}
