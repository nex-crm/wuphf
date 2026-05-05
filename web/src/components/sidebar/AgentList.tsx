import type { OfficeMember } from "../../api/client";
import { useDefaultHarness } from "../../hooks/useConfig";
import { useFirstRunNudge } from "../../hooks/useFirstRunNudge";
import { useOfficeMembers } from "../../hooks/useMembers";
import { useOverflow } from "../../hooks/useOverflow";
import { resolveHarness } from "../../lib/harness";
import { useCurrentRoute } from "../../routes/useCurrentRoute";
import { useAppStore } from "../../stores/app";
import { AgentWizard, useAgentWizard } from "../agents/AgentWizard";
import { HarnessBadge } from "../ui/HarnessBadge";
import { PixelAvatar } from "../ui/PixelAvatar";
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
            agents.map((agent) => {
              const ac = classifyActivity(agent);
              const isDMActive = activeDmAgent === agent.slug;
              const harness = resolveHarness(agent.provider, defaultHarness);
              const isFirst = agent.slug === firstAgentSlug;

              return (
                <div
                  key={agent.slug}
                  className="sidebar-agent-row"
                  data-agent-slug={agent.slug}
                >
                  <button
                    type="button"
                    className={`sidebar-agent${isDMActive ? " active" : ""}`}
                    title={`${agent.name} — ${ac.label}`}
                    onClick={() => setActiveAgentSlug(agent.slug)}
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
                      <span className="sidebar-agent-name">
                        {agent.name || agent.slug}
                      </span>
                      <AgentEventPill
                        slug={agent.slug}
                        agentRole={agent.role}
                        fallbackTask={agent.task}
                      />
                    </div>
                    <span className={`status-dot ${ac.dotClass}`} />
                  </button>
                  {isFirst && showNudge ? (
                    <span
                      className="sidebar-agent-nudge"
                      data-testid="first-run-nudge"
                    >
                      {`→ tag @${agent.slug} in #general`}
                    </span>
                  ) : null}
                </div>
              );
            })
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
