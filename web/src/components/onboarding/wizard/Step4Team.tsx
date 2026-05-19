import { useState } from "react";

import { ONBOARDING_COPY } from "../../../lib/constants";
import { BtnLabel, CheckIcon, EnterHint } from "./components";
import type { BlueprintAgent } from "./types";

// Pool of pixel-art sprites — alternate by index across the roster so
// adjacent agents always look different.
// Sprites are interleaved manager/worker in /public so adjacent slots
// always look different. Position 0 is reserved for the lead.
const ROSTER_SPRITES = [
  "/team-agent.gif",
  "/team-agent-2.gif",
  "/team-agent-3.gif",
  "/team-agent-4.gif",
  "/team-agent-5.gif",
  "/team-agent-6.gif",
  "/team-agent-7.gif",
  "/team-agent-8.gif",
  "/team-agent-9.gif",
  "/team-agent-10.gif",
  "/team-agent-11.gif",
  "/team-agent-12.gif",
  "/team-agent-13.gif",
  "/team-agent-14.gif",
  "/team-agent-15.gif",
  "/team-agent-16.gif",
  "/team-agent-17.gif",
  "/team-agent-18.gif",
  "/team-agent-19.gif",
  "/team-agent-20.gif",
  "/team-agent-21.gif",
  "/team-agent-22.gif",
  "/team-agent-23.gif",
  "/team-agent-24.gif",
  "/team-agent-25.gif",
];
function spriteAt(index: number): string {
  return ROSTER_SPRITES[index % ROSTER_SPRITES.length];
}

interface TeamStepProps {
  agents: BlueprintAgent[];
  onToggle: (slug: string) => void;
  onNext: () => void;
  onBack: () => void;
}

function TeamAgentTile({
  agent,
  onToggle,
  onHover,
}: {
  agent: BlueprintAgent;
  onToggle: (slug: string) => void;
  onHover: (slug: string | null) => void;
}) {
  const locked = agent.built_in === true;

  return (
    <button
      className={`wiz-team-tile ${agent.checked ? "selected" : ""} ${locked ? "locked" : ""}`}
      onClick={() => onToggle(agent.slug)}
      onMouseEnter={() => onHover(agent.slug)}
      onMouseLeave={() => onHover(null)}
      type="button"
      disabled={locked}
      title={locked ? "Lead agent — always included" : undefined}
    >
      <div className="wiz-team-check">
        {agent.checked ? <CheckIcon /> : null}
      </div>
      <div className="wiz-team-info">
        <div className="wiz-team-name-row">
          <span className="wiz-team-name">{agent.name}</span>
          {locked ? <span className="wiz-team-lead-badge">Lead</span> : null}
        </div>
        {agent.role ? <div className="wiz-team-role">{agent.role}</div> : null}
      </div>
    </button>
  );
}

export function TeamStep({ agents, onToggle, onNext, onBack }: TeamStepProps) {
  const [hoveredSlug, setHoveredSlug] = useState<string | null>(null);
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
        <>
          <div className="wiz-team-roster" aria-hidden="true">
            {agents
              .filter((a) => a.checked)
              .map((agent, i) => (
                <img
                  key={agent.slug}
                  className="wiz-team-roster-sprite"
                  data-muted={
                    hoveredSlug !== null && hoveredSlug !== agent.slug
                      ? "true"
                      : undefined
                  }
                  src={spriteAt(i)}
                  alt=""
                  width={80}
                  height={80}
                />
              ))}
          </div>
          <div className="wiz-team-grid">
            {agents.map((agent) => (
              <TeamAgentTile
                key={agent.slug}
                agent={agent}
                onToggle={onToggle}
                onHover={setHoveredSlug}
              />
            ))}
          </div>
        </>
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
