// OperatorAppDetail — the detail view for a REAL built app (id `app_…`). It
// keeps the operator App's tab model (UI / Workflow / Data / Integrations /
// Knowledge); the UI tab renders the live, persisted app inside the shipped
// hardened sandbox (CustomAppFrame + Bridge v2). The other tabs are honest
// empty states for this slice — the workflow/data/knowledge wiring lands next.

import { useEffect, useRef, useState } from "react";
import type { UseQueryResult } from "@tanstack/react-query";
import {
  ArrowLeft,
  ChevronsLeft,
  ChevronsRight,
  Maximize2,
  Minimize2,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";

import type { CustomApp, CustomAppDetail } from "../../api/apps";
import { AppLivePreview } from "../../components/apps/AppLivePreview";
import { CustomAppFrame } from "../../components/apps/CustomAppFrame";
import { AgentName } from "../agents/AgentName";
import { AgentPurpose } from "../agents/AgentPurpose";
import { AgentSessions } from "../agents/AgentSessions";
import {
  appBuildState,
  useDeleteApp,
  useOperatorApp,
} from "../apps/useOperatorApps";
import { ArtifactsTab } from "../artifacts/ArtifactsTab";
import type { Artifact } from "../artifacts/artifacts";
import { EmptyState } from "../components/EmptyState";
import { type TabDef, Tabs } from "../components/primitives";
import { RoutinesTab } from "../routines/RoutinesTab";
import { ToolsProvider } from "../tools/toolsContext";
import { AppDataTab } from "./AppDataTab";
import { AppToolsTab } from "./AppToolsTab";
import { KnowledgeSurface } from "./KnowledgeSurface";
import { ToolIntegrations } from "./ToolIntegrations";

type PanelSize = "dock" | "wide" | "modal";

type AppTab =
  | "artifacts"
  | "workflow"
  | "tools"
  | "data"
  | "integrations"
  | "knowledge";

const TABS: readonly TabDef<AppTab>[] = [
  { id: "artifacts", label: "Artifacts" },
  { id: "workflow", label: "Routines" },
  // Tools: the callable tools Nex builds from taught workflows; the app's chat
  // calls them. Additive — the Workflow tab is unchanged.
  { id: "tools", label: "Tools" },
  { id: "data", label: "Data" },
  { id: "knowledge", label: "Knowledge" },
  { id: "integrations", label: "Integrations" },
];

interface OperatorAppDetailProps {
  appId: string;
  onBack: () => void;
  /**
   * Build mode: once the app publishes, walk the tabs UI → Workflow → Data →
   * Knowledge so the operator sees each part get hooked up, then settle back on
   * the UI. Used by the build experience; a manual tab click cancels the walk.
   */
  buildWalk?: boolean;
}

export function OperatorAppDetail({
  appId,
  onBack,
  buildWalk,
}: OperatorAppDetailProps) {
  const [tab, setTab] = useState<AppTab>("artifacts");
  const [chatOpen, setChatOpen] = useState(false);
  // A routine's "Open its chat" jumps the Ask Agent dock to that session.
  const [requestedSession, setRequestedSession] = useState<string | null>(null);
  const [panelSize, setPanelSize] = useState<PanelSize>("dock");
  const query = useOperatorApp(appId);
  const remove = useDeleteApp();

  const detail = query.data;
  const app = detail?.app;
  const state = app ? appBuildState(app) : "building";
  const failed = state === "failed";
  const ready = state === "ready" && !!detail?.html;

  // Guided reveal: when the app finishes building, walk through the tabs once so
  // the operator sees the workflow, data, and knowledge get hooked up.
  const walkedRef = useRef(false);
  useEffect(() => {
    if (!(buildWalk && ready) || walkedRef.current) return;
    walkedRef.current = true;
    const timers = [
      window.setTimeout(() => setTab("workflow"), 900),
      window.setTimeout(() => setTab("data"), 3200),
      window.setTimeout(() => setTab("knowledge"), 5500),
      window.setTimeout(() => setTab("artifacts"), 8000),
    ];
    return () => timers.forEach((t) => window.clearTimeout(t));
  }, [buildWalk, ready]);

  function removeAndBack() {
    if (!app) return;
    remove.mutate(app.id, { onSuccess: onBack });
  }

  return (
    <ToolsProvider appName={app?.name ?? "This app"}>
      <div
        className={`opr-detail-wrap${
          chatOpen && panelSize !== "modal" ? ` is-chat-${panelSize}` : ""
        }`}
      >
        <div className="opr-surface-wide opr-app-detail">
          <button type="button" className="opr-back" onClick={onBack}>
            <ArrowLeft size={13} strokeWidth={1.9} aria-hidden={true} />
            All agents
          </button>

          <div className="opr-detail-head">
            <span className="opr-tool-emoji" aria-hidden={true}>
              {app?.icon || "🧩"}
            </span>
            <div className="opr-detail-titles">
              <div className="opr-detail-name">
                {app ? (
                  <AgentName id={app.id} fallback={app.name} />
                ) : (
                  "Loading agent…"
                )}
              </div>
              <div className="opr-tool-meta">
                <span
                  className={`opr-pill ${failed ? "opr-pill-bad" : "opr-pill-muted"}`}
                >
                  <span
                    className={`opr-led ${
                      failed
                        ? "opr-led-bad"
                        : ready
                          ? "opr-led-live"
                          : "opr-led-draft"
                    }`}
                  />
                  {failed ? "Failed" : ready ? "Live" : "Building"}
                </span>
                {app ? (
                  <span className="opr-meta-dot">v{app.version}</span>
                ) : null}
              </div>
            </div>
            {ready ? (
              <div className="opr-detail-actions">
                <button
                  type="button"
                  className="opr-btn opr-btn-sm"
                  onClick={() => setChatOpen(true)}
                >
                  <Sparkles size={13} strokeWidth={1.9} aria-hidden={true} />
                  Ask Agent
                </button>
              </div>
            ) : failed ? (
              <div className="opr-detail-actions">
                <button
                  type="button"
                  className="opr-btn opr-btn-sm"
                  onClick={removeAndBack}
                  disabled={remove.isPending}
                >
                  <Trash2 size={13} strokeWidth={1.9} aria-hidden={true} />
                  Remove
                </button>
              </div>
            ) : null}
          </div>

          <AgentPurpose summary={app?.summary} />

          <Tabs tabs={TABS} active={tab} onSelect={setTab} />

          <div
            role="tabpanel"
            id={`opr-panel-${tab}`}
            aria-labelledby={`opr-tab-${tab}`}
          >
            {/* The Artifacts tab (hosting the live app frame) stays MOUNTED
                across tab switches — hidden, not unmounted — so returning to it
                does NOT reload the iframe and re-run the app every time. The
                other tabs mount only while active. */}
            <div style={tab === "artifacts" ? undefined : { display: "none" }}>
              <ArtifactsTab
                agentName={app?.name ?? "This agent"}
                artifacts={[
                  {
                    id: "app",
                    type: "app",
                    title: app?.name ?? "App",
                    producedBy: "built by Nex",
                    at: app ? `v${app.version}` : "",
                  },
                ]}
                renderApp={() => (
                  <UiTab
                    query={query}
                    failed={failed}
                    onRemove={removeAndBack}
                    removing={remove.isPending}
                  />
                )}
              />
            </div>
            {tab !== "artifacts" ? (
              <TabBody
                tab={tab}
                query={query}
                onOpenRoutineSession={(sessionId) => {
                  setRequestedSession(sessionId);
                  setChatOpen(true);
                }}
              />
            ) : null}
          </div>
        </div>

        {/* Ask AI — floating bubble + docked drawer, openable from any tab. During
          the build experience the build chat is already docked, so suppress it. */}
        {app && ready && !buildWalk ? (
          <AskAiDock
            app={app}
            open={chatOpen}
            size={panelSize}
            onOpenChange={setChatOpen}
            onSizeChange={setPanelSize}
            requestedSessionId={requestedSession}
          />
        ) : null}
      </div>
    </ToolsProvider>
  );
}

// ── Ask AI dock: floating bubble + right-side docked drawer (dock/wide/modal) ──

function AskAiDock({
  app,
  open,
  size,
  onOpenChange,
  onSizeChange,
  requestedSessionId,
}: {
  app: CustomApp;
  open: boolean;
  size: PanelSize;
  onOpenChange: (open: boolean) => void;
  onSizeChange: (next: (s: PanelSize) => PanelSize) => void;
  requestedSessionId?: string | null;
}) {
  if (!open) {
    return (
      <button
        type="button"
        className="opr-ask-fab"
        onClick={() => onOpenChange(true)}
        aria-label={`Ask Agent about ${app.name}`}
      >
        <Sparkles size={16} strokeWidth={2} aria-hidden={true} />
        Ask Agent
      </button>
    );
  }
  return (
    <>
      {size === "modal" ? (
        <button
          type="button"
          className="opr-ask-backdrop"
          aria-label="Close chat"
          onClick={() => onOpenChange(false)}
        />
      ) : null}
      <aside
        className={`opr-ask-panel is-${size}`}
        aria-label={`Ask Agent about ${app.name}`}
      >
        <div className="opr-ask-bar">
          <span className="opr-ask-bar-title">
            <Sparkles size={13} strokeWidth={2} aria-hidden={true} />
            Ask Agent · {app.name}
          </span>
          <div className="opr-ask-bar-controls">
            <button
              type="button"
              className="opr-icon-btn"
              onClick={() =>
                onSizeChange((s) => (s === "wide" ? "dock" : "wide"))
              }
              aria-label={size === "wide" ? "Narrow panel" : "Widen panel"}
              title={size === "wide" ? "Narrow" : "Widen"}
            >
              {size === "wide" ? (
                <ChevronsRight size={15} strokeWidth={1.9} aria-hidden={true} />
              ) : (
                <ChevronsLeft size={15} strokeWidth={1.9} aria-hidden={true} />
              )}
            </button>
            <button
              type="button"
              className="opr-icon-btn"
              onClick={() =>
                onSizeChange((s) => (s === "modal" ? "dock" : "modal"))
              }
              aria-label={size === "modal" ? "Exit full screen" : "Full screen"}
              title={size === "modal" ? "Exit full screen" : "Full screen"}
            >
              {size === "modal" ? (
                <Minimize2 size={14} strokeWidth={1.9} aria-hidden={true} />
              ) : (
                <Maximize2 size={14} strokeWidth={1.9} aria-hidden={true} />
              )}
            </button>
            <button
              type="button"
              className="opr-icon-btn"
              onClick={() => onOpenChange(false)}
              aria-label="Close chat"
              title="Close"
            >
              <X size={15} strokeWidth={1.9} aria-hidden={true} />
            </button>
          </div>
        </div>
        <div className="opr-ask-body">
          <AgentSessions
            agentName={app.name}
            requestedSessionId={requestedSessionId}
          />
        </div>
      </aside>
    </>
  );
}

function TabBody({
  tab,
  query,
  onOpenRoutineSession,
}: {
  tab: AppTab;
  query: UseQueryResult<CustomAppDetail>;
  onOpenRoutineSession?: (sessionId: string) => void;
}) {
  const app = query.data?.app;
  switch (tab) {
    case "workflow":
      return (
        <RoutinesTab
          agentName={app?.name ?? "This agent"}
          onOpenSession={(sessionId) => onOpenRoutineSession?.(sessionId)}
        />
      );
    case "tools":
      return <AppToolsTab appName={app?.name ?? "This app"} />;
    case "data":
      return app ? (
        <AppDataTab appId={app.id} />
      ) : (
        <EmptyState
          glyph="▦"
          title="No data yet"
          hint="The data this agent reads and writes appears here once it has finished building."
        />
      );
    case "integrations":
      return <ToolIntegrations usedNames={[]} />;
    case "knowledge":
      // The gbrain-backed, Wikipedia-style reader with cited claims — backed by
      // the agent's REAL synthesized pages (grounded in its own artifacts).
      return app ? (
        <KnowledgeSurface appId={app.id} />
      ) : (
        <EmptyState
          glyph="📖"
          title="No knowledge yet"
          hint="Your AI writes cited pages about this agent once it has finished building."
        />
      );
    default:
      return null;
  }
}

function UiTab({
  query,
  failed,
  onRemove,
  removing,
}: {
  query: UseQueryResult<CustomAppDetail>;
  failed: boolean;
  onRemove: () => void;
  removing: boolean;
}) {
  const detail = query.data;
  const app = detail?.app;
  const ready = app && appBuildState(app) === "ready" && detail?.html;
  if (ready) {
    return (
      <div className="opr-app-frame">
        <CustomAppFrame appId={app.id} title={app.name} html={detail.html} />
      </div>
    );
  }
  // Still building, but the app exists: show the LIVE dev-server preview so the
  // UI builds in front of you (HMR reflects the agent's edits) instead of a
  // static placeholder. Reuses the shipped AppLivePreview.
  if (app && !failed) {
    return (
      <div className="opr-app-frame">
        <AppLivePreview appId={app.id} title={app.name} />
      </div>
    );
  }
  if (failed) {
    return (
      <div className="opr-app-building opr-app-failed" role="status">
        <span className="opr-empty-glyph" aria-hidden={true}>
          ⚠
        </span>
        <div className="opr-empty-title">Build failed</div>
        <div className="opr-empty-hint">
          This agent stalled before it published a version — it is not building
          anymore. Remove it and rebuild, or describe it again.
        </div>
        <div className="opr-empty-actions">
          <button
            type="button"
            className="opr-btn opr-btn-primary opr-btn-sm"
            onClick={onRemove}
            disabled={removing}
          >
            <Trash2 size={13} strokeWidth={1.9} aria-hidden={true} />
            Remove agent
          </button>
        </div>
      </div>
    );
  }
  return (
    <div className="opr-app-building" role="status">
      <span className="opr-work-dots" aria-hidden={true}>
        <span />
        <span />
        <span />
      </span>
      <div className="opr-empty-title">
        {query.isError ? "Could not load this agent" : "Building your agent…"}
      </div>
      <div className="opr-empty-hint">
        {query.isError
          ? "The workspace may be offline. It will retry automatically."
          : "The live app appears here the moment the first version publishes."}
      </div>
    </div>
  );
}
