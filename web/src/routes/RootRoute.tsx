import {
  Component,
  type ComponentType,
  lazy,
  type ReactNode,
  Suspense,
  useEffect,
  useState,
} from "react";
import { Outlet, useMatches, useRouterState } from "@tanstack/react-router";

import { get, initApi } from "../api/client";
import { TelegramConnectHost } from "../components/integrations/TelegramConnectModal";
import { Shell } from "../components/layout/Shell";
import { UpgradeBanner } from "../components/layout/UpgradeBanner";
import { Composer } from "../components/messages/Composer";
import { DMView } from "../components/messages/DMView";
import { InterviewBar } from "../components/messages/InterviewBar";
import { MessageFeed } from "../components/messages/MessageFeed";
import { TypingIndicator } from "../components/messages/TypingIndicator";
import { SplashScreen } from "../components/onboarding/SplashScreen";
import { Wizard } from "../components/onboarding/Wizard";
import { ConfirmHost } from "../components/ui/ConfirmDialog";
import { ProviderSwitcherHost } from "../components/ui/ProviderSwitcher";
import { ToastContainer } from "../components/ui/Toast";
import type { WikiTab } from "../components/wiki/WikiTabs";
import WikiTabs from "../components/wiki/WikiTabs";
import { useBrokerEvents } from "../hooks/useBrokerEvents";
import { useKeyboardShortcuts } from "../hooks/useKeyboardShortcuts";
import { rootRoute, router } from "../lib/router";
import { useAppStore } from "../stores/app";
import { type CurrentRoute, useCurrentRoute } from "./useCurrentRoute";

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
// (xterm in Console, three.js in Graph, the markdown stack in Wiki and
// Notebook); deferring them keeps the conversation surface — which is
// what every user lands on — small and fast.
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
const RequestsApp = lazy(() =>
  import("../components/apps/RequestsApp").then((m) => ({
    default: m.RequestsApp,
  })),
);
const SettingsApp = lazy(() =>
  import("../components/apps/SettingsApp").then((m) => ({
    default: m.SettingsApp,
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
const ThreadsApp = lazy(() =>
  import("../components/apps/ThreadsApp").then((m) => ({
    default: m.ThreadsApp,
  })),
);
const Notebook = lazy(() => import("../components/notebook/Notebook"));
const ReviewQueueKanban = lazy(
  () => import("../components/review/ReviewQueueKanban"),
);
const CitedAnswer = lazy(() => import("../components/wiki/CitedAnswer"));
const Wiki = lazy(() => import("../components/wiki/Wiki"));

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
// Reads the current navigation slice from the Zustand store and renders the
// matching panel. During the router migration, the URL→store hydrator below
// keeps the store in sync with the matched route, so MainContent can keep
// its existing store-driven shape until step 4 (drain route fields out of
// Zustand).

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

const APP_PANELS: Record<string, ComponentType> = {
  tasks: TasksApp,
  requests: RequestsApp,
  graph: GraphApp,
  policies: PoliciesApp,
  calendar: CalendarApp,
  skills: SkillsApp,
  activity: ArtifactsApp,
  receipts: ReceiptsApp,
  "health-check": HealthCheckApp,
  settings: SettingsApp,
  threads: ThreadsApp,
  console: ConsoleApp,
};

function ConversationView() {
  return (
    <>
      <MessageFeed />
      <TypingIndicator />
      <InterviewBar />
      <Composer />
    </>
  );
}

interface WikiSurfaceProps {
  current: "wiki" | "notebooks" | "reviews";
  route: CurrentRoute;
}

function WikiSurface({ current, route }: WikiSurfaceProps) {
  // Pam's onActionDone bumps this; Wiki re-fetches article + history when
  // the prop changes. Lifted to the surface so Pam (in the tab bar) can
  // tell Wiki to re-render without remounting it.
  const [articleRefreshNonce, setArticleRefreshNonce] = useState(0);

  const articlePath = route.kind === "wiki-article" ? route.articlePath : null;
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

function AppPanel({ appId }: { appId: string }) {
  const Panel = APP_PANELS[appId];
  return (
    <div className="app-panel active" data-testid={`app-page-${appId}`}>
      {Panel ? (
        <Panel />
      ) : (
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
      )}
    </div>
  );
}

function MainContent() {
  const route = useCurrentRoute();

  switch (route.kind) {
    case "channel":
      return <ConversationView />;
    case "dm":
      return (
        <DMView agentSlug={route.agentSlug} channelSlug={route.channelSlug} />
      );
    case "app":
      return <AppPanel appId={route.appId} />;
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
    case "unknown":
      // Handled by RoutedBody/NotFoundSurface; reaching here means the
      // not-found check upstream didn't catch a root-only match. Return
      // null defensively rather than render stale content.
      return null;
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
      <a href="#/channels/general" style={{ color: "var(--text-secondary)" }}>
        Go to #general
      </a>
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
// components are intentionally absent: URL→store hydration happens in a
// single root-level effect so child routes don't fight over write order.
//
// Critical rules (violations caused the blank-page regression):
// 1. ALL hooks are called unconditionally at the top. No early returns
//    before hook calls.
// 2. initApi() runs in an effect, but we render the shell immediately so
//    the user sees something even while init is pending.
// 3. ErrorBoundary wraps the whole tree so render errors are visible.

export default function RootRoute() {
  const [apiReady, setApiReady] = useState(false);
  const [showSplash, setShowSplash] = useState(false);
  const theme = useAppStore((s) => s.theme);
  const onboardingComplete = useAppStore((s) => s.onboardingComplete);
  const setBrokerConnected = useAppStore((s) => s.setBrokerConnected);
  const setOnboardingComplete = useAppStore((s) => s.setOnboardingComplete);

  useKeyboardShortcuts();
  useBrokerEvents(apiReady);

  useEffect(() => {
    const existing = document.getElementById(
      "theme-css",
    ) as HTMLLinkElement | null;
    if (existing) {
      existing.href = `/themes/${theme}.css`;
    } else {
      const el = document.createElement("link");
      el.id = "theme-css";
      el.rel = "stylesheet";
      el.href = `/themes/${theme}.css`;
      document.head.appendChild(el);
    }
  }, [theme]);

  useEffect(() => {
    let cancelled = false;
    initApi()
      .then(() => {
        if (cancelled) return;
        setBrokerConnected(true);
        return get<{ onboarded?: boolean }>("/onboarding/state");
      })
      .then((s) => {
        if (cancelled || !s) return;
        if (s.onboarded === true) {
          setOnboardingComplete(true);
        }
      })
      .catch(() => {
        // Endpoint unreachable — fall through to wizard. Safer default for
        // fresh installs where the broker may not have mounted onboarding yet.
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
  } else if (showSplash) {
    body = <SplashScreen onDone={() => setShowSplash(false)} />;
  } else if (!onboardingComplete) {
    body = (
      <Wizard
        onComplete={() => {
          setShowSplash(true);
        }}
      />
    );
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
