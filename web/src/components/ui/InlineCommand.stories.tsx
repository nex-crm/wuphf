import type { Meta, StoryObj } from "@storybook/react-vite";

import { InlineCommand } from "./InlineCommand";
import { showNotice, ToastContainer } from "./Toast";

const meta: Meta<typeof InlineCommand> = {
  title: "Design System/Molecules/InlineCommand",
  component: InlineCommand,
  argTypes: {
    tone: { control: "inline-radio", options: ["warning", "neutral"] },
  },
  args: {
    command: "wuphf reset",
    tone: "warning",
    onRun: () => showNotice("Ran wuphf reset", "info"),
  },
  parameters: { layout: "padded" },
  decorators: [
    (Story) => (
      <div>
        <Story />
        <ToastContainer />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof InlineCommand>;

export const Warning: Story = {};

export const Neutral: Story = {
  args: { tone: "neutral", command: "wuphf status" },
};

export const Destructive: Story = {
  args: {
    command: "wuphf shred",
    destructive: {
      title: "Shred this office?",
      intro:
        "This permanently deletes the office, agents, history, and saved workflows.",
      confirmLabel: "Shred office",
      severity: "critical",
    },
  },
};
