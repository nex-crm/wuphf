// OperatorAppDetail — the detail view for a REAL built app (id `app_…`). It
// keeps the operator App's tab model (UI / Workflow / Data / Integrations /
// Knowledge); the UI tab renders the live, persisted app inside the shipped
// hardened sandbox (CustomAppFrame + Bridge v2). The other tabs are honest
// empty states for this slice — the workflow/data/knowledge wiring lands next.

import { useState } from "react";
import type { UseQueryResult } from "@tanstack/react-query";
import { ArrowLeft, Sparkles } from "lucide-react";

import type { CustomAppDetail } from "../../api/apps";
import { CustomAppFrame } from "../../components/apps/CustomAppFrame";
import { useOperatorApp } from "../apps/useOperatorApps";
import { EmptyState } from "../components/EmptyState";
import { Eyebrow, type TabDef, Tabs } from "../components/primitives";
import { AppDeliverySchedule } from "./AppDeliverySchedule";
import { ToolIntegrations } from "./ToolIntegrations";

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
  /** Open the app builder in edit mode against this app. */
  onAskAI: (id: string, name: string) => void;
}

export function OperatorAppDetail({
  appId,
  onBack,
  onAskAI,
}: OperatorAppDetailProps) {
  const [tab, setTab] = useState<AppTab>("ui");
  const query = useOperatorApp(appId);

  const detail = query.data;
  const app = detail?.app;
  const building = !app || app.status === "building" || !detail?.html;

  return (
    <div className="opr-detail-wrap">
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
              <span className="opr-pill opr-pill-muted">
                <span
                  className={`opr-led ${
                    building ? "opr-led-draft" : "opr-led-live"
                  }`}
                />
                {building ? "Building" : "Live"}
              </span>
              {app ? (
                <span className="opr-meta-dot">v{app.version}</span>
              ) : null}
            </div>
          </div>
          {app && !building ? (
            <div className="opr-detail-actions">
              <button
                type="button"
                className="opr-btn opr-btn-sm"
                onClick={() => onAskAI(app.id, app.name)}
              >
                <Sparkles size={13} strokeWidth={1.9} aria-hidden={true} />
                Ask AI
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
          <TabBody tab={tab} query={query} />
        </div>
      </div>
    </div>
  );
}

function TabBody({
  tab,
  query,
}: {
  tab: AppTab;
  query: UseQueryResult<CustomAppDetail>;
}) {
  const app = query.data?.app;
  const ready = app && app.status !== "building" && query.data?.html;
  switch (tab) {
    case "ui":
      return <UiTab query={query} />;
    case "workflow":
      return ready ? (
        <AppDeliverySchedule appName={app.name} />
      ) : (
        <EmptyState
          glyph="⌥"
          title="No automation yet"
          hint="Schedule this app to run and post to Slack once it has finished building."
        />
      );
    case "data":
      return (
        <EmptyState
          glyph="▦"
          title="No data table yet"
          hint="Apps that capture or store rows show them here as a typed table you own. This app does not define one yet."
        />
      );
    case "integrations":
      return <ToolIntegrations usedNames={[]} />;
    case "knowledge":
      return (
        <div className="opr-tool-scoped">
          <Eyebrow>Workspace knowledge</Eyebrow>
          <p className="opr-scoped-note">
            Knowledge is owned by your workspace and inherited by every app.
            Connect it here in a later step.
          </p>
        </div>
      );
    default:
      return null;
  }
}

function UiTab({ query }: { query: UseQueryResult<CustomAppDetail> }) {
  const detail = query.data;
  const app = detail?.app;
  const ready = app && app.status !== "building" && detail?.html;
  if (ready) {
    return (
      <div className="opr-app-frame">
        <CustomAppFrame appId={app.id} title={app.name} html={detail.html} />
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
