/**
 * tourSlides: single source of truth for the guided office tour.
 *
 * This module owns the slide CONTRACT and the slide COPY. The modal host
 * (`OfficeTour.tsx`) and the four slide components (`SlideIntro`,
 * `SlideAgents`, `SlideIssues`, `SlideWiki`, authored by a sibling task)
 * both conform to the types and ids declared here so neither side can
 * drift from the other.
 *
 * Copy is finalized in section 6 of docs/specs/office-onboarding-uplift.md
 * and reproduced verbatim below. Keep it in one reviewable place: changing
 * a headline means editing this file, not hunting through JSX.
 */

/** Ordered slide ids. The tour always runs intro → agents → issues → wiki. */
export type OfficeTourSlideId = "intro" | "agents" | "issues" | "wiki";

/**
 * Props every slide component receives from the modal host.
 *
 * `active` flips true when the slide becomes the visible one. A slide is
 * expected to remount or otherwise replay its entrance animation when
 * `active` transitions to true, so the entrance reads fresh each time the
 * user steps forward or back. The host guarantees a stable React key per
 * slide; slides may additionally key internal motion off `active`.
 */
export interface OfficeTourSlideProps {
  active: boolean;
}

/**
 * Ordered slide ids. Index in this array is the slide's position and the
 * source of the progress-dot order. Do not reorder without updating copy.
 */
export const OFFICE_TOUR_SLIDE_IDS: OfficeTourSlideId[] = [
  "intro",
  "agents",
  "issues",
  "wiki",
];

/**
 * Per-slide copy. `eyebrow` is the small caps kicker above the headline
 * (the intro slide has none). `caption` is the short instructional line under
 * the body that tells the user what the slide's visual is showing them; not
 * every slide needs one. All strings are WUPHF voice: no em-dashes, no
 * contractions, Oxford comma. Verbatim from spec section 6.
 */
export const OFFICE_TOUR_COPY: Record<
  OfficeTourSlideId,
  {
    eyebrow?: string;
    headline: string;
    body: string;
    caption?: string;
    lead?: string;
  }
> = {
  intro: {
    // `lead` is the CEO hand-off line. The tour opens as a continuation of the
    // onboarding chat you just finished, so it reads as one arc rather than a
    // second, separate intro: the CEO walks you into your new office.
    lead: "Your office is ready. Let me show you around.",
    headline: "This is your office.",
    body: "A team of agents lives here. They claim work, they ship, and they actually answer your messages.",
    caption:
      "This mock office assembles itself on the right. Yours is already real, one panel over.",
  },
  agents: {
    eyebrow: "MEET THE TEAM",
    headline: "Your team, on the clock.",
    body: "Every agent has a role, a heartbeat that keeps it checking in, and a memory that does not reset. Unlike Ryan Howard, they actually ship.",
  },
  issues: {
    eyebrow: "FILE AN ISSUE",
    headline: "File it. They ship it.",
    body: "Mention an agent with @, hand off a problem, and the work fans out into tasks across the team while you watch.",
    caption:
      "Watch one message fan out into a task per agent, then land back in a channel you can see.",
  },
  wiki: {
    eyebrow: "YOUR CONTEXT GRAPH",
    headline: "Write it once. The whole office knows.",
    body: "Your wiki is the shared brain. Agents read it as first-class citizens, so context you capture once never has to be repeated.",
  },
};

/**
 * UI chrome labels for the modal host. The finish CTA deliberately reads as
 * an action ("Write your first issue") rather than "Done": the tour
 * deposits the user mid-action instead of dead-ending. `transitionFallback`
 * is the synchronous-fallback note used when the View Transitions API is
 * unavailable or motion is reduced. Verbatim from spec section 6.
 */
export const OFFICE_TOUR_LABELS = {
  dialog: "A quick tour of your office",
  skip: "Skip the tour",
  next: "Next",
  back: "Back",
  finish: "Write your first issue",
  transitionFallback: "Setting up the next room of the tour.",
  replay: "Replay the office tour",
} as const;
