import type { OfficeMember } from "../../../api/client";
import { classifyMember } from "../../../lib/officeStatus";

interface AgentPulsePanelProps {
  agents: OfficeMember[];
  limit?: number;
}

/**
 * Renders a compact list of active agents with a status dot and current task.
 * Accepts agents pre-filtered to active members only (see `isAgentActive`).
 * Used by both OfficeOverviewApp and ArtifactsApp.
 */
export function AgentPulsePanel({ agents, limit = 10 }: AgentPulsePanelProps) {
  if (agents.length === 0) {
    return (
      <div
        style={{
          padding: "20px 0",
          textAlign: "center",
          color: "var(--text-tertiary)",
          fontSize: 13,
        }}
      >
        No agents are visibly active right now.
      </div>
    );
  }

  return (
    <>
      {agents.slice(0, limit).map((member) => {
        const { state, label } = classifyMember(member);
        return (
          <div
            key={member.slug}
            className="app-card"
            style={{ marginBottom: 6, display: "flex", alignItems: "center", gap: 8 }}
          >
            <span className={`status-dot ${state}`} />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontWeight: 600, fontSize: 13 }}>
                {member.name || member.slug}
              </div>
              <div className="app-card-meta" style={{ marginTop: 1 }}>
                {member.task || label}
              </div>
            </div>
          </div>
        );
      })}
    </>
  );
}
