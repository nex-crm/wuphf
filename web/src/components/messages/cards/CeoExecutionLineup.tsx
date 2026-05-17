/**
 * CeoExecutionLineup — Phase 4 execution roster suggestion card.
 *
 * Rendered inside the CEO DM when the broker proposes an agent roster
 * for executing an approved issue. The user can accept or decline each
 * agent individually before submitting.
 *
 * Universal three-stage card matrix (spec design review decisions - State coverage):
 *   pending    — list of agents with role + reason + Accept/Decline chips
 *   submitting — button disabled, inline spinner
 *   committed  — collapses to a one-line confirmation in --text-secondary
 *
 * Sanitization: all agent strings (slug, role, reason) are rendered as
 * React text children, never via innerHTML. Backend sanitization via
 * sanitizeContextValue (PR 684) is inherited through the existing
 * suggestion payload path.
 *
 * Keyboard a11y: Accept/Decline chips are Tab-navigable; Enter and Space
 * activate them.
 *
 * Wire: submit POSTs to /onboarding/suggestion/ack with
 * { suggestion_id, selected_agent_slugs }.
 */

import { useState } from "react";

import { post } from "../../../api/client";
import type {
  CardStage,
  CeoExecutionLineupPayload,
  ExecutionLineupAgent,
} from "../../onboarding/types";
import { showNotice } from "../../ui/Toast";

interface CeoExecutionLineupProps {
  payload: CeoExecutionLineupPayload;
  stage: CardStage;
  onStageChange: (next: CardStage) => void;
}

/**
 * One agent row with Accept / Decline toggle chips.
 * Renders slug/role/reason as plain text nodes.
 */
function AgentRow({
  agent,
  accepted,
  disabled,
  onToggle,
}: {
  agent: ExecutionLineupAgent;
  accepted: boolean;
  disabled: boolean;
  onToggle: () => void;
}) {
  return (
    <div
      className="ceo-lineup-agent-row"
      data-testid={`lineup-agent-row-${agent.slug}`}
    >
      <div className="ceo-lineup-agent-info">
        <span className="ceo-lineup-agent-role">{agent.role}</span>
        <span className="ceo-lineup-agent-reason">{agent.reason}</span>
      </div>
      <button
        type="button"
        className={`ceo-lineup-chip ${accepted ? "ceo-lineup-chip--accept" : "ceo-lineup-chip--decline"}`}
        disabled={disabled}
        onClick={onToggle}
        aria-pressed={accepted}
        aria-label={`${accepted ? "Decline" : "Accept"} ${agent.role}`}
        data-testid={`lineup-chip-${agent.slug}`}
      >
        {accepted ? "Accept" : "Decline"}
      </button>
    </div>
  );
}

export function CeoExecutionLineup({
  payload,
  stage,
  onStageChange,
}: CeoExecutionLineupProps) {
  // All agents accepted by default per spec.
  const [accepted, setAccepted] = useState<Set<string>>(
    () => new Set(payload.agents.map((a) => a.slug)),
  );

  if (stage === "committed") {
    const count = accepted.size;
    return (
      <div
        className="ceo-card ceo-card--committed"
        role="status"
        data-testid="lineup-committed"
      >
        <span className="ceo-card-committed-text">
          &#10003; {count} {count === 1 ? "agent" : "agents"} added to roster
        </span>
      </div>
    );
  }

  const isSubmitting = stage === "submitting";
  const selectedCount = accepted.size;

  const toggleAgent = (slug: string) => {
    if (isSubmitting) return;
    setAccepted((prev) => {
      const next = new Set(prev);
      if (next.has(slug)) {
        next.delete(slug);
      } else {
        next.add(slug);
      }
      return next;
    });
  };

  const handleSubmit = async () => {
    if (isSubmitting || selectedCount === 0) return;
    onStageChange("submitting");
    try {
      await post("/onboarding/suggestion/ack", {
        suggestion_id: payload.suggestion_id,
        selected_agent_slugs: [...accepted],
      });
      onStageChange("committed");
    } catch (err: unknown) {
      const message =
        err instanceof Error ? err.message : "Failed to confirm lineup";
      showNotice(message, "error");
      onStageChange("pending");
    }
  };

  return (
    <div
      className="ceo-card ceo-card--execution-lineup"
      data-testid="ceo-execution-lineup"
    >
      <div className="ceo-card-label">Proposed execution lineup</div>
      <ul className="ceo-lineup-agents" aria-label="Proposed agents">
        {payload.agents.map((agent) => (
          <li key={agent.slug}>
            <AgentRow
              agent={agent}
              accepted={accepted.has(agent.slug)}
              disabled={isSubmitting}
              onToggle={() => toggleAgent(agent.slug)}
            />
          </li>
        ))}
      </ul>
      <div className="ceo-card-actions">
        <button
          type="button"
          className="btn btn-primary btn-sm ceo-card-submit"
          disabled={isSubmitting || selectedCount === 0}
          onClick={() => void handleSubmit()}
          data-testid="lineup-submit"
          aria-label={`Spin up ${selectedCount} ${selectedCount === 1 ? "agent" : "agents"}`}
        >
          {isSubmitting ? (
            <span className="ceo-card-spinner" aria-hidden="true" />
          ) : null}
          {isSubmitting
            ? "Spinning up…"
            : `Spin up ${selectedCount} ${selectedCount === 1 ? "agent" : "agents"}`}
        </button>
      </div>
    </div>
  );
}
