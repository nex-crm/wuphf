import type { Meta, StoryObj } from "@storybook/react-vite";

import { ArtifactSkeleton } from "./ArtifactSkeleton";

const meta: Meta<typeof ArtifactSkeleton> = {
  title: "Messages / ArtifactSkeleton",
  component: ArtifactSkeleton,
  args: { label: "drafting visual…" },
  parameters: {
    layout: "padded",
    docs: {
      description: {
        component:
          "Skeletal placeholder rendered after an agent's 'gist' chat message while " +
          "the visual artifact (HTML article) is still being generated. Replaces the " +
          "~2 minutes of dead silence in the chat feed between the gist and the " +
          "artifact card. Shimmer is compositor-only (background-position) so it " +
          "never triggers layout work.",
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
  args: { label: "writing article" },
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
