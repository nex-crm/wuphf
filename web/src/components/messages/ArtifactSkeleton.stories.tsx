import type { Meta, StoryObj } from "@storybook/react-vite";

import { ArtifactSkeleton } from "./ArtifactSkeleton";

const meta: Meta<typeof ArtifactSkeleton> = {
  title: "Messages / ArtifactSkeleton",
  component: ArtifactSkeleton,
  args: { label: "drafting figure", figureNumber: 7 },
  parameters: {
    layout: "padded",
    docs: {
      description: {
        component:
          "Technical-manual draft preview rendered after an agent's gist message " +
          "while the HTML artifact is still being generated. Frames the wait as " +
          "FIG_NNN being plotted live on a paper card — schematic SVG self-draws, " +
          "an accent dot pulses, an ellipsis ticks, and a thin accent sliver at the " +
          "bottom grows over ~25s. All motion is compositor-only and collapses " +
          "to a static state under prefers-reduced-motion.",
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof ArtifactSkeleton>;

/** Default copy — what ships in production. */
export const Default: Story = {};

/** Caller can swap the label for surfaces that produce text rather than charts. */
export const WritingArticle: Story = {
  args: { label: "writing article", figureNumber: 12 },
};

/** Demonstrates the reduced-motion fallback so reviewers can confirm the
 * static state still reads as "draft figure in progress". Toggle the
 * "prefers-reduced-motion" emulator from the Storybook toolbar to view. */
export const ReducedMotion: Story = {
  args: { label: "drafting figure", figureNumber: 18 },
  parameters: {
    docs: {
      description: {
        story:
          "Activate the prefers-reduced-motion media query (toolbar or OS) " +
          "to see the static, no-animation rendering.",
      },
    },
  },
};

/**
 * Side-by-side with a faux "gist" message above it, so it's obvious how this
 * reads inside a real chat feed.
 */
export const InChatContext: Story = {
  render: (args) => (
    <div
      style={{
        display: "grid",
        gap: 8,
        maxWidth: 640,
        fontFamily:
          "var(--font-base, system-ui), -apple-system, Segoe UI, sans-serif",
      }}
    >
      <div
        style={{
          padding: "8px 12px",
          color: "var(--text)",
          background: "var(--bg-card)",
          border: "1px solid var(--border)",
          borderRadius: 8,
        }}
      >
        <div
          style={{
            color: "var(--text-secondary)",
            fontSize: 12,
            marginBottom: 4,
          }}
        >
          Mara · just now
        </div>
        <div>
          Coffee extraction is a 2-axis problem: grind size sets the surface
          area, temperature sets the rate. Full breakdown below.
        </div>
      </div>
      <ArtifactSkeleton {...args} />
    </div>
  ),
};

/**
 * Theme coverage. Each story below pre-selects the theme via the toolbar arg
 * the .storybook decorator already maps to `data-theme` + the matching CSS
 * file under `/themes/`. If the toolbar global doesn't apply, fall back to
 * switching the theme manually from the Storybook toolbar.
 */
export const LightTheme: Story = {
  globals: { theme: "nex-light" },
};

export const DarkTheme: Story = {
  globals: { theme: "nex-dark" },
};

export const NoirGoldTheme: Story = {
  globals: { theme: "noir-gold" },
};
