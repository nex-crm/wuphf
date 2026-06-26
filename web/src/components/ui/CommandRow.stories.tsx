import type { Meta, StoryObj } from "@storybook/react-vite";

import { ToastContainer } from "./Toast";

import { CommandRow } from "./CommandRow";

const meta: Meta<typeof CommandRow> = {
  title: "Design System/Molecules/CommandRow",
  component: CommandRow,
  args: { command: "ollama pull llama3.2" },
  parameters: { layout: "padded" },
  decorators: [
    (Story) => (
      <div style={{ width: 480 }}>
        <Story />
        <ToastContainer />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof CommandRow>;

export const Default: Story = {};

export const LongCommand: Story = {
  args: {
    command:
      "curl -fsSL https://example.com/install.sh | sh && export PATH=$HOME/.local/bin:$PATH",
  },
};
