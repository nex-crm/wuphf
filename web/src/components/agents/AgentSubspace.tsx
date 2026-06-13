/**
 * AgentSubspace — tabbed per-agent view with 6 tabs:
 *   Chat · Tasks · Skills · Policies · Live Stream · Config
 *
 * The shell header (avatar + editable name + role + status + current-task chip)
 * is persistent across all tabs. Tab content is mounted/unmounted on switch;
 * the active tab uses stable keys to prevent unnecessary remounts.
 */

import { useCallback } from "react";

import type { OfficeMember } from "../../api/client";
import { useDefaultHarness } from "../../hooks/useConfig";
import { resolveHarness } from "../../lib/harness";
import { router } from "../../lib/router";
import { HarnessBadge } from "../ui/HarnessBadge";
import { PixelAvatar } from "../ui/PixelAvatar";
import { EditableName } from "./AgentProfilePanel";
import { ChatTab } from "./tabs/ChatTab";
import { ConfigTab } from "./tabs/ConfigTab";
import { LiveStreamTab } from "./tabs/LiveStreamTab";
import { PoliciesTab } from "./tabs/PoliciesTab";
import { SkillsTab } from "./tabs/SkillsTab";
import { TasksTab } from "./tabs/TasksTab";

// ── Tab definitions ─────────────────────────────────────────────

export type AgentTab =
  | "chat"
  | "tasks"
  | "skills"
  | "policies"
  | "live-stream"
  | "config";

export const AGENT_TABS: Array<{ id: AgentTab; label: string }> = [
  { id: "chat", label: "Chat" },
  { id: "tasks", label: "Tasks" },
  { id: "skills", label: "Skills" },
  { id: "policies", label: "Policies" },
  { id: "live-stream", label: "Live Stream" },
  { id: "config", label: "Config" },
];

// ── Props ────────────────────────────────────────────────────────

interface AgentSubspaceProps {
  agent: OfficeMember;
  tab: string;
}

function isAgentTab(value: string): value is AgentTab {
  return AGENT_TABS.some((t) => t.id === value);
}

function resolveTab(raw: string): AgentTab {
  return isAgentTab(raw) ? raw : "chat";
}

// ── Shell header ─────────────────────────────────────────────────

interface ShellHeaderProps {
  agent: OfficeMember;
}

function ShellHeader({ agent }: ShellHeaderProps) {
  const defaultHarness = useDefaultHarness();
  const harness = resolveHarness(agent.provider, defaultHarness);
  const statusClass = agent.status === "active" ? "active pulse" : "lurking";

  return (
    <div className="agent-subspace-header">
      <div className="agent-subspace-header-identity">
        {/* Large pixel avatar with harness badge */}
        <div className="agent-subspace-header-avatar avatar-with-harness">
          <PixelAvatar slug={agent.slug} size={48} />
          <HarnessBadge
            kind={harness}
            size={16}
            className="harness-badge-on-avatar"
          />
        </div>

        {/* Name + role + status */}
        <div className="agent-subspace-header-meta">
          <div className="agent-subspace-header-name-row">
            <EditableName agent={agent} />
            <span
              className={`status-dot ${statusClass}`}
              title={agent.status === "active" ? "Active" : "Idle"}
              aria-hidden="true"
            />
          </div>
          {agent.role ? (
            <div className="agent-subspace-header-role">{agent.role}</div>
          ) : null}
          <div className="agent-subspace-header-status-row">
            {agent.status === "active" ? (
              <span className="agent-subspace-status-badge agent-subspace-status-badge--active">
                Working
              </span>
            ) : (
              <span className="agent-subspace-status-badge agent-subspace-status-badge--idle">
                Idle
              </span>
            )}
            {agent.task && agent.status === "active" ? (
              <span className="agent-subspace-task-chip" title={agent.task}>
                {agent.task}
              </span>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  );
}

// ── Tab bar ──────────────────────────────────────────────────────

interface TabBarProps {
  agentSlug: string;
  activeTab: AgentTab;
}

function TabBar({ agentSlug, activeTab }: TabBarProps) {
  const navigate = useCallback(
    (tab: AgentTab) => {
      void router.navigate({
        to: "/agents/$agentSlug/$tab",
        params: { agentSlug, tab },
      });
    },
    [agentSlug],
  );

  return (
    <div
      className="agent-subspace-tabbar"
      role="tablist"
      aria-label="Agent sections"
    >
      {AGENT_TABS.map((t) => {
        const isActive = t.id === activeTab;
        return (
          <button
            key={t.id}
            type="button"
            role="tab"
            id={`agent-tab-${t.id}`}
            aria-selected={isActive}
            aria-controls={`agent-tabpanel-${t.id}`}
            className={`agent-subspace-tab${isActive ? " is-active" : ""}`}
            onClick={() => navigate(t.id)}
          >
            {t.label}
          </button>
        );
      })}
    </div>
  );
}

// ── Content dispatch ─────────────────────────────────────────────

function TabContent({ agent, tab }: { agent: OfficeMember; tab: AgentTab }) {
  // Each case renders into a stable panel. The `key` on each component ensures
  // the chat/stream don't remount when parent re-renders (slug is stable).
  switch (tab) {
    case "chat":
      return (
        <div
          role="tabpanel"
          id="agent-tabpanel-chat"
          aria-labelledby="agent-tab-chat"
          className="agent-subspace-panel"
        >
          <ChatTab key={`chat-${agent.slug}`} agent={agent} />
        </div>
      );
    case "tasks":
      return (
        <div
          role="tabpanel"
          id="agent-tabpanel-tasks"
          aria-labelledby="agent-tab-tasks"
          className="agent-subspace-panel"
        >
          <TasksTab key={`tasks-${agent.slug}`} agentSlug={agent.slug} />
        </div>
      );
    case "skills":
      return (
        <div
          role="tabpanel"
          id="agent-tabpanel-skills"
          aria-labelledby="agent-tab-skills"
          className="agent-subspace-panel"
        >
          <SkillsTab key={`skills-${agent.slug}`} agentSlug={agent.slug} />
        </div>
      );
    case "policies":
      return (
        <div
          role="tabpanel"
          id="agent-tabpanel-policies"
          aria-labelledby="agent-tab-policies"
          className="agent-subspace-panel"
        >
          <PoliciesTab key={`policies-${agent.slug}`} agentSlug={agent.slug} />
        </div>
      );
    case "live-stream":
      return (
        <div
          role="tabpanel"
          id="agent-tabpanel-live-stream"
          aria-labelledby="agent-tab-live-stream"
          className="agent-subspace-panel"
        >
          <LiveStreamTab
            key={`live-stream-${agent.slug}`}
            agentSlug={agent.slug}
          />
        </div>
      );
    case "config":
      return (
        <div
          role="tabpanel"
          id="agent-tabpanel-config"
          aria-labelledby="agent-tab-config"
          className="agent-subspace-panel"
        >
          <ConfigTab key={`config-${agent.slug}`} agent={agent} />
        </div>
      );
    default: {
      const _exhaustive: never = tab;
      void _exhaustive;
      return null;
    }
  }
}

// ── Main export ──────────────────────────────────────────────────

export function AgentSubspace({ agent, tab }: AgentSubspaceProps) {
  const activeTab = resolveTab(tab);

  return (
    <div
      className="agent-subspace"
      data-testid="agent-subspace"
      data-agent-slug={agent.slug}
    >
      <ShellHeader agent={agent} />
      <TabBar agentSlug={agent.slug} activeTab={activeTab} />
      <TabContent agent={agent} tab={activeTab} />
    </div>
  );
}
