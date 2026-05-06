import { useRef } from "react";

import type { OfficeMember } from "../../api/client";
import { useAgentEventPeek } from "../../hooks/useAgentEventPeek";
import { useDefaultHarness } from "../../hooks/useConfig";
import { useFirstRunNudge } from "../../hooks/useFirstRunNudge";
import { useOfficeMembers } from "../../hooks/useMembers";
import { useOverflow } from "../../hooks/useOverflow";
import { type HarnessKind, resolveHarness } from "../../lib/harness";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { AgentWizard, useAgentWizard } from "../agents/AgentWizard";
import { HarnessBadge } from "../ui/HarnessBadge";
import { PixelAvatar } from "../ui/PixelAvatar";
import { AgentEventPeek } from "./AgentEventPeek";
import { AgentEventPill, AgentEventTickProvider } from "./AgentEventPill";

function classifyActivity(member: OfficeMember | undefined) {
  if (!member)
    return { state: "lurking", label: "lurking", dotClass: "lurking" };
  const status = (member.status || "").toLowerCase();
  const activity = (member.task || "").toLowerCase();

  if (
    status === "active" &&
    /tool|code|write|edit|commit|build|deploy|ship|push|run|test/.test(activity)
  )
    return { state: "shipping", label: "shipping", dotClass: "shipping" };
  if (
    status === "active" &&
    /think|plan|queue|review|sync|debug|trace|investigat/.test(activity)
  )
    return { state: "plotting", label: "plotting", dotClass: "plotting" };
  if (status === "active")
    return { state: "talking", label: "talking", dotClass: "active pulse" };
  return { state: "lurking", label: "lurking", dotClass: "lurking" };
}

interface SidebarAgentRowProps {
  agent: OfficeMember;
  isDMActive: boolean;
  isFirst: boolean;
  showNudge: boolean;
  defaultHarness: HarnessKind;
  onSelect: (slug: string) => void;
}

/**
 * Row body extracted into its own component so the per-row hook
 * (`useAgentEventPeek`) is called once per row instead of inside a `.map`
 * loop in the parent — React forbids hooks in loops directly.
 */
function SidebarAgentRow({
  agent,
  isDMActive,
  isFirst,
  showNudge,
  defaultHarness,
  onSelect,
}: SidebarAgentRowProps) {
  const peek = useAgentEventPeek(agent.slug);
  const anchorRef = useRef<HTMLDivElement>(null);
  const ac = classifyActivity(agent);
  const harness = resolveHarness(agent.provider, defaultHarness);
  const displayName = agent.name || agent.slug;

  return (
    <div
      className="sidebar-agent-row"
      ref={anchorRef}
      {...peek.hoverHandlers}
      {...peek.longPressHandlers}
    >
      <button
        type="button"
        className={`sidebar-agent${isDMActive ? " active" : ""}`}
        title={`${agent.name} — ${ac.label}`}
        onClick={() => onSelect(agent.slug)}
        data-agent-slug={agent.slug}
      >
        <span className="sidebar-agent-avatar avatar-with-harness">
          <PixelAvatar
            slug={agent.slug}
            size={24}
            className="pixel-avatar-sidebar"
          />
          <HarnessBadge
            kind={harness}
            size={10}
            className="harness-badge-on-avatar"
          />
        </span>
        <div className="sidebar-agent-wrap">
          <span className="sidebar-agent-name">{displayName}</span>
          <AgentEventPill
            slug={agent.slug}
            agentRole={agent.role}
            fallbackTask={agent.task}
          />
        </div>
        <span className={`status-dot ${ac.dotClass}`} />
      </button>
      <button
        type="button"
        className="sidebar-agent-peek-trigger"
        aria-haspopup="dialog"
        aria-expanded={peek.isOpen}
        aria-controls={`agent-peek-${agent.slug}`}
        aria-label={`Recent activity for ${displayName}`}
        onClick={(e) => {
          e.stopPropagation();
          peek.toggle();
        }}
        data-testid={`peek-trigger-${agent.slug}`}
      >
        <svg width="8" height="8" viewBox="0 0 8 8" aria-hidden="true">
          <path
            d="M2 1 L6 4 L2 7"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </button>
      <AgentEventPeek
        slug={agent.slug}
        agentName={displayName}
        agentRole={agent.role}
        open={peek.isOpen}
        current={peek.current}
        history={peek.history}
        anchorRef={anchorRef}
        onClose={peek.close}
        onOpenWorkspace={() => {
          peek.close();
          onSelect(agent.slug);
        }}
      />
      {isFirst && showNudge ? (
        <span className="sidebar-agent-nudge" data-testid="first-run-nudge">
          {`→ tag @${agent.slug} in #general`}
        </span>
      ) : null}
    </div>
  );
}

export function AgentList() {
  const { data: members = [] } = useOfficeMembers();
  const setActiveAgentSlug = useAppStore((s) => s.setActiveAgentSlug);
  const route = useCurrentRoute();
  const activeDmAgent = route.kind === "dm" ? route.agentSlug : null;
  const wizard = useAgentWizard();
  const overflowRef = useOverflow<HTMLDivElement>();
  const defaultHarness = useDefaultHarness();
  const { showNudge } = useFirstRunNudge();
  const isReconnecting = useAppStore((s) => s.isReconnecting);

  const agents = members.filter((m) => m.slug && m.slug !== "human");
  const firstAgentSlug = agents[0]?.slug;

  return (
    <AgentEventTickProvider>
      <div className="sidebar-scroll-wrap is-agents">
        <div className="sidebar-agents" ref={overflowRef}>
          {agents.length === 0 ? (
            <div
              style={{
                fontSize: 11,
                color: "var(--text-tertiary)",
                padding: "4px 8px",
              }}
            >
              No agents online
            </div>
          ) : (
            agents.map((agent) => (
              <SidebarAgentRow
                key={agent.slug}
                agent={agent}
                isDMActive={activeDmAgent === agent.slug}
                isFirst={agent.slug === firstAgentSlug}
                showNudge={showNudge}
                defaultHarness={defaultHarness}
                onSelect={setActiveAgentSlug}
              />
            ))
          )}
          <button
            type="button"
            className="sidebar-item sidebar-add-btn"
            onClick={wizard.show}
            title="Create a new agent"
          >
            <span style={{ width: 18, textAlign: "center", flexShrink: 0 }}>
              +
            </span>
            <span>New Agent</span>
          </button>
          {isReconnecting ? (
            <div
              className="sidebar-agents-reconnecting"
              role="status"
              aria-live="polite"
              data-testid="agents-reconnecting"
            >
              Reconnecting…
            </div>
          ) : null}
        </div>
      </div>
      <AgentWizard open={wizard.open} onClose={wizard.hide} />
    </AgentEventTickProvider>
  );
}
