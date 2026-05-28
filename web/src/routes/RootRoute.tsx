import {
  Component,
  type ComponentType,
  lazy,
  type ReactNode,
  Suspense,
  useEffect,
  useState,
} from "react";
import {
  Link,
  Outlet,
  useMatches,
  useRouterState,
} from "@tanstack/react-router";

import { get, initApi } from "../api/client";
import { TelegramConnectHost } from "../components/integrations/TelegramConnectModal";
import { Shell } from "../components/layout/Shell";
import { UpgradeBanner } from "../components/layout/UpgradeBanner";
import { ChannelParticipants } from "../components/messages/ChannelParticipants";
import { Composer } from "../components/messages/Composer";
import { DMView } from "../components/messages/DMView";
import { InterviewBar } from "../components/messages/InterviewBar";
import { MessageFeed } from "../components/messages/MessageFeed";
import { TypingIndicator } from "../components/messages/TypingIndicator";
import { OnboardingChat } from "../components/onboarding/OnboardingChat";
import { PrePickScreen } from "../components/onboarding/PrePickScreen";
import { ConfirmHost } from "../components/ui/ConfirmDialog";
import { ProviderSwitcherHost } from "../components/ui/ProviderSwitcher";
import { ToastContainer } from "../components/ui/Toast";
import type { WikiTab } from "../components/wiki/WikiTabs";
import WikiTabs from "../components/wiki/WikiTabs";
import { useBrokerEvents } from "../hooks/useBrokerEvents";
import { useKeyboardShortcuts } from "../hooks/useKeyboardShortcuts";
import { rootRoute, router } from "../lib/router";
import { getTheme } from "../lib/themes";
import { useAppStore } from "../stores/app";
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
const CalendarApp = lazy(() =>
  import("../components/apps/CalendarApp").then((m) => ({
    default: m.CalendarApp,
  })),
);
const ConsoleApp = lazy(() =>
  import("../components/apps/ConsoleApp").then((m) => ({
    default: m.ConsoleApp,
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
const ReceiptsApp = lazy(() =>
  import("../components/apps/ReceiptsApp").then((m) => ({
    default: m.ReceiptsApp,
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
const SkillsApp = lazy(() =>
  import("../components/apps/SkillsApp").then((m) => ({
    default: m.SkillsApp,
  })),
);
const TasksApp = lazy(() =>
  import("../components/apps/TasksApp").then((m) => ({
    default: m.TasksApp,
  })),
);
const Notebook = lazy(() => import("../components/notebook/Notebook"));
const DecisionInbox = lazy(() =>
  import("../components/lifecycle/DecisionInbox").then((m) => ({
    default: m.DecisionInbox,
  })),
);
const DecisionPacketRoute = lazy(() =>
  import("../components/lifecycle/DecisionPacketRoute").then((m) => ({
    default: m.DecisionPacketRoute,
  })),
);
const CitedAnswer = lazy(() => import("../components/wiki/CitedAnswer"));
const Wiki = lazy(() => import("../components/wiki/Wiki"));
const ReviewQueueKanban = lazy(
  () => import("../components/review/ReviewQueueKanban"),
);
// Phase 3 — Issues surface.
const IssuesList = lazy(() =>
  import("../components/lifecycle/IssuesList").then((m) => ({
    default: m.IssuesList,
  })),
);
const IssueDocumentRoute = lazy(() =>
  import("../components/lifecycle/IssueDocumentRoute").then((m) => ({
    default: m.IssueDocumentRoute,
  })),
);
const IssueNewForm = lazy(() =>
  import("../components/lifecycle/IssueNewForm").then((m) => ({
    default: m.IssueNewForm,
  })),
);
// v3 MVP — per-agent subspace shell.
const AgentSubspaceRoute = lazy(() => import("./AgentSubspaceRoute"));
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
  tasks: TasksApp,
  requests: InboxRedirect,
  graph: GraphApp,
  policies: PoliciesApp,
  calendar: CalendarApp,
  skills: SkillsApp,
  activity: ArtifactsApp,
  receipts: ReceiptsApp,
  "health-check": HealthCheckApp,
  integrations: IntegrationsApp,
  settings: SettingsApp,
  console: ConsoleApp,
} satisfies Record<AppPanelId, ComponentType>;

function ConversationView() {
  const channelSlug = useChannelSlug() ?? "general";

  return (
    <div className="conversation-shell">
      <div className="conversation-chat">
        <MessageFeed />
        <TypingIndicator />
        <InterviewBar />
        <Composer />
      </div>
      <ChannelParticipants channelSlug={channelSlug} />
    </div>
  );
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
        {current === "reviews" && <ReviewQueueKanban />}
      </div>
    </div>
  );
}

/**
 * InboxRedirect navigates the user from the deprecated /apps/requests
 * surface to the unified Inbox. Phase 2 collapsed Requests into the
 * Decision Inbox; this stub keeps existing /apps/requests bookmarks
 * working. (The Reviews tab inside Wiki is its own surface again —
 * see ReviewQueueKanban.)
 */
function InboxRedirect() {
  useEffect(() => {
    void router.navigate({ to: "/inbox", replace: true });
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
        Redirecting to Inbox…
      </div>
    </div>
  );
}

/**
 * FirstClassAppRedirect navigates the user from a `/apps/$id` URL whose
 * `$id` is a first-class app (wiki, inbox) to that app's canonical
 * dedicated route. Users who type a sidebar-label-style URL by hand
 * (e.g. `/#/apps/wiki`) used to hit "Page not found" because first-class
 * apps live at `/wiki` and `/inbox`, not under `/apps`. Mirrors
 * `InboxRedirect` so the route ↔ sidebar mapping is forgiving without
 * the route registry sprouting alias entries.
 */
function FirstClassAppRedirect({ appId }: { appId: FirstClassAppId }) {
  useEffect(() => {
    if (appId === "wiki") {
      void router.navigate({ to: "/wiki", replace: true });
    } else {
      void router.navigate({ to: "/inbox", replace: true });
    }
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
 * (ConsoleApp, RequestsApp, sidebar request badge) keep pointing at
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
  const channelSlug =
    route.kind === "channel" || route.kind === "dm" ? route.channelSlug : null;
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
    case "channel":
      return <ConversationView />;
    case "dm":
      return (
        <DMView agentSlug={route.agentSlug} channelSlug={route.channelSlug} />
      );
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
      return (
        <div className="app-panel active" data-testid="app-page-tasks">
          <TasksApp />
        </div>
      );
    case "task-detail":
      return (
        <div className="app-panel active" data-testid="app-page-tasks">
          <TasksApp taskId={route.taskId} />
        </div>
      );
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
    case "inbox":
      return <DecisionInbox />;
    case "task-decision":
      return <DecisionPacketRoute taskId={route.taskId} />;
    case "issues-list":
      return <IssuesList />;
    case "issue-detail":
      return <IssueDocumentRoute issueId={route.issueId} />;
    case "issue-new":
      return <IssueNewForm />;
    case "agent-subspace":
      return (
        <AgentSubspaceRoute agentSlug={route.agentSlug} tab={route.tab} />
      );
    case "skill-detail":
      return <SkillDetailRoute skillName={route.skillName} />;
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

  // When CEO onboarding is active (phase set, not "complete"), redirect any
  // root or /channels/general URL to the CEO DM so the Shell always shows
  // the CEO conversation regardless of the URL the user landed on.
  // This is a TanStack-level navigate (not a beforeLoad, because onboarding
  // state is only known after the /onboarding/state API call in the effect
  // below). Already-onboarded users (onboardingComplete === true) skip the
  // whole inCeoOnboarding branch and never hit this redirect.
  useEffect(() => {
    if (!(inCeoOnboarding || bootPhase)) return;
    // Only redirect when the user is on a generic destination (root, general).
    // Other explicit URLs (e.g. /dm/some-agent) should not be redirected so
    // deep-links remain functional during onboarding.
    const hash = typeof window !== "undefined" ? window.location.hash : "";
    const onGenericRoute =
      hash === "" ||
      hash === "#/" ||
      hash === "#/channels/general" ||
      hash.startsWith("#/?");
    if (onGenericRoute) {
      void router.navigate({
        to: "/dm/$agentSlug",
        params: { agentSlug: "ceo" },
        replace: true,
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inCeoOnboarding, bootPhase]);

  useKeyboardShortcuts();
  useBrokerEvents(apiReady);

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
    initApi()
      .then(() => {
        if (cancelled) return;
        setBrokerConnected(true);
        return get<{ onboarded?: boolean; phase?: string }>(
          "/onboarding/state",
        );
      })
      .then((s) => {
        if (cancelled || !s) return;
        if (s.onboarded === true) {
          setOnboardingComplete(true);
        } else if (typeof s.phase === "string" && s.phase) {
          // Resume mid-onboarding: broker already started the CEO phase machine.
          setBootPhase(s.phase);
          setInCeoOnboarding(true);
        }
      })
      .catch(() => {
        // Endpoint unreachable — fall through to PrePickScreen. Safer default
        // for fresh installs where the broker may not have mounted onboarding.
      })
      .finally(() => {
        if (!cancelled) setApiReady(true);
      });
    return () => {
      cancelled = true;
    };
  }, [setBrokerConnected, setOnboardingComplete]);

  let body: ReactNode;
  if (!apiReady) {
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
      // CEO wizard running — render OnboardingChat full-screen, NOT inside
      // the office Shell. The user is not "in the office" yet; the wizard
      // is mocked as a CEO chat so the experience reads as a conversation
      // while still gating progress through deterministic chip / form
      // cards. Once the broker flips onboarded=true the user falls through
      // to the post-onboarding Shell branch below and lands in the office.
      body = <OnboardingChat />;
    } else {
      // Provider picker. No phase set yet — user hasn't picked a runtime.
      // After they pick, setInCeoOnboarding(true) to enter the CEO DM.
      body = (
        <PrePickScreen
          onComplete={(info) => {
            // Issue #979: when the broker reports phase=complete at pick time
            // (session-loss recovery), skip the CEO DM hand-off entirely and
            // route straight into the office. Otherwise enter the normal CEO
            // onboarding hand-off: PrePickScreen has just POSTed
            // /onboarding/transition phase=greet, so the Shell renders the
            // OnboardingDMRoute around the CEO DM until the broker flips
            // onboarded=true.
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
