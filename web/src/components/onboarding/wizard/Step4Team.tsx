import { ArrowIcon, CheckIcon, EnterHint } from "./components";
import type { BlueprintAgent } from "./types";

interface TeamStepProps {
  agents: BlueprintAgent[];
  onToggle: (slug: string) => void;
  onNext: () => void;
  onBack: () => void;
}

export function TeamStep({ agents, onToggle, onNext, onBack }: TeamStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-panel">
        <p className="wizard-panel-title">Your team</p>
        <p
          style={{
            fontSize: 12,
            color: "var(--text-secondary)",
            margin: "-8px 0 12px 0",
          }}
        >
          These are the specialists your blueprint assembled. Toggle anyone you
          don&apos;t need.
        </p>

        {agents.length === 0 ? (
          <div className="wiz-team-empty">
            No teammates yet. Go back and pick a blueprint, or open the office
            and add agents from the team panel.
          </div>
        ) : (
          <div className="wiz-team-grid">
            {agents.map((a) => {
              // Lead agent is always included and cannot be unchecked here.
              // The backend also refuses to remove or disable any BuiltIn
              // member, so this is UI belt + server-side braces.
              const locked = a.built_in === true;
              return (
                <button
                  key={a.slug}
                  className={`wiz-team-tile ${a.checked ? "selected" : ""} ${locked ? "locked" : ""}`}
                  onClick={() => onToggle(a.slug)}
                  type="button"
                  disabled={locked}
                  title={locked ? "Lead agent — always included" : undefined}
                >
                  <div className="wiz-team-check">
                    {a.checked ? <CheckIcon /> : null}
                  </div>
                  <div>
                    {a.emoji ? (
                      <span style={{ marginRight: 6 }}>{a.emoji}</span>
                    ) : null}
                    <span className="wiz-team-name">{a.name}</span>
                    {locked ? (
                      <span className="wiz-team-lead-badge">Lead</span>
                    ) : null}
                    {a.role ? (
                      <div className="wiz-team-role">{a.role}</div>
                    ) : null}
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </div>

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button className="btn btn-primary" onClick={onNext} type="button">
          Continue
          <ArrowIcon />
          <EnterHint />
        </button>
      </div>
    </div>
  );
}
