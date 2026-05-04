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
import {
  appRoute,
  channelRoute,
  dmRoute,
  indexRoute,
  notebookAgentRoute,
  notebookEntryRoute,
  notebooksRoute,
  reviewsRoute,
  router,
  wikiArticleRoute,
  wikiIndexRoute,
  wikiLookupRoute,
} from "../lib/router";
import {
  type ChannelMeta,
  directChannelSlug,
  isDMChannel,
  useAppStore,
} from "../stores/app";

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
// Keeps the navigation slice of the Zustand store in sync with the matched
// route. Single source of truth for hydration; running at the root means
// nested route effects don't have to coordinate write order.
//
// Layout-effect timing ensures the store is written before child renders
// observe a stale slice on the same commit.

interface HydratorSetters {
  setCurrentApp: (id: string | null) => void;
  setCurrentChannel: (slug: string) => void;
  setLastMessageId: (id: string | null) => void;
  enterDM: (agentSlug: string, channelSlug: string) => void;
  setWikiPath: (path: string | null) => void;
  setWikiLookupQuery: (q: string | null) => void;
  setNotebookRoute: (
    agentSlug: string | null,
    entrySlug: string | null,
  ) => void;
}

type HydratorFn = (
  params: Record<string, string | undefined>,
  search: Record<string, unknown>,
  setters: HydratorSetters,
) => void;

const ROUTE_HYDRATORS: Record<string, HydratorFn> = {
  [channelRoute.id]: (params, _search, s) => {
    s.setCurrentApp(null);
    s.setCurrentChannel(params.channelSlug ?? "general");
    s.setLastMessageId(null);
  },
  [dmRoute.id]: (params, _search, s) => {
    const agentSlug = params.agentSlug ?? "";
    if (!agentSlug) return;
    s.enterDM(agentSlug, directChannelSlug(agentSlug));
    s.setCurrentApp(null);
    s.setLastMessageId(null);
  },
  [appRoute.id]: (params, _search, s) => {
    const appId = params.appId ?? "";
    if (appId) s.setCurrentApp(appId);
  },
  [wikiIndexRoute.id]: (_params, _search, s) => {
    s.setCurrentApp("wiki");
    s.setWikiPath(null);
  },
  [wikiLookupRoute.id]: (_params, search, s) => {
    s.setWikiLookupQuery(typeof search.q === "string" ? search.q : null);
    s.setCurrentApp("wiki-lookup");
  },
  [wikiArticleRoute.id]: (params, _search, s) => {
    const splat =
      typeof params._splat === "string" && params._splat.length > 0
        ? params._splat
        : null;
    s.setWikiPath(splat);
    s.setCurrentApp("wiki");
  },
  [notebooksRoute.id]: (_params, _search, s) => {
    s.setNotebookRoute(null, null);
    s.setCurrentApp("notebooks");
  },
  [notebookAgentRoute.id]: (params, _search, s) => {
    s.setNotebookRoute(params.agentSlug ?? null, null);
    s.setCurrentApp("notebooks");
  },
  [notebookEntryRoute.id]: (params, _search, s) => {
    s.setNotebookRoute(params.agentSlug ?? null, params.entrySlug ?? null);
    s.setCurrentApp("notebooks");
  },
  [reviewsRoute.id]: (_params, _search, s) => {
    s.setCurrentApp("reviews");
  },
};

function applyMatchToStore(
  routeId: string,
  params: Record<string, string | undefined>,
  search: Record<string, unknown>,
  setters: HydratorSetters,
): void {
  ROUTE_HYDRATORS[routeId]?.(params, search, setters);
}

function UrlToStoreHydrator() {
  const matches = useMatches();
  const navigate = useNavigate();
  // Select each setter as its own stable function reference. Zustand store
  // actions are referentially stable, so each selector returns the same
  // function across renders and avoids spurious re-renders.
  const setCurrentApp = useAppStore((s) => s.setCurrentApp);
  const setCurrentChannel = useAppStore((s) => s.setCurrentChannel);
  const setLastMessageId = useAppStore((s) => s.setLastMessageId);
  const enterDM = useAppStore((s) => s.enterDM);
  const setWikiPath = useAppStore((s) => s.setWikiPath);
  const setWikiLookupQuery = useAppStore((s) => s.setWikiLookupQuery);
  const setNotebookRoute = useAppStore((s) => s.setNotebookRoute);

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
    applyMatchToStore(routeId, params, search, {
      setCurrentApp,
      setCurrentChannel,
      setLastMessageId,
      enterDM,
      setWikiPath,
      setWikiLookupQuery,
      setNotebookRoute,
    });
  }, [
    routeId,
    paramsKey,
    searchKey,
    navigate,
    setCurrentApp,
    setCurrentChannel,
    setLastMessageId,
    enterDM,
    setWikiPath,
    setWikiLookupQuery,
    setNotebookRoute,
  ]);

  return null;
}

// ── Store→URL bridge ────────────────────────────────────────────
//
// Mirrors navigation-relevant store fields back into the router so the URL
// stays fresh while existing call sites still mutate the store directly
// (sidebar clicks, slash commands, MainContent's wiki tabs, etc.).
//
// Skips the navigate call when the derived URL already matches the current
// pathname. Safe against loops because (a) the hydrator above only writes
// the store, never the URL, and (b) writes from the hydrator that match the
// current store slice produce no Zustand emission.
//
// **Debt**: this bridge is the temporary adapter from step 2 of the router
// migration. Step 3 replaces every call site with `router.navigate(...)`,
// and step 4 deletes the mirrored store fields. When both are done, this
// component goes away.

interface NavSlice {
  currentApp: string | null;
  currentChannel: string;
  channelMeta: Record<string, ChannelMeta>;
  wikiPath: string | null;
  wikiLookupQuery: string | null;
  notebookAgentSlug: string | null;
  notebookEntrySlug: string | null;
}

interface NavTarget {
  to:
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

function deriveNavTarget(slice: NavSlice): NavTarget {
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

function fillPath(target: NavTarget): string {
  let path: string = target.to;
  if (target.params) {
    for (const [key, value] of Object.entries(target.params)) {
      const placeholder = key === "_splat" ? "$" : `$${key}`;
      path = path.replace(placeholder, encodeURIComponent(value));
    }
  }
  return path;
}

function pickNavSlice(s: ReturnType<typeof useAppStore.getState>): NavSlice {
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

function navSliceEquals(a: NavSlice, b: NavSlice): boolean {
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

function StoreToRouterBridge() {
  const navigate = useNavigate();

  useEffect(() => {
    // Subscribe directly to Zustand instead of mirroring the slice into
    // hook state. This means:
    //   - The bridge fires only on store changes, never on URL changes.
    //     URL changes are handled by UrlToStoreHydrator above.
    //   - There are no useEffect deps that look "unused" because we read
    //     the live state at fire time (router.state for the URL,
    //     getState() implicitly via the subscribe callback's argument).
    //   - When the hydrator writes the same slice that the URL just
    //     described, the bridge sees no slice change and stays silent;
    //     no-op writes can't cause it to fire because Zustand store
    //     actions return new state objects only when fields actually
    //     change.
    let prev = pickNavSlice(useAppStore.getState());
    const unsubscribe = useAppStore.subscribe((state) => {
      const next = pickNavSlice(state);
      if (navSliceEquals(prev, next)) return;
      prev = next;

      const target = deriveNavTarget(next);
      const targetPath = fillPath(target);
      const targetSearch =
        target.search && Object.keys(target.search).length > 0
          ? `?${new URLSearchParams(target.search).toString()}`
          : "";
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
          <MainContent />
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
