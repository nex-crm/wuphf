import {
  Component,
  type ComponentType,
  type ReactNode,
  useEffect,
  useLayoutEffect,
  useState,
} from "react";
import { Outlet, useMatches, useNavigate } from "@tanstack/react-router";

import { get, initApi } from "../api/client";
import { ArtifactsApp } from "../components/apps/ArtifactsApp";
import { CalendarApp } from "../components/apps/CalendarApp";
import { ConsoleApp } from "../components/apps/ConsoleApp";
import { GraphApp } from "../components/apps/GraphApp";
import { HealthCheckApp } from "../components/apps/HealthCheckApp";
import { PoliciesApp } from "../components/apps/PoliciesApp";
import { ReceiptsApp } from "../components/apps/ReceiptsApp";
import { RequestsApp } from "../components/apps/RequestsApp";
import { SettingsApp } from "../components/apps/SettingsApp";
import { SkillsApp } from "../components/apps/SkillsApp";
import { TasksApp } from "../components/apps/TasksApp";
import { ThreadsApp } from "../components/apps/ThreadsApp";
import { TelegramConnectHost } from "../components/integrations/TelegramConnectModal";
import { Shell } from "../components/layout/Shell";
import { UpgradeBanner } from "../components/layout/UpgradeBanner";
import { Composer } from "../components/messages/Composer";
import { DMView } from "../components/messages/DMView";
import { InterviewBar } from "../components/messages/InterviewBar";
import { MessageFeed } from "../components/messages/MessageFeed";
import { TypingIndicator } from "../components/messages/TypingIndicator";
import Notebook from "../components/notebook/Notebook";
import { SplashScreen } from "../components/onboarding/SplashScreen";
import { Wizard } from "../components/onboarding/Wizard";
import ReviewQueueKanban from "../components/review/ReviewQueueKanban";
import { ConfirmHost } from "../components/ui/ConfirmDialog";
import { ProviderSwitcherHost } from "../components/ui/ProviderSwitcher";
import { ToastContainer } from "../components/ui/Toast";
import CitedAnswer from "../components/wiki/CitedAnswer";
import Wiki from "../components/wiki/Wiki";
import type { WikiTab } from "../components/wiki/WikiTabs";
import WikiTabs from "../components/wiki/WikiTabs";
import { useBrokerEvents } from "../hooks/useBrokerEvents";
import { useKeyboardShortcuts } from "../hooks/useKeyboardShortcuts";
import { indexRoute, router } from "../lib/router";
import { isDMChannel, useAppStore } from "../stores/app";
import {
  applyMatchToStore,
  deriveNavTarget,
  fillPath,
  isUnmatchedRoute,
  navSliceEquals,
  navTargetSearchString,
  pickNavSlice,
} from "./routeSync";

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

function MainContent() {
  const currentApp = useAppStore((s) => s.currentApp);
  const currentChannel = useAppStore((s) => s.currentChannel);
  const channelMeta = useAppStore((s) => s.channelMeta);
  const wikiPath = useAppStore((s) => s.wikiPath);
  const setWikiPath = useAppStore((s) => s.setWikiPath);
  const wikiLookupQuery = useAppStore((s) => s.wikiLookupQuery);
  const setCurrentApp = useAppStore((s) => s.setCurrentApp);
  const notebookAgentSlug = useAppStore((s) => s.notebookAgentSlug);
  const notebookEntrySlug = useAppStore((s) => s.notebookEntrySlug);
  const setNotebookRoute = useAppStore((s) => s.setNotebookRoute);
  // Pam's onActionDone bumps this; Wiki re-fetches article + history when
  // the prop changes. Lifted up here because Pam lives inside the tab bar
  // (so her desk can rest on the divider line).
  const [articleRefreshNonce, setArticleRefreshNonce] = useState(0);

  if (!currentApp && isDMChannel(currentChannel, channelMeta)) {
    return <DMView />;
  }

  if (currentApp === "wiki-lookup") {
    return (
      <div className="wiki-shell">
        <WikiTabs
          current="wiki"
          onSelect={(tab) => {
            if (tab === "wiki") setCurrentApp("wiki");
            else if (tab === "notebooks") {
              setNotebookRoute(null, null);
              setCurrentApp("notebooks");
            } else setCurrentApp("reviews");
          }}
        />
        <div className="wiki-shell-body">
          <CitedAnswer query={wikiLookupQuery || ""} />
        </div>
      </div>
    );
  }

  if (
    currentApp === "wiki" ||
    currentApp === "notebooks" ||
    currentApp === "reviews"
  ) {
    const handleTabChange = (tab: WikiTab) => {
      if (tab === "wiki") {
        setCurrentApp("wiki");
      } else if (tab === "notebooks") {
        setNotebookRoute(null, null);
        setCurrentApp("notebooks");
      } else {
        setCurrentApp("reviews");
      }
    };

    const pamArticlePath = currentApp === "wiki" ? (wikiPath ?? null) : null;

    return (
      <div className="wiki-shell">
        <WikiTabs
          current={currentApp}
          onSelect={handleTabChange}
          pamArticlePath={pamArticlePath}
          onPamActionDone={() => setArticleRefreshNonce((n) => n + 1)}
        />
        <div className="wiki-shell-body">
          {currentApp === "wiki" && (
            <Wiki
              articlePath={wikiPath}
              externalRefreshNonce={articleRefreshNonce}
              onNavigate={(path) => {
                if (path === null) {
                  setWikiPath(null);
                } else {
                  setWikiPath(path || null);
                }
              }}
            />
          )}
          {currentApp === "notebooks" && (
            <Notebook
              agentSlug={notebookAgentSlug}
              entrySlug={notebookEntrySlug}
              onOpenCatalog={() => setNotebookRoute(null, null)}
              onOpenAgent={(slug) => setNotebookRoute(slug, null)}
              onOpenEntry={(slug, entry) => setNotebookRoute(slug, entry)}
              onNavigateWiki={(path) => {
                setCurrentApp("wiki");
                setWikiPath(path || null);
              }}
            />
          )}
          {currentApp === "reviews" && <ReviewQueueKanban />}
        </div>
      </div>
    );
  }

  if (currentApp) {
    const panels: Record<string, ComponentType> = {
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
    const Panel = panels[currentApp];
    return (
      <div className="app-panel active" data-testid={`app-page-${currentApp}`}>
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
            Unknown app: {currentApp}
          </div>
        )}
      </div>
    );
  }

  return (
    <>
      <MessageFeed />
      <TypingIndicator />
      <InterviewBar />
      <Composer />
    </>
  );
}

// ── URL→store hydrator ──────────────────────────────────────────
//
// Single source of truth for URL→store hydration: writes the matched
// route's params/search into the existing Zustand fields atomically.
// Living at the root means nested route effects don't have to coordinate
// write order, and a layout-effect commit keeps the store ahead of any
// child render that observes the slice.
//
// The dispatch + atomic-setState logic lives in `./routeSync.ts` so it's
// independently testable without rendering the whole shell.

function UrlToStoreHydrator() {
  const matches = useMatches();
  const navigate = useNavigate();

  const leaf = matches.at(-1);
  const routeId = leaf?.routeId ?? "";
  // Stringify so the effect only re-fires when params/search structurally
  // change. useMatches returns fresh references each render, so passing
  // the live objects as deps would over-fire on every store update that
  // re-renders RootRoute.
  const paramsKey = JSON.stringify(leaf?.params ?? {});
  const searchKey = JSON.stringify(leaf?.search ?? {});

  // Index route redirects to the default channel. Doing it in a
  // layout-effect (rather than a route loader) keeps the route definitions
  // dependency-free and lets the store stay the single hydration target.
  useLayoutEffect(() => {
    if (routeId === indexRoute.id) {
      void navigate({
        to: "/channels/$channelSlug",
        params: { channelSlug: "general" },
        replace: true,
      });
      return;
    }
    const params = JSON.parse(paramsKey) as Record<string, string | undefined>;
    const search = JSON.parse(searchKey) as Record<string, unknown>;
    applyMatchToStore(routeId, params, search);
  }, [routeId, paramsKey, searchKey, navigate]);

  return null;
}

// ── Store→URL bridge ────────────────────────────────────────────
//
// Mirrors navigation-relevant store fields back into the router so the URL
// stays fresh while existing call sites still mutate the store directly
// (sidebar clicks, slash commands, MainContent's wiki tabs, etc.).
//
// Skips the navigate call when the derived URL already matches the current
// pathname. Safe against loops because the hydrator only writes the store
// (never the URL), and the bridge subscribes to Zustand directly so it
// only fires on actual store changes.
//
// **Debt**: this bridge is the temporary adapter from step 2 of the router
// migration. Step 3 replaces every call site with `router.navigate(...)`
// and deletes the bridge; step 4 deletes the mirrored store fields and
// the hydrator above.

function StoreToRouterBridge() {
  const navigate = useNavigate();

  useEffect(() => {
    let prev = pickNavSlice(useAppStore.getState());
    const unsubscribe = useAppStore.subscribe((state) => {
      const next = pickNavSlice(state);
      if (navSliceEquals(prev, next)) return;
      prev = next;

      const target = deriveNavTarget(next);
      const targetPath = fillPath(target);
      const targetSearch = navTargetSearchString(target);
      const currentPath = router.state.location.pathname;
      const currentSearchStr = router.state.location.searchStr;
      if (
        decodeURIComponent(currentPath) === decodeURIComponent(targetPath) &&
        currentSearchStr === targetSearch
      ) {
        return;
      }
      void navigate({ ...target, replace: true });
    });
    return unsubscribe;
  }, [navigate]);

  return null;
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
  if (isUnmatchedRoute(leaf?.routeId)) {
    return <NotFoundSurface pathname={router.state.location.pathname} />;
  }
  return <MainContent />;
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
      <>
        <UrlToStoreHydrator />
        <StoreToRouterBridge />
        <Shell>
          <RoutedBody />
          {/* Outlet renders the matched leaf route's default component (an
              empty <Outlet />), which is invisible. Hydration of the store
              from the matched route happens in UrlToStoreHydrator above. */}
          <Outlet />
        </Shell>
      </>
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
