import type { Meta, StoryObj } from "@storybook/react-vite";

import { ThemeSwitcher } from "./ThemeSwitcher";

const meta: Meta<typeof ThemeSwitcher> = {
  title: "Design System/Organisms/ThemeSwitcher",
  component: ThemeSwitcher,
  parameters: { layout: "padded" },
};

export default meta;
type Story = StoryObj<typeof ThemeSwitcher>;

export const Default: Story = {};
