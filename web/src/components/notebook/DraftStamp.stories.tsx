import type { Meta, StoryObj } from "@storybook/react-vite";

import DraftStamp from "./DraftStamp";

const meta: Meta<typeof DraftStamp> = {
  title: "Features/Notebook/DraftStamp",
  component: DraftStamp,
  parameters: { layout: "padded" },
  decorators: [
    (Story) => (
      <div
        style={{
          position: "relative",
          width: 480,
          height: 280,
          padding: 32,
          background: "var(--bg-card)",
          border: "1px solid var(--border)",
          borderRadius: "var(--radius-md)",
        }}
      >
        <h1
          style={{
            margin: 0,
            fontSize: 22,
            fontWeight: 600,
            color: "var(--text)",
            fontFamily: "Newsreader, serif",
          }}
        >
          Shipping log — week of May 19
        </h1>
        <p
          style={{
            margin: "12px 0",
            color: "var(--text-secondary)",
            fontSize: 14,
            lineHeight: 1.55,
          }}
        >
          Three migrations landed this week; one rollback was needed for the
          cosign signing path. Detail below.
        </p>
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof DraftStamp>;

export const Default: Story = {};

export const CustomLabel: Story = {
  args: { label: "Internal draft — do not link" },
};
