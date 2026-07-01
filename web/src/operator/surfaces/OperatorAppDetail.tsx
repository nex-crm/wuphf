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
import {
  appBuildState,
  useDeleteApp,
  useOperatorApp,
} from "../apps/useOperatorApps";
import { EmptyState } from "../components/EmptyState";
import { type TabDef, Tabs } from "../components/primitives";
import { AppBuilderChat } from "./AppBuilderChat";
import { AppDataTab } from "./AppDataTab";
import { AppWorkflowTab } from "./AppWorkflowTab";
import { KnowledgeSurface } from "./KnowledgeSurface";
import { ToolIntegrations } from "./ToolIntegrations";

type PanelSize = "dock" | "wide" | "modal";

type AppTab = "ui" | "workflow" | "data" | "integrations" | "knowledge";

const TABS: readonly TabDef<AppTab>[] = [
  { id: "ui", label: "UI" },
  { id: "workflow", label: "Workflow" },
  { id: "data", label: "Data" },
  { id: "integrations", label: "Integrations" },
  { id: "knowledge", label: "Knowledge" },
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
  const [tab, setTab] = useState<AppTab>("ui");
  const [chatOpen, setChatOpen] = useState(false);
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
      window.setTimeout(() => setTab("ui"), 8000),
    ];
    return () => timers.forEach((t) => window.clearTimeout(t));
  }, [buildWalk, ready]);

  function removeAndBack() {
    if (!app) return;
    remove.mutate(app.id, { onSuccess: onBack });
  }

  return (
    <div
      className={`opr-detail-wrap${
        chatOpen && panelSize !== "modal" ? ` is-chat-${panelSize}` : ""
      }`}
    >
      <div className="opr-surface-wide opr-app-detail">
        <button type="button" className="opr-back" onClick={onBack}>
          <ArrowLeft size={13} strokeWidth={1.9} aria-hidden={true} />
          All apps
        </button>

        <div className="opr-detail-head">
          <span className="opr-tool-emoji" aria-hidden={true}>
            {app?.icon || "🧩"}
          </span>
          <div className="opr-detail-titles">
            <div className="opr-detail-name">{app?.name ?? "Loading app…"}</div>
            {app?.summary ? (
              <p className="opr-tool-summary">{app.summary}</p>
            ) : null}
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
                Ask AI
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

        <Tabs tabs={TABS} active={tab} onSelect={setTab} />

        <div
          role="tabpanel"
          id={`opr-panel-${tab}`}
          aria-labelledby={`opr-tab-${tab}`}
        >
          {/* The UI tab's app frame stays MOUNTED across tab switches — hidden,
              not unmounted, when another tab is active — so returning to the UI
              tab does NOT reload the iframe and re-run the app (re-fetching
              Gmail, re-summarizing, re-rendering) every single time. The other
              tabs mount only while active. */}
          <div style={tab === "ui" ? undefined : { display: "none" }}>
            <UiTab
              query={query}
              failed={failed}
              onRemove={removeAndBack}
              removing={remove.isPending}
            />
          </div>
          {tab !== "ui" ? (
            <TabBody
              tab={tab}
              query={query}
              failed={failed}
              onRemove={removeAndBack}
              removing={remove.isPending}
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
          onFinish={() => {
            // The edit republished a new version; refetch so the detail shows it.
            void query.refetch();
          }}
        />
      ) : null}
    </div>
  );
}

// ── Ask AI dock: floating bubble + right-side docked drawer (dock/wide/modal) ──

function AskAiDock({
  app,
  open,
  size,
  onOpenChange,
  onSizeChange,
  onFinish,
}: {
  app: CustomApp;
  open: boolean;
  size: PanelSize;
  onOpenChange: (open: boolean) => void;
  onSizeChange: (next: (s: PanelSize) => PanelSize) => void;
  onFinish: () => void;
}) {
  if (!open) {
    return (
      <button
        type="button"
        className="opr-ask-fab"
        onClick={() => onOpenChange(true)}
        aria-label={`Ask AI about ${app.name}`}
      >
        <Sparkles size={16} strokeWidth={2} aria-hidden={true} />
        Ask AI
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
        aria-label={`Ask AI about ${app.name}`}
      >
        <div className="opr-ask-bar">
          <span className="opr-ask-bar-title">
            <Sparkles size={13} strokeWidth={2} aria-hidden={true} />
            Ask AI · {app.name}
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
          <AppBuilderChat
            panelMode={true}
            editApp={{ id: app.id, name: app.name }}
            onClose={() => onOpenChange(false)}
            onFinish={onFinish}
          />
        </div>
      </aside>
    </>
  );
}

function TabBody({
  tab,
  query,
  failed,
  onRemove,
  removing,
}: {
  tab: AppTab;
  query: UseQueryResult<CustomAppDetail>;
  failed: boolean;
  onRemove: () => void;
  removing: boolean;
}) {
  const app = query.data?.app;
  const ready =
    app && appBuildState(app) === "ready" && Boolean(query.data?.html);
  switch (tab) {
    case "ui":
      return (
        <UiTab
          query={query}
          failed={failed}
          onRemove={onRemove}
          removing={removing}
        />
      );
    case "workflow":
      return ready ? (
        <AppWorkflowTab appId={app.id} />
      ) : (
        <EmptyState
          glyph="⌥"
          title="No automation yet"
          hint="Once this app finishes building, compile it into a deterministic workflow and run it on a schedule."
        />
      );
    case "data":
      return app ? (
        <AppDataTab appId={app.id} />
      ) : (
        <EmptyState
          glyph="▦"
          title="No data yet"
          hint="The data this app reads and writes appears here once it has finished building."
        />
      );
    case "integrations":
      return <ToolIntegrations usedNames={[]} />;
    case "knowledge":
      // The gbrain-backed, Wikipedia-style reader with cited claims and an
      // Explain-why affordance — the same rich Knowledge surface the workspace
      // uses, scoped into the app tab. (Replaces the plain wiki-catalog reader.)
      return <KnowledgeSurface />;
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
          This app stalled before it published a version — it is not building
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
            Remove app
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
        {query.isError ? "Could not load this app" : "Building your app…"}
      </div>
      <div className="opr-empty-hint">
        {query.isError
          ? "The workspace may be offline. It will retry automatically."
          : "The live app appears here the moment the first version publishes."}
      </div>
    </div>
  );
}
