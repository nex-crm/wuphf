import type { Meta, StoryObj } from "@storybook/react-vite";

import { TourMockupSidebar } from "./TourMockupSidebar";

/**
 * Stories for the decorative mock sidebar used inside the office tour. The
 * component is purely presentational (no store, no query), so each story only
 * varies the two inputs that slides drive: `activeAgent` and `litRows`. A wide
 * card decorator gives the rows room to breathe; nothing here mounts the app
 * shell.
 */
const meta: Meta<typeof TourMockupSidebar> = {
  title: "Onboarding/TourMockupSidebar",
  component: TourMockupSidebar,
  decorators: [
    (Story) => (
      <div
        style={{
          background: "var(--bg-card)",
          border: "1px solid var(--border)",
          borderRadius: "var(--radius-md, 8px)",
          padding: 16,
          width: 280,
        }}
      >
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof TourMockupSidebar>;

/** Slide 1 state: the office is waking up, nothing lit, no active agent. */
export const Empty: Story = {
  args: {},
};

/** Slide 2 state: the analyst row is highlighted and earns the first tick. */
export const AnalystActive: Story = {
  args: {
    activeAgent: "@analyst",
    litRows: ["analyst"],
  },
};

/** Mid-tour: agents have filled in, channels begin to light up. */
export const PartiallyLit: Story = {
  args: {
    activeAgent: "@engineer",
    litRows: ["analyst", "engineer", "general"],
  },
};

/** The all-ticks end state: every agent and channel row is complete. */
export const AllTicks: Story = {
  args: {
    litRows: ["ceo", "analyst", "engineer", "general", "engineering"],
  },
};
