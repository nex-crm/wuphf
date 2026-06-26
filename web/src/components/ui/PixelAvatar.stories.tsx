import type { Meta, StoryObj } from "@storybook/react-vite";

import { PixelAvatar } from "./PixelAvatar";

const meta: Meta<typeof PixelAvatar> = {
  title: "Design System/Atoms/PixelAvatar",
  component: PixelAvatar,
  argTypes: {
    size: { control: { type: "range", min: 16, max: 128, step: 4 } },
  },
  args: { slug: "alex", size: 64 },
};

export default meta;
type Story = StoryObj<typeof PixelAvatar>;

export const Default: Story = {};

export const Gallery: StoryObj = {
  render: () => (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 12 }}>
      {["alex", "lina", "ops", "scout", "sage", "echo", "atlas", "iris"].map(
        (slug) => (
          <div
            key={slug}
            style={{
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              gap: 6,
              fontSize: 11,
              color: "var(--text-secondary)",
            }}
          >
            <PixelAvatar slug={slug} size={48} />
            <span>{slug}</span>
          </div>
        ),
      )}
    </div>
  ),
};
