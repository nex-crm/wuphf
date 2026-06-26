import type { Meta, StoryObj } from "@storybook/react-vite";

import { LightningIcon } from "./LightningIcon";

const meta: Meta<typeof LightningIcon> = {
  title: "Design System/Atoms/LightningIcon",
  component: LightningIcon,
  argTypes: {
    size: { control: { type: "range", min: 8, max: 64, step: 1 } },
  },
  args: { size: 16 },
};

export default meta;
type Story = StoryObj<typeof LightningIcon>;

export const Default: Story = {};

export const WithLabel: Story = {
  args: { size: 24, title: "Quick action" },
};

export const Inline: StoryObj = {
  render: () => (
    <p style={{ fontSize: 14, color: "var(--text)" }}>
      Press <LightningIcon size={14} /> for the lightning panel.
    </p>
  ),
};

export const ColorInherit: StoryObj = {
  render: () => (
    <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
      <span style={{ color: "var(--accent, #9f4dbf)" }}>
        <LightningIcon size={20} />
      </span>
      <span style={{ color: "var(--warning-500, #c97f2a)" }}>
        <LightningIcon size={20} />
      </span>
      <span style={{ color: "var(--text-secondary, #777)" }}>
        <LightningIcon size={20} />
      </span>
    </div>
  ),
};
