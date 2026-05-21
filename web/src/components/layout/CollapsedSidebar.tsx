import { type MouseEvent, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useQuery } from "@tanstack/react-query";
import {
  Activity,
  ChatBubbleWarning,
  ClockRotateRight,
  Group,
  MultiBubble,
  SidebarExpand,
} from "iconoir-react";

import { getUsage } from "../../api/platform";
import { formatTokens, formatUSD } from "../../lib/format";
import { useAppStore } from "../../stores/app";
import { AgentList } from "../sidebar/AgentList";
import { ChannelList } from "../sidebar/ChannelList";
import { IssuesGroup } from "../sidebar/IssuesGroup";

type Popover = "team" | "channels" | "issues" | "recent" | "usage" | null;
type HintState = { label: string; y: number } | null;

// biome-ignore lint/complexity/noExcessiveCognitiveComplexity: Existing cognitive complexity is baselined for a focused follow-up refactor.
export function CollapsedSidebar() {
  const toggleCollapsed = useAppStore((s) => s.toggleSidebarCollapsed);
  const [popover, setPopover] = useState<Popover>(null);
  const [hint, setHint] = useState<HintState>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const closeTimer = useRef<number | null>(null);

  function openPopover(p: Popover) {
    if (closeTimer.current) {
      window.clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
    setHint(null);
    setPopover(p);
  }
  function scheduleClose() {
    if (closeTimer.current) window.clearTimeout(closeTimer.current);
    closeTimer.current = window.setTimeout(() => setPopover(null), 120);
  }
  function showHint(e: MouseEvent<HTMLElement>, label: string) {
    const r = e.currentTarget.getBoundingClientRect();
    setHint({ label, y: r.top + r.height / 2 });
  }
  function hideHint() {
    setHint(null);
  }

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        setPopover(null);
        setHint(null);
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    return () => {
      if (closeTimer.current) {
        window.clearTimeout(closeTimer.current);
        closeTimer.current = null;
      }
    };
  }, []);

  return (
    <>
      <div className="sidebar-rail-top">
        <button
          type="button"
          className="sidebar-icon-btn"
          aria-label="Expand sidebar"
          onClick={toggleCollapsed}
          onMouseEnter={(e) => showHint(e, "Expand sidebar")}
          onMouseLeave={hideHint}
        >
          <SidebarExpand />
        </button>
        {/* Settings moved to the WorkspaceRail bottom; the collapsed
            sidebar no longer carries a separate Settings shortcut. */}
      </div>

      <div className="sidebar-rail-middle">
        <button
          type="button"
          className={`sidebar-icon-btn${popover === "team" ? " is-open" : ""}`}
          aria-label="Agents"
          aria-haspopup="dialog"
          aria-expanded={popover === "team"}
          onMouseEnter={() => openPopover("team")}
          onMouseLeave={scheduleClose}
          onFocus={() => openPopover("team")}
          onBlur={scheduleClose}
        >
          <Group />
        </button>
        <button
          type="button"
          className={`sidebar-icon-btn${popover === "channels" ? " is-open" : ""}`}
          aria-label="Channels"
          aria-haspopup="dialog"
          aria-expanded={popover === "channels"}
          onMouseEnter={() => openPopover("channels")}
          onMouseLeave={scheduleClose}
          onFocus={() => openPopover("channels")}
          onBlur={scheduleClose}
        >
          <MultiBubble />
        </button>
        <button
          type="button"
          className={`sidebar-icon-btn${popover === "issues" ? " is-open" : ""}`}
          aria-label="Issues"
          aria-haspopup="dialog"
          aria-expanded={popover === "issues"}
          onMouseEnter={() => openPopover("issues")}
          onMouseLeave={scheduleClose}
          onFocus={() => openPopover("issues")}
          onBlur={scheduleClose}
        >
          <ChatBubbleWarning />
        </button>
        <button
          type="button"
          className={`sidebar-icon-btn${popover === "recent" ? " is-open" : ""}`}
          aria-label="Recent"
          aria-haspopup="dialog"
          aria-expanded={popover === "recent"}
          onMouseEnter={() => openPopover("recent")}
          onMouseLeave={scheduleClose}
          onFocus={() => openPopover("recent")}
          onBlur={scheduleClose}
        >
          <ClockRotateRight />
        </button>
      </div>

      {/* Tools moved to the WorkspaceRail (left edge); the collapsed
          sidebar no longer mirrors them here. */}

      <UsageRail
        onEnter={() => openPopover("usage")}
        onLeave={scheduleClose}
        active={popover === "usage"}
      />

      {popover
        ? createPortal(
            <div
              ref={popoverRef}
              className={`sidebar-rail-popover sidebar-rail-popover-${popover}`}
              role="dialog"
              onMouseEnter={() => openPopover(popover)}
              onMouseLeave={scheduleClose}
            >
              <div className="sidebar-rail-popover-title">
                {popover === "team"
                  ? "Agents"
                  : popover === "channels"
                    ? "Channels"
                    : popover === "issues"
                      ? "Issues"
                      : popover === "recent"
                        ? "Recent"
                        : "Usage"}
              </div>
              <div className="sidebar-rail-popover-body">
                {popover === "team" ? <AgentList /> : null}
                {popover === "channels" ? <ChannelList /> : null}
                {popover === "issues" ? <IssuesGroup open /> : null}
                {popover === "recent" ? (
                  <div className="rail-popover-empty">
                    No recent items yet.
                  </div>
                ) : null}
                {popover === "usage" ? <UsageBody /> : null}
              </div>
            </div>,
            document.body,
          )
        : null}

      {hint
        ? createPortal(
            <div
              className="sidebar-rail-hint"
              style={{ top: hint.y }}
              role="tooltip"
            >
              {hint.label}
            </div>,
            document.body,
          )
        : null}
    </>
  );
}

function formatCompactUSD(v: number): string {
  if (v >= 1000) return `$${(v / 1000).toFixed(1)}k`;
  if (v >= 100) return `$${v.toFixed(0)}`;
  if (v >= 10) return `$${v.toFixed(1)}`;
  return `$${v.toFixed(2)}`;
}

function UsageRail({
  onEnter,
  onLeave,
  active,
}: {
  onEnter: () => void;
  onLeave: () => void;
  active: boolean;
}) {
  const { data: usage } = useQuery({
    queryKey: ["usage"],
    queryFn: () => getUsage(),
    refetchInterval: 30_000,
  });
  const totalCost = usage?.total?.cost_usd ?? 0;
  return (
    <button
      type="button"
      className={`sidebar-rail-bottom${active ? " is-open" : ""}`}
      aria-label={`Usage ${formatUSD(totalCost)}`}
      aria-haspopup="dialog"
      aria-expanded={active}
      onMouseEnter={onEnter}
      onMouseLeave={onLeave}
      onFocus={onEnter}
      onBlur={onLeave}
      title={`Usage ${formatUSD(totalCost)}`}
    >
      <Activity className="sidebar-rail-usage-icon" />
      <span className="sidebar-rail-usage-value">
        {formatCompactUSD(totalCost)}
      </span>
    </button>
  );
}

function UsageBody() {
  const { data: usage } = useQuery({
    queryKey: ["usage"],
    queryFn: () => getUsage(),
    refetchInterval: 5000,
  });
  const totalCost = usage?.total?.cost_usd ?? 0;
  const agents = usage?.agents ?? {};
  const slugs = Object.keys(agents).sort();
  if (slugs.length === 0 && totalCost === 0) {
    return (
      <p
        style={{
          fontSize: 11,
          color: "var(--text-tertiary)",
          padding: "8px 14px",
        }}
      >
        No usage recorded yet.
      </p>
    );
  }
  return (
    <div className="sidebar-rail-usage-panel">
      <table className="usage-table">
        <thead>
          <tr>
            {["Agent", "In", "Out", "Cache", "Cost"].map((h) => (
              <th key={h}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {slugs.map((slug) => {
            const a = agents[slug];
            return (
              <tr key={slug}>
                <td>{slug}</td>
                <td>{formatTokens(a.input_tokens)}</td>
                <td>{formatTokens(a.output_tokens)}</td>
                <td>{formatTokens(a.cache_read_tokens)}</td>
                <td>{formatUSD(a.cost_usd)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <div className="usage-total">
        <span>
          Session: {formatTokens(usage?.session?.total_tokens ?? 0)} tokens
        </span>
        <span className="usage-total-cost">{formatUSD(totalCost)}</span>
      </div>
    </div>
  );
}
