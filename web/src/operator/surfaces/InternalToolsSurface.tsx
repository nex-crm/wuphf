// Internal Tools — the list surface. A live "hero" tool, then drafts and
// AI-suggested tools. Selecting one opens InternalToolDetail (state in the
// shell). Mock data only.

import { ArrowRight, PhoneCall, Plus } from "lucide-react";

import {
  Eyebrow,
  sigil,
  SurfaceHeader,
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
  const live = TOOLS.filter(
    (t) => t.status === "enabled" || t.status === "disabled",
  );
  const drafts = TOOLS.filter((t) => t.status === "draft");
  const suggested = TOOLS.filter((t) => t.status === "suggested");

  return (
    <div className="opr-surface-wide">
      <SurfaceHeader
        eyebrow="Internal tools"
        title="Your tools"
        lede="Each tool watches for something, decides what to do, and acts, exactly the way you taught it. Build a new one by describing it in chat, or talk it through on a call."
        actions={
          <div className="opr-header-actions">
            <button
              type="button"
              className="opr-btn opr-btn-primary"
              onClick={onBuild}
            >
              <Plus size={14} strokeWidth={1.9} aria-hidden />
              Build a tool
            </button>
            <button
              type="button"
              className="opr-btn"
              onClick={onStartCall}
            >
              <PhoneCall size={14} strokeWidth={1.9} aria-hidden />
              Teach your workflow to Nex
            </button>
          </div>
        }
      />

      {live.map((t) => (
        <HeroToolCard key={t.id} tool={t} onOpen={() => onOpen(t.id)} />
      ))}

      {drafts.length > 0 ? (
        <>
          <div className="opr-section-label">
            <Eyebrow>Drafts</Eyebrow>
            <div className="opr-section-rule" />
          </div>
          <div className="opr-grid">
            {drafts.map((t) => (
              <ToolRow key={t.id} tool={t} onOpen={() => onOpen(t.id)} />
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
              <ToolRow key={t.id} tool={t} onOpen={() => onOpen(t.id)} />
            ))}
          </div>
        </>
      ) : null}
    </div>
  );
}

function HeroToolCard({
  tool,
  onOpen,
}: {
  tool: InternalTool;
  onOpen: () => void;
}) {
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
        <span className="opr-tool-emoji" aria-hidden>
          {sigil(tool.name)}
        </span>
        <div className="opr-detail-titles">
          <div className="opr-tool-name">{tool.name}</div>
          <p className="opr-tool-summary">{tool.summary}</p>
          <div className="opr-tool-meta">
            <ToolStatusBadge status={tool.status} />
            <span className="opr-meta-dot">{tool.runsToday} runs today</span>
            <span className="opr-meta-dot">last run {tool.lastRun}</span>
          </div>
        </div>
        <button type="button" className="opr-btn opr-btn-sm">
          Open
          <ArrowRight size={13} strokeWidth={1.9} aria-hidden />
        </button>
      </div>
    </div>
  );
}

function ToolRow({
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
      <span className="opr-tool-emoji" aria-hidden>
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
