import {
  Component,
  type ComponentType,
  lazy,
  type ReactNode,
  Suspense,
  useEffect,
  useState,
} from "react";

import { get, initApi } from "./api/client";
import { ConfirmHost } from "./components/ui/ConfirmDialog";
import { ProviderSwitcherHost } from "./components/ui/ProviderSwitcher";
import { ToastContainer } from "./components/ui/Toast";
import type { WikiTab } from "./components/wiki/WikiTabs";
import WikiTabs from "./components/wiki/WikiTabs";
import { AgentWorkbench } from "./components/workbench/AgentWorkbench";
import { useBrokerEvents } from "./hooks/useBrokerEvents";
import { useHashRouter } from "./hooks/useHashRouter";
import { useKeyboardShortcuts } from "./hooks/useKeyboardShortcuts";
import { isDMChannel, useAppStore } from "./stores/app";
import "./styles/shadcn.css";
import "./styles/global.css";
import "./styles/layout.css";
import "./styles/messages.css";
import "./styles/agents.css";
import "./styles/search.css";
import "./styles/wiki-shell.css";
import "./styles/kbd.css";
import "./styles/console.css";
import "@xterm/xterm/css/xterm.css";

const ArtifactsApp = lazy(() =>
  import("./components/apps/ArtifactsApp").then((m) => ({
    default: m.ArtifactsApp,
  })),
);
const CalendarApp = lazy(() =>
  import("./components/apps/CalendarApp").then((m) => ({
    default: m.CalendarApp,
  })),
);
const ConsoleApp = lazy(() =>
  import("./components/apps/ConsoleApp").then((m) => ({
    default: m.ConsoleApp,
  })),
);
const GraphApp = lazy(() =>
  import("./components/apps/GraphApp").then((m) => ({ default: m.GraphApp })),
);
const HealthCheckApp = lazy(() =>
  import("./components/apps/HealthCheckApp").then((m) => ({
    default: m.HealthCheckApp,
  })),
);
const PoliciesApp = lazy(() =>
  import("./components/apps/PoliciesApp").then((m) => ({
    default: m.PoliciesApp,
  })),
);
const ReceiptsApp = lazy(() =>
  import("./components/apps/ReceiptsApp").then((m) => ({
    default: m.ReceiptsApp,
  })),
);
const RequestsApp = lazy(() =>
  import("./components/apps/RequestsApp").then((m) => ({
    default: m.RequestsApp,
  })),
);
const SettingsApp = lazy(() =>
  import("./components/apps/SettingsApp").then((m) => ({
    default: m.SettingsApp,
  })),
);
const SkillsApp = lazy(() =>
  import("./components/apps/SkillsApp").then((m) => ({ default: m.SkillsApp })),
);
const TasksApp = lazy(() =>
  import("./components/apps/TasksApp").then((m) => ({ default: m.TasksApp })),
);
const ThreadsApp = lazy(() =>
  import("./components/apps/ThreadsApp").then((m) => ({
    default: m.ThreadsApp,
  })),
);
const TelegramConnectHost = lazy(() =>
  import("./components/integrations/TelegramConnectModal").then((m) => ({
    default: m.TelegramConnectHost,
  })),
);
const Shell = lazy(() =>
  import("./components/layout/Shell").then((m) => ({ default: m.Shell })),
);
const UpgradeBanner = lazy(() =>
  import("./components/layout/UpgradeBanner").then((m) => ({
    default: m.UpgradeBanner,
  })),
);
const Composer = lazy(() =>
  import("./components/messages/Composer").then((m) => ({
    default: m.Composer,
  })),
);
const DMView = lazy(() =>
  import("./components/messages/DMView").then((m) => ({ default: m.DMView })),
);
const InterviewBar = lazy(() =>
  import("./components/messages/InterviewBar").then((m) => ({
    default: m.InterviewBar,
  })),
);
const MessageFeed = lazy(() =>
  import("./components/messages/MessageFeed").then((m) => ({
    default: m.MessageFeed,
  })),
);
const TypingIndicator = lazy(() =>
  import("./components/messages/TypingIndicator").then((m) => ({
    default: m.TypingIndicator,
  })),
);
const Notebook = lazy(() => import("./components/notebook/Notebook"));
const SplashScreen = lazy(() =>
  import("./components/onboarding/SplashScreen").then((m) => ({
    default: m.SplashScreen,
  })),
);
const Wizard = lazy(() =>
  import("./components/onboarding/Wizard").then((m) => ({ default: m.Wizard })),
);
const ReviewQueueKanban = lazy(
  () => import("./components/review/ReviewQueueKanban"),
);
const CitedAnswer = lazy(() => import("./components/wiki/CitedAnswer"));
const Wiki = lazy(() => import("./components/wiki/Wiki"));

function PanelFallback() {
  return (
    <div className="app-panel-loading" role="status" aria-live="polite">
      Loading...
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

class NonCriticalBoundary extends Component<
  { children: ReactNode },
  { hasError: boolean }
> {
  state = { hasError: false };

  static getDerivedStateFromError(): { hasError: boolean } {
    return { hasError: true };
  }

  componentDidCatch(error: Error, info: { componentStack?: string | null }) {
    // eslint-disable-next-line no-console
    console.error("[WUPHF NonCriticalBoundary]", error, info);
  }

  render() {
    return this.state.hasError ? null : this.props.children;
  }
}

// ── Routed main content ─────────────────────────────────────────

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
  const workbenchAgentSlug = useAppStore((s) => s.workbenchAgentSlug);
  const workbenchTaskId = useAppStore((s) => s.workbenchTaskId);
  const setWorkbenchRoute = useAppStore((s) => s.setWorkbenchRoute);
  // Pam's onActionDone bumps this; Wiki re-fetches article + history when
  // the prop changes. Lifted up here because Pam lives inside the tab bar
  // (so her desk can rest on the divider line).
  const [articleRefreshNonce, setArticleRefreshNonce] = useState(0);

  if (!currentApp && isDMChannel(currentChannel, channelMeta)) {
    return (
      <Suspense fallback={<PanelFallback />}>
        <DMView />
      </Suspense>
    );
  }

  // Wiki, Notebooks, and Reviews share one app shell with a tab bar on top.
  // The surfaces underneath keep their own design systems (.wiki-surface vs
  // .notebook-surface) but the parent chrome is unified.
  if (currentApp === "wiki-lookup") {
    return (
      <div className="wiki-shell">
        <WikiTabs
          current="wiki"
          onSelect={(tab) =>
            selectWikiShellTab(tab, setCurrentApp, setNotebookRoute)
          }
        />
        <div className="wiki-shell-body">
          <Suspense fallback={<PanelFallback />}>
            <CitedAnswer query={wikiLookupQuery || ""} />
          </Suspense>
        </div>
      </div>
    );
  }

  if (isWikiShellApp(currentApp)) {
    // Pam only belongs on the wiki surface (notebooks + reviews are
    // separate contexts). When we're not on the wiki tab, articlePath is
    // null so she renders as disabled scenery without actionable state.
    const pamArticlePath = currentApp === "wiki" ? (wikiPath ?? null) : null;

    return (
      <div className="wiki-shell">
        <WikiTabs
          current={currentApp}
          onSelect={(tab) =>
            selectWikiShellTab(tab, setCurrentApp, setNotebookRoute)
          }
          pamArticlePath={pamArticlePath}
          onPamActionDone={() => setArticleRefreshNonce((n) => n + 1)}
        />
        <div className="wiki-shell-body">
          <Suspense fallback={<PanelFallback />}>
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
          </Suspense>
        </div>
      </div>
    );
  }

  if (currentApp === "workbench") {
    return (
      <WorkbenchPanel
        agentSlug={workbenchAgentSlug}
        taskId={workbenchTaskId}
        onSelectionChange={setWorkbenchRoute}
        onClose={() => setCurrentApp(workbenchTaskId ? "tasks" : null)}
      />
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
          <Suspense fallback={<PanelFallback />}>
            <Panel />
          </Suspense>
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
      <Suspense fallback={<PanelFallback />}>
        <MessageFeed />
        <TypingIndicator />
        <InterviewBar />
      </Suspense>
      <Suspense fallback={null}>
        <Composer />
      </Suspense>
    </>
  );
}

function WorkbenchPanel({
  agentSlug,
  taskId,
  onSelectionChange,
  onClose,
}: {
  agentSlug: string | null;
  taskId: string | null;
  onSelectionChange: (agentSlug: string | null, taskId: string | null) => void;
  onClose: () => void;
}) {
  return (
    <div className="app-panel active">
      <AgentWorkbench
        agentSlug={agentSlug}
        taskId={taskId}
        onSelectionChange={onSelectionChange}
        onClose={onClose}
      />
    </div>
  );
}

function isWikiShellApp(app: string | null): app is WikiTab {
  return app === "wiki" || app === "notebooks" || app === "reviews";
}

function selectWikiShellTab(
  tab: WikiTab,
  setCurrentApp: (app: string | null) => void,
  setNotebookRoute: (
    agentSlug: string | null,
    entrySlug: string | null,
  ) => void,
) {
  if (tab === "notebooks") {
    setNotebookRoute(null, null);
  }
  setCurrentApp(tab === "reviews" ? "reviews" : tab);
}

// ── App root ────────────────────────────────────────────────────
//
// Critical rules (violations caused the blank-page regression):
// 1. ALL hooks are called unconditionally at the top of App(). No early
//    returns before hook calls.
// 2. initApi() runs in an effect, but we render the shell immediately so
//    the user sees something even while init is pending.
// 3. ErrorBoundary wraps the whole tree so render errors are visible.

export default function App() {
  // --- All hooks first, in a fixed order, every render ---
  const [apiReady, setApiReady] = useState(false);
  const [showSplash, setShowSplash] = useState(false);
  const theme = useAppStore((s) => s.theme);
  const onboardingComplete = useAppStore((s) => s.onboardingComplete);
  const setBrokerConnected = useAppStore((s) => s.setBrokerConnected);
  const setOnboardingComplete = useAppStore((s) => s.setOnboardingComplete);

  useKeyboardShortcuts();
  useHashRouter();
  useBrokerEvents(apiReady);

  // Load theme CSS when theme changes
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

  // Init API and determine onboarding state.
  // Source of truth: GET /onboarding/state.onboarded (backed by ~/.wuphf/onboarded.json).
  // Broker health / default agents must not skip the wizard — the broker seeds 7
  // default agents on every boot, so a health-based check was making the wizard
  // permanently unreachable for fresh installs.
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

  // --- Render (no hooks past this point) ---

  let body: ReactNode;
  if (!apiReady) {
    // The static skeleton in index.html already covers this case, but
    // render a matching React fallback so nothing flashes.
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
    body = (
      <Suspense fallback={<PanelFallback />}>
        <SplashScreen onDone={() => setShowSplash(false)} />
      </Suspense>
    );
  } else if (!onboardingComplete) {
    body = (
      <Suspense fallback={<PanelFallback />}>
        <Wizard
          onComplete={() => {
            setShowSplash(true);
          }}
        />
      </Suspense>
    );
  } else {
    body = (
      <Suspense fallback={<PanelFallback />}>
        <Shell>
          <MainContent />
        </Shell>
      </Suspense>
    );
  }

  return (
    <ErrorBoundary>
      <NonCriticalBoundary>
        <Suspense fallback={null}>
          <UpgradeBanner />
        </Suspense>
      </NonCriticalBoundary>
      {body}
      <ToastContainer />
      <ConfirmHost />
      <ProviderSwitcherHost />
      <NonCriticalBoundary>
        <Suspense fallback={null}>
          <TelegramConnectHost />
        </Suspense>
      </NonCriticalBoundary>
    </ErrorBoundary>
  );
}
