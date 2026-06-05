/**
 * AgentsTool — the dedicated Agents surface.
 *
 * Two views, both mounted under `/agents`:
 *   • `AgentsTool`   (/agents)            — a roster grid of every agent.
 *   • `AgentDetail`  (/agents/$agentSlug) — the per-agent config page,
 *                                           reusing AgentProfilePanel.
 *
 * Agents are first-class in WUPHF, but they are NOT chat surfaces. The
 * pure task-scoped model reaches an agent through the tasks it owns (each
 * task has its own channel where the agent is a member); this tool is for
 * seeing the roster and configuring an agent's provider / role / skills.
 */

import { useMemo } from "react";

import type { OfficeMember } from "../../api/client";
import { useDefaultHarness } from "../../hooks/useConfig";
import { useOfficeMembers } from "../../hooks/useMembers";
import { type HarnessKind, resolveHarness } from "../../lib/harness";
import { router } from "../../lib/router";
import { HarnessBadge } from "../ui/HarnessBadge";
import { PixelAvatar } from "../ui/PixelAvatar";
import { AgentProfilePanel } from "./AgentProfilePanel";
import { AgentWizard, useAgentWizard } from "./AgentWizard";

/** Short descriptors for the always-present default agents. */
const DEFAULT_AGENT_HINT: Record<string, string> = {
  ceo: "Orchestrator — present on every task",
  librarian: "Librarian — writes and organizes the wiki",
};

function navigateToAgent(slug: string): void {
  void router.navigate({
    to: "/agents/$agentSlug",
    params: { agentSlug: slug },
  });
}

function roleHint(agent: OfficeMember): string {
  return DEFAULT_AGENT_HINT[agent.slug] ?? agent.role ?? "Specialist";
}

interface AgentCardProps {
  agent: OfficeMember;
  defaultHarness: HarnessKind;
}

function AgentCard({ agent, defaultHarness }: AgentCardProps) {
  const harness = resolveHarness(agent.provider, defaultHarness);
  const displayName = agent.name || agent.slug;
  const isActive = (agent.status || "").toLowerCase() === "active";

  return (
    <button
      type="button"
      className="agents-tool-card"
      onClick={() => navigateToAgent(agent.slug)}
      data-agent-slug={agent.slug}
      aria-label={`Configure ${displayName}`}
    >
      <span className="agents-tool-card-avatar avatar-with-harness">
        <PixelAvatar slug={agent.slug} size={40} />
        <HarnessBadge
          kind={harness}
          size={12}
          className="harness-badge-on-avatar"
        />
        {agent.online ? (
          <span className="online-badge" aria-hidden="true" />
        ) : null}
      </span>
      <span className="agents-tool-card-name">{displayName}</span>
      <span className="agents-tool-card-role">{roleHint(agent)}</span>
      <span
        className={`agents-tool-card-status${isActive ? " is-active" : ""}`}
      >
        {isActive ? "Working" : "Idle"}
      </span>
    </button>
  );
}

export function AgentsTool() {
  const { data: members = [] } = useOfficeMembers();
  const defaultHarness = useDefaultHarness();
  const wizard = useAgentWizard();

  // CEO first (orchestrator), then the rest in broker order. Keeps the
  // default agents (CEO + Librarian) reading as the spine of the roster.
  // Key on `members` (stable across polls): filtering inline outside the memo
  // produced a new array every render, so the memo never actually cached.
  const ordered = useMemo<OfficeMember[]>(() => {
    const agents = members.filter((m) => m.slug && m.slug !== "human");
    const ceo = agents.find((a) => a.slug === "ceo");
    const rest = agents.filter((a) => a.slug !== "ceo");
    return ceo ? [ceo, ...rest] : rest;
  }, [members]);

  return (
    <div className="app-panel active agents-tool" data-testid="agents-tool">
      <header className="agents-tool-header">
        <h2 className="agents-tool-heading">Agents</h2>
        <button
          type="button"
          className="issues-new-btn issues-new-btn--header"
          onClick={wizard.show}
          data-testid="agents-tool-new-btn"
          title="Create a new agent"
        >
          + New agent
        </button>
      </header>
      {ordered.length === 0 ? (
        <p className="agents-tool-empty">No agents yet.</p>
      ) : (
        <div className="agents-tool-grid" data-testid="agents-tool-grid">
          {ordered.map((agent) => (
            <AgentCard
              key={agent.slug}
              agent={agent}
              defaultHarness={defaultHarness}
            />
          ))}
        </div>
      )}
      <AgentWizard open={wizard.open} onClose={wizard.hide} />
    </div>
  );
}

interface AgentDetailProps {
  agentSlug: string;
}

export function AgentDetail({ agentSlug }: AgentDetailProps) {
  const { data: members = [] } = useOfficeMembers();
  const agent = useMemo(
    () => members.find((m) => m.slug === agentSlug),
    [members, agentSlug],
  );

  function back() {
    void router.navigate({ to: "/agents" });
  }

  if (!agent) {
    return (
      <div className="app-panel active agents-tool" data-testid="agent-detail">
        <div className="agents-tool-empty">
          <p>No agent “{agentSlug}”.</p>
          <button
            type="button"
            className="issues-new-btn"
            onClick={back}
            data-testid="agent-detail-back"
          >
            ← Back to Agents
          </button>
        </div>
      </div>
    );
  }

  return (
    <div
      className="app-panel active agent-detail-panel"
      data-testid="agent-detail"
      data-agent-slug={agentSlug}
    >
      <AgentProfilePanel agent={agent} onClose={back} />
    </div>
  );
}
