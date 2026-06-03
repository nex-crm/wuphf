import { useEffect, useState } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import { OfficeTour } from "./OfficeTour";
import { OFFICE_TOUR_SLIDE_IDS, type OfficeTourSlideId } from "./tourSlides";

/**
 * Stories for the office tour modal host. `OfficeTour` is uncontrolled past
 * its `open` prop and always starts on the intro slide (it resets `index` to
 * 0 whenever `open` flips true), so a "land on slide N" story needs to step
 * the tour forward after it opens. The `StartAtSlide` harness does exactly
 * that by firing ArrowRight the right number of times once mounted, which is
 * how the real modal advances. No app shell, no store, no query: the tour is
 * self-contained.
 *
 * The reduced-motion variant is documented via a Storybook parameter so the
 * decorator and any addon can react to it; the component itself feature-tests
 * `prefers-reduced-motion` at runtime and simply skips the View Transition.
 */
const meta: Meta<typeof OfficeTour> = {
  title: "Onboarding/OfficeTour",
  component: OfficeTour,
  parameters: {
    layout: "fullscreen",
  },
  args: {
    open: true,
    onClose: () => {},
    onFinish: () => {},
  },
};

export default meta;
type Story = StoryObj<typeof OfficeTour>;

/** Advance the (already open) tour to a target slide via ArrowRight. */
function pressArrowRight(times: number): void {
  for (let i = 0; i < times; i += 1) {
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight" }));
  }
}

interface StartAtSlideProps {
  slide: OfficeTourSlideId;
  onClose: () => void;
  onFinish: () => void;
}

/**
 * Mount the tour open, then step it forward to `slide`. The step runs on the
 * next tick (after the open-reset effect has set index back to 0) so the
 * arrow presses land on a stable starting point.
 */
function StartAtSlide({ slide, onClose, onFinish }: StartAtSlideProps) {
  const [ready, setReady] = useState(false);
  const target = OFFICE_TOUR_SLIDE_IDS.indexOf(slide);

  useEffect(() => {
    const id = window.setTimeout(() => {
      if (target > 0) pressArrowRight(target);
      setReady(true);
    }, 0);
    return () => window.clearTimeout(id);
  }, [target]);

  // `ready` exists only to make the deferred advance observable in the story
  // tree; the tour itself is always rendered open.
  return (
    <div data-tour-ready={ready}>
      <OfficeTour open={true} onClose={onClose} onFinish={onFinish} />
    </div>
  );
}

/** Default: the tour opens on the intro slide. */
export const Open: Story = {};

/** Slide 1, "This is your office." (same as Open, named for clarity.) */
export const SlideIntro: Story = {
  render: (args) => (
    <StartAtSlide
      slide="intro"
      onClose={args.onClose}
      onFinish={args.onFinish}
    />
  ),
};

/** Slide 2, "Your team, on the clock." */
export const SlideAgents: Story = {
  render: (args) => (
    <StartAtSlide
      slide="agents"
      onClose={args.onClose}
      onFinish={args.onFinish}
    />
  ),
};

/** Slide 3, "File it. They ship it." */
export const SlideIssues: Story = {
  render: (args) => (
    <StartAtSlide
      slide="issues"
      onClose={args.onClose}
      onFinish={args.onFinish}
    />
  ),
};

/** Slide 4, "Write it once. The whole office knows." Finish CTA visible. */
export const SlideWiki: Story = {
  render: (args) => (
    <StartAtSlide
      slide="wiki"
      onClose={args.onClose}
      onFinish={args.onFinish}
    />
  ),
};

/**
 * Reduced-motion variant. The component skips the View Transition under
 * `prefers-reduced-motion: reduce`; this story flags that intent for the
 * Storybook addon and renders the same modal so the static layout can be
 * reviewed without slide-morph motion.
 */
export const ReducedMotion: Story = {
  parameters: {
    chromatic: { prefersReducedMotion: "reduce" },
  },
  decorators: [
    (Story) => (
      <div data-prefers-reduced-motion="reduce">
        <Story />
      </div>
    ),
  ],
};
