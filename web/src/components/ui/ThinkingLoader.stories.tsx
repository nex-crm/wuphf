import type { Meta, StoryObj } from "@storybook/react-vite";

import { ThinkingLoader } from "./ThinkingLoader";

const meta: Meta<typeof ThinkingLoader> = {
  title: "Design System/Atoms/ThinkingLoader",
  component: ThinkingLoader,
  parameters: {
    docs: {
      description: {
        component:
          "Claude-style 'a response is materializing here' loader. The inline " +
          "variant is a soft incoming-bubble pill with wave dots and a " +
          "traveling sheen; the block variant is a centered shimmer label for " +
          "whole-surface loading. Both adapt to theme via color-mix and respect " +
          "prefers-reduced-motion.",
      },
    },
  },
};

export default meta;
type Story = StoryObj<typeof ThinkingLoader>;

export const Inline: Story = {
  args: { variant: "inline", label: "CEO is typing…" },
};

export const InlineNoLabel: Story = {
  args: { variant: "inline" },
};

export const Block: Story = {
  args: { variant: "block", label: "Loading messages…" },
  decorators: [
    (Story) => (
      <div style={{ height: 160, display: "flex" }}>
        <Story />
      </div>
    ),
  ],
};
