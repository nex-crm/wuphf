/**
 * SlideWiki — tour slide 4, "Write it once. The whole office knows."
 *
 * Shows the seeded getting-started wiki as a small file tree where each page
 * "lights up" one after another (staggered entrance + a green read tick), so
 * the founder sees the office already has a populated shared brain. A reader
 * row beneath the tree makes the load-bearing point explicit: agents read the
 * wiki as first-class consumers, the same as humans do.
 *
 * The page titles are the real seeded pages from spec section 5; they live
 * here because they are slide-internal illustration, not headline copy.
 * Eyebrow/headline/body come from `OFFICE_TOUR_COPY.wiki`. An instructional
 * caption tells the user what the lighting-up tree represents.
 *
 * Entrance is transform/opacity only, staggered per page, keyed off
 * `data-active`, and reduced-motion safe (pages render lit, no motion).
 */

import { OFFICE_TOUR_COPY, type OfficeTourSlideProps } from "./tourSlides";

const COPY = OFFICE_TOUR_COPY.wiki;

/** The seeded getting-started pages (spec section 5), in reading order. */
const SEEDED_PAGES = [
  "How your office works",
  "Working with agents",
  "The company brain",
  "Channels",
  "Skills and runtimes",
];

/** A small green page tick that lands when a page "lights up". */
function PageTick() {
  return (
    <span className="tour-wiki-tick" aria-hidden="true">
      <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
        <path
          d="M1.5 5.2 L4 7.5 L8.5 2.5"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.6"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </span>
  );
}

export function SlideWiki({ active }: OfficeTourSlideProps) {
  return (
    <div
      className="office-tour-slide office-tour-slide-wiki"
      data-active={active}
    >
      <div className="office-tour-slide-copy">
        <p className="office-tour-slide-eyebrow">{COPY.eyebrow}</p>
        <h2 className="office-tour-slide-headline">{COPY.headline}</h2>
        <p className="office-tour-slide-body">{COPY.body}</p>
        <p className="office-tour-slide-caption">
          Your office ships with these pages already written, and your agents
          read them before they touch your work.
        </p>
      </div>

      <div className="office-tour-slide-stage office-tour-slide-stage--wiki">
        <div className="tour-wiki-tree" aria-hidden="true">
          <div className="tour-wiki-tree-head">
            <span className="tour-wiki-tree-folder">Getting Started</span>
          </div>
          <ul className="tour-wiki-pages">
            {SEEDED_PAGES.map((title, index) => (
              <li
                key={title}
                className="tour-wiki-page"
                style={{ "--tour-wiki-index": index } as React.CSSProperties}
              >
                <span className="tour-wiki-page-glyph">¶</span>
                <span className="tour-wiki-page-title">{title}</span>
                <PageTick />
              </li>
            ))}
          </ul>
        </div>

        <div className="tour-wiki-readers" aria-hidden="true">
          <span className="tour-wiki-reader-dot" />
          <span className="tour-wiki-reader-text">
            Humans and agents read the same pages.
          </span>
        </div>
      </div>
    </div>
  );
}
