/**
 * SlideIntro — tour slide 1, "This is your office."
 *
 * Sets the metaphor: WUPHF is an office, and a team of agents lives in it.
 * The visual is the `TourMockupSidebar` materializing piece by piece — the
 * workspace label, then the channels, then the agents staggering in — so the
 * very first thing the founder sees is their (mock) office assembling itself.
 *
 * Copy is pulled from `OFFICE_TOUR_COPY.intro` (the single source of truth);
 * this file never inlines strings. The headline uses the serif `--font-logo`
 * to read as a title card, not body text. The instructional caption tells the
 * user what they are looking at so the metaphor lands on a first read.
 *
 * Entrance: keyed off `data-active` (set when `active` is true). All motion is
 * transform/opacity only and is disabled under prefers-reduced-motion via the
 * slides stylesheet. The host remounts the slide on navigation, so the
 * entrance replays each time the slide becomes active.
 */

import { PixelAvatar } from "../../ui/PixelAvatar";
import { TourMockupSidebar } from "./TourMockupSidebar";
import { OFFICE_TOUR_COPY, type OfficeTourSlideProps } from "./tourSlides";

const COPY = OFFICE_TOUR_COPY.intro;

export function SlideIntro({ active }: OfficeTourSlideProps) {
  return (
    <div
      className="office-tour-slide office-tour-slide-intro"
      data-active={active}
    >
      <div className="office-tour-slide-copy">
        {COPY.lead ? (
          <p className="office-tour-slide-lead">
            <span className="office-tour-slide-lead-avatar" aria-hidden="true">
              <PixelAvatar slug="ceo" size={24} />
            </span>
            <span className="office-tour-slide-lead-text">{COPY.lead}</span>
          </p>
        ) : null}
        <h2 className="office-tour-slide-headline office-tour-slide-headline--serif">
          {COPY.headline}
        </h2>
        <p className="office-tour-slide-body">{COPY.body}</p>
        {COPY.caption ? (
          <p className="office-tour-slide-caption">{COPY.caption}</p>
        ) : null}
      </div>

      <div className="office-tour-slide-stage office-tour-slide-stage--intro">
        {/* The mock sidebar materializes via staggered CSS entrance on its
            own sections; no lit ticks yet, the office is just waking up. */}
        <TourMockupSidebar />
      </div>
    </div>
  );
}
