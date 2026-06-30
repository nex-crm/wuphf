// Apps — the list surface. Real built apps (from the shipped app-builder
// backend) lead; the AI-suggested tools below are still mock aspiration. A live
// hero app, then the rest, then suggestions. Selecting a real app opens
// OperatorAppDetail; selecting a suggestion opens the mock detail.

import { ArrowRight, PhoneCall, Plus, Trash2 } from "lucide-react";

import type { CustomApp } from "../../api/apps";
import {
  appBuildState,
  useDeleteApp,
  useOperatorApps,
} from "../apps/useOperatorApps";
import {
  Eyebrow,
  SurfaceHeader,
  sigil,
  ToolStatusBadge,
} from "../components/primitives";
import { type InternalTool, TOOLS } from "../mock/data";

interface InternalToolsSurfaceProps {
  onOpen: (toolId: string) => void;
  onStartCall: () => void;
  onBuild: () => void;
}

export function InternalToolsSurface({
  onOpen,
  onStartCall,
  onBuild,
}: InternalToolsSurfaceProps) {
  const appsQuery = useOperatorApps();
  const deleteApp = useDeleteApp();
  const apps = appsQuery.data ?? [];
  const ready = apps.filter((a) => appBuildState(a) === "ready");
  const buildingApps = apps.filter((a) => appBuildState(a) === "building");
  const failedApps = apps.filter((a) => appBuildState(a) === "failed");
  const hero = ready[0];
  const rest = ready.slice(1);

  const suggested = TOOLS.filter((t) => t.status === "suggested");

  return (
    <div className="opr-surface-wide">
      <SurfaceHeader
        eyebrow="Apps"
        title="Your apps"
        lede="Each app is a small tool your team runs — a dashboard, a form, a workflow — built by describing it in chat, or talked through on a call."
        actions={
          <div className="opr-header-actions">
            <button
              type="button"
              className="opr-btn opr-btn-primary"
              onClick={onBuild}
            >
              <Plus size={14} strokeWidth={1.9} aria-hidden={true} />
              Build an app
            </button>
            <button type="button" className="opr-btn" onClick={onStartCall}>
              <PhoneCall size={14} strokeWidth={1.9} aria-hidden={true} />
              Demo workflow to Nex
            </button>
          </div>
        }
      />

      {hero ? (
        <HeroAppCard app={hero} onOpen={() => onOpen(hero.id)} />
      ) : appsQuery.isLoading ? (
        <p className="opr-scoped-note">Loading your apps…</p>
      ) : (
        <div className="opr-empty">
          <span className="opr-empty-glyph" aria-hidden={true}>
            ◧
          </span>
          <div className="opr-empty-title">No apps yet</div>
          <div className="opr-empty-hint">
            Describe the tool your team needs and your AI builds it — a
            dashboard, a form, a workflow. It appears here the moment it is
            ready.
          </div>
          <div className="opr-empty-actions">
            <button
              type="button"
              className="opr-btn opr-btn-primary opr-btn-sm"
              onClick={onBuild}
            >
              <Plus size={13} strokeWidth={1.9} aria-hidden={true} />
              Build your first app
            </button>
          </div>
        </div>
      )}

      {rest.length > 0 || buildingApps.length > 0 || failedApps.length > 0 ? (
        <>
          <div className="opr-section-label">
            <Eyebrow>All apps</Eyebrow>
            <div className="opr-section-rule" />
          </div>
          <div className="opr-grid">
            {rest.map((app) => (
              <AppRow key={app.id} app={app} onOpen={() => onOpen(app.id)} />
            ))}
            {buildingApps.map((app) => (
              <BuildingRow key={app.id} app={app} />
            ))}
            {failedApps.map((app) => (
              <FailedRow
                key={app.id}
                app={app}
                onRemove={() => deleteApp.mutate(app.id)}
                removing={deleteApp.isPending}
              />
            ))}
          </div>
        </>
      ) : null}

      {suggested.length > 0 ? (
        <>
          <div className="opr-section-label">
            <Eyebrow>Suggested by your AI</Eyebrow>
            <div className="opr-section-rule" />
          </div>
          <div className="opr-grid">
            {suggested.map((t) => (
              <SuggestedRow key={t.id} tool={t} onOpen={() => onOpen(t.id)} />
            ))}
          </div>
        </>
      ) : null}
    </div>
  );
}

function appGlyph(app: CustomApp): string {
  return app.icon?.trim() ? app.icon : sigil(app.name);
}

function HeroAppCard({ app, onOpen }: { app: CustomApp; onOpen: () => void }) {
  return (
    <div
      className="opr-card opr-card-hero"
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      style={{ cursor: "pointer", marginBottom: "var(--space-2)" }}
    >
      <div className="opr-detail-head" style={{ marginBottom: 0 }}>
        <span className="opr-tool-emoji" aria-hidden={true}>
          {appGlyph(app)}
        </span>
        <div className="opr-detail-titles">
          <div className="opr-tool-name">{app.name}</div>
          {app.summary ? (
            <p className="opr-tool-summary">{app.summary}</p>
          ) : null}
          <div className="opr-tool-meta">
            <span className="opr-pill opr-pill-muted">
              <span className="opr-led opr-led-live" />
              Live
            </span>
            <span className="opr-meta-dot">v{app.version}</span>
          </div>
        </div>
        <span className="opr-btn opr-btn-sm" aria-hidden={true}>
          Open
          <ArrowRight size={13} strokeWidth={1.9} />
        </span>
      </div>
    </div>
  );
}

function AppRow({ app, onOpen }: { app: CustomApp; onOpen: () => void }) {
  return (
    <div
      className="opr-tool-row"
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
    >
      <span className="opr-tool-emoji" aria-hidden={true}>
        {appGlyph(app)}
      </span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="opr-tool-name" style={{ fontSize: "var(--text-md)" }}>
          {app.name}
        </div>
        {app.summary ? <p className="opr-tool-summary">{app.summary}</p> : null}
      </div>
      <span className="opr-pill opr-pill-muted">
        <span className="opr-led opr-led-live" />v{app.version}
      </span>
    </div>
  );
}

function BuildingRow({ app }: { app: CustomApp }) {
  return (
    <div className="opr-tool-row" aria-disabled={true} style={{ opacity: 0.7 }}>
      <span className="opr-tool-emoji" aria-hidden={true}>
        {appGlyph(app)}
      </span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="opr-tool-name" style={{ fontSize: "var(--text-md)" }}>
          {app.name}
        </div>
        <p className="opr-tool-summary">Building…</p>
      </div>
      <span className="opr-pill opr-pill-muted">
        <span className="opr-led opr-led-draft" />
        Building
      </span>
    </div>
  );
}

function FailedRow({
  app,
  onRemove,
  removing,
}: {
  app: CustomApp;
  onRemove: () => void;
  removing: boolean;
}) {
  return (
    <div className="opr-tool-row opr-tool-row-failed">
      <span className="opr-tool-emoji" aria-hidden={true}>
        {appGlyph(app)}
      </span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="opr-tool-name" style={{ fontSize: "var(--text-md)" }}>
          {app.name}
        </div>
        <p className="opr-tool-summary">
          Build failed — it stalled before publishing.
        </p>
      </div>
      <span className="opr-pill opr-pill-bad">
        <span className="opr-led opr-led-bad" />
        Failed
      </span>
      <button
        type="button"
        className="opr-icon-btn"
        onClick={onRemove}
        disabled={removing}
        aria-label={`Remove ${app.name}`}
        title="Remove"
      >
        <Trash2 size={15} strokeWidth={1.9} aria-hidden={true} />
      </button>
    </div>
  );
}

function SuggestedRow({
  tool,
  onOpen,
}: {
  tool: InternalTool;
  onOpen: () => void;
}) {
  return (
    <div
      className="opr-tool-row"
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
    >
      <span className="opr-tool-emoji" aria-hidden={true}>
        {sigil(tool.name)}
      </span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="opr-tool-name" style={{ fontSize: "var(--text-md)" }}>
          {tool.name}
        </div>
        <p className="opr-tool-summary">{tool.summary}</p>
      </div>
      <ToolStatusBadge status={tool.status} />
    </div>
  );
}
