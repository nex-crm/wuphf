/**
 * SlideAgents — tour slide 2, "Your team, on the clock."
 *
 * Teaches what an agent IS by anatomizing one. The mock sidebar lights
 * `@analyst` (the row the hero card belongs to) so the user connects the row
 * to the card. The hero card carries three numbered callouts, each with a
 * one-line instruction:
 *   1. Persona — the agent has a name and a role, not a blank prompt box.
 *   2. Heartbeat — a pulsing green status dot; the agent checks in on its own.
 *   3. Claims work — the agent picks up issues without being micromanaged.
 *
 * Copy for the eyebrow/headline/body comes from `OFFICE_TOUR_COPY.agents`; the
 * callout instructions live here because they are slide-internal teaching
 * scaffolding, not headline copy. All callout text follows WUPHF voice (no
 * contractions, no em-dashes, Oxford comma).
 *
 * Entrance is transform/opacity only, keyed off `data-active`, reduced-motion
 * safe via the slides stylesheet.
 */

import { TourMockupSidebar } from "./TourMockupSidebar";
import { OFFICE_TOUR_COPY, type OfficeTourSlideProps } from "./tourSlides";

const COPY = OFFICE_TOUR_COPY.agents;

/** Slide-internal teaching callouts that anatomize a single agent. */
interface AgentCallout {
  /** Short label for the part of the agent being pointed at. */
  label: string;
  /** One-line instruction so a first-time founder learns the concept. */
  detail: string;
}

const CALLOUTS: AgentCallout[] = [
  {
    label: "Persona",
    detail: "A name and a role, so you know who you are handing work to.",
  },
  {
    label: "Heartbeat",
    detail:
      "A pulse that keeps it checking in, even when you are not watching.",
  },
  {
    label: "Claims work",
    detail:
      "It picks up the CRM cleanup and funnel checks and runs, no nudging required.",
  },
];

export function SlideAgents({ active }: OfficeTourSlideProps) {
  return (
    <div
      className="office-tour-slide office-tour-slide-agents"
      data-active={active}
    >
      <div className="office-tour-slide-copy">
        <p className="office-tour-slide-eyebrow">{COPY.eyebrow}</p>
        <h2 className="office-tour-slide-headline">{COPY.headline}</h2>
        <p className="office-tour-slide-body">{COPY.body}</p>
      </div>

      <div className="office-tour-slide-stage office-tour-slide-stage--agents">
        <TourMockupSidebar activeAgent="@analyst" litRows={["analyst"]} />

        <article className="tour-agent-card">
          <header className="tour-agent-card-head">
            {/* Numbered callout 1 anchors on the identity block. */}
            <span className="tour-agent-card-avatar" aria-hidden="true">
              <span className="tour-agent-card-avatar-glyph">A</span>
            </span>
            <span className="tour-agent-card-identity">
              <span className="tour-agent-card-name">Analyst</span>
              <span className="tour-agent-card-handle">@analyst</span>
            </span>
            {/* Numbered callout 2 anchors on the heartbeat dot. */}
            <span className="tour-agent-card-heartbeat">
              <span className="tour-agent-card-pulse" aria-hidden="true" />
              <span className="tour-agent-card-status">On the clock</span>
            </span>
          </header>

          <ol className="tour-agent-callouts">
            {CALLOUTS.map((callout, index) => (
              <li key={callout.label} className="tour-agent-callout">
                <span className="tour-agent-callout-num" aria-hidden="true">
                  {index + 1}
                </span>
                <span className="tour-agent-callout-text">
                  <span className="tour-agent-callout-label">
                    {callout.label}
                  </span>
                  <span className="tour-agent-callout-detail">
                    {callout.detail}
                  </span>
                </span>
              </li>
            ))}
          </ol>
        </article>
      </div>
    </div>
  );
}
