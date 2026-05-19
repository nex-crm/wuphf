import type { Meta, StoryObj } from "@storybook/react-vite";

import { RedactedBadge } from "./RedactedBadge";

const meta: Meta<typeof RedactedBadge> = {
  title: "Design System/Atoms/RedactedBadge",
  component: RedactedBadge,
};

export default meta;
type Story = StoryObj<typeof RedactedBadge>;

export const Default: Story = {};

export const WithReasons: Story = {
  args: { reasons: ["api-key", "email", "phone"] },
};
