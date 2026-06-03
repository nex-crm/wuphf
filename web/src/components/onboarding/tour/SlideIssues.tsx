/**
 * SlideIssues — tour slide 3, "File it. They ship it."
 *
 * Visualizes the issue → tasks → ship loop end to end:
 *   1. A mock composer "types out" an @mention issue via a CSS typewriter
 *      (steps() over the monospace command, with a blinking caret).
 *   2. The work fans out into a small grid of task cards, each with a live
 *      pulsing green heartbeat dot, so the user sees parallel agents pick
 *      work up.
 *   3. A `#general` destination pill closes the loop: the ship lands back in
 *      a channel the founder can see.
 *
 * The typed command string lives here (it is a concrete demo input, not
 * reusable headline copy). Eyebrow/headline/body come from
 * `OFFICE_TOUR_COPY.issues`. An instructional caption tells the user what the
 * animation is showing them.
 *
 * The typewriter is implemented in CSS keyed off `data-active`, animating only
 * `max-width`/transform/opacity, and is disabled under prefers-reduced-motion
 * (the full command is shown statically there). The caret and heartbeat dots
 * are opacity-only animations, also reduced-motion safe.
 */

import type { CSSProperties } from "react";

import { OFFICE_TOUR_COPY, type OfficeTourSlideProps } from "./tourSlides";

const COPY = OFFICE_TOUR_COPY.issues;

/** The concrete issue the mock composer types out. */
const TYPED_COMMAND =
  "@revops dedupe our accounts and backfill every missing deal owner";

/** One fanned-out task card. */
interface MockTask {
  /** Stable key. */
  id: string;
  /** Which agent claimed it. */
  owner: string;
  /** Short, plausible task title. */
  title: string;
}

const FANNED_TASKS: MockTask[] = [
  { id: "task-1", owner: "@revops", title: "Merge 142 duplicate accounts" },
  { id: "task-2", owner: "@revops", title: "Backfill 38 missing deal owners" },
  { id: "task-3", owner: "@analyst", title: "Flag 12 opps stale 30+ days" },
];

export function SlideIssues({ active }: OfficeTourSlideProps) {
  return (
    <div
      className="office-tour-slide office-tour-slide-issues"
      data-active={active}
    >
      <div className="office-tour-slide-copy">
        <p className="office-tour-slide-eyebrow">{COPY.eyebrow}</p>
        <h2 className="office-tour-slide-headline">{COPY.headline}</h2>
        <p className="office-tour-slide-body">{COPY.body}</p>
        {COPY.caption ? (
          <p className="office-tour-slide-caption">{COPY.caption}</p>
        ) : null}
      </div>

      <div className="office-tour-slide-stage office-tour-slide-stage--issues">
        {/* Step 1: the composer types the issue out. The typed length drives
            the caret travel distance (--typed-ch) so the follow stays exact if
            the command copy changes, instead of a hardcoded step count. */}
        <div className="tour-composer" aria-hidden="true">
          <div
            className="tour-composer-input"
            style={
              { "--typed-ch": `${TYPED_COMMAND.length}ch` } as CSSProperties
            }
          >
            <span className="tour-composer-typed">{TYPED_COMMAND}</span>
            <span className="tour-composer-caret" />
          </div>
          <span className="tour-composer-send">Send</span>
        </div>

        {/* Step 2: fan-out into task cards with live heartbeat dots. */}
        <div className="tour-fanout" aria-hidden="true">
          <span className="tour-fanout-stem" />
          <ul className="tour-task-grid">
            {FANNED_TASKS.map((task) => (
              <li key={task.id} className="tour-task-card">
                <span className="tour-task-card-head">
                  <span className="tour-task-heartbeat" />
                  <span className="tour-task-owner">{task.owner}</span>
                </span>
                <span className="tour-task-title">{task.title}</span>
              </li>
            ))}
          </ul>
        </div>

        {/* Step 3: the ship lands in a visible channel. */}
        <div className="tour-destination" aria-hidden="true">
          <span className="tour-destination-arrow" />
          <span className="tour-destination-pill">
            <span className="tour-destination-hash">#</span>revops
          </span>
        </div>
      </div>
    </div>
  );
}
