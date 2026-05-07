import { ONBOARDING_COPY } from "../../../lib/constants";
import { BtnLabel, CheckIcon, EnterHint } from "./components";
import type { BlueprintAgent } from "./types";

interface TeamStepProps {
  agents: BlueprintAgent[];
  onToggle: (slug: string) => void;
  onNext: () => void;
  onBack: () => void;
}

function TeamAgentTile({
  agent,
  onToggle,
}: {
  agent: BlueprintAgent;
  onToggle: (slug: string) => void;
}) {
  const locked = agent.built_in === true;

  return (
    <button
      className={`wiz-team-tile ${agent.checked ? "selected" : ""} ${locked ? "locked" : ""}`}
      onClick={() => onToggle(agent.slug)}
      type="button"
      disabled={locked}
      title={locked ? "Lead agent — always included" : undefined}
    >
      <div className="wiz-team-check">
        {agent.checked ? <CheckIcon /> : null}
      </div>
      <div className="wiz-team-info">
        <div className="wiz-team-name-row">
          {agent.emoji ? (
            <span className="wiz-team-emoji">{agent.emoji}</span>
          ) : null}
          <span className="wiz-team-name">{agent.name}</span>
          {locked ? <span className="wiz-team-lead-badge">Lead</span> : null}
        </div>
        {agent.role ? <div className="wiz-team-role">{agent.role}</div> : null}
      </div>
    </button>
  );
}

export function TeamStep({ agents, onToggle, onNext, onBack }: TeamStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <h1 className="wizard-headline wizard-headline-sm">
          {ONBOARDING_COPY.step4_headline}
        </h1>
        <p className="wizard-subhead">{ONBOARDING_COPY.step4_subhead}</p>
      </div>

      {agents.length === 0 ? (
        <div className="wizard-panel">
          <div className="wiz-team-empty">
            No teammates yet. Go back and pick a blueprint, or open the office
            and add agents from the team panel.
          </div>
        </div>
      ) : (
        <div className="wiz-team-grid">
          {agents.map((agent) => (
            <TeamAgentTile key={agent.slug} agent={agent} onToggle={onToggle} />
          ))}
        </div>
      )}

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button className="btn btn-primary" onClick={onNext} type="button">
          <BtnLabel>{ONBOARDING_COPY.step4_cta}</BtnLabel>
          <EnterHint />
        </button>
      </div>
    </div>
  );
}
