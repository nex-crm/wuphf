import type { Meta, StoryObj } from "@storybook/react-vite";

import { Breadcrumb } from "./Breadcrumb";

const meta: Meta<typeof Breadcrumb> = {
  title: "Design System/Molecules/Breadcrumb",
  component: Breadcrumb,
  parameters: { layout: "padded" },
};

export default meta;
type Story = StoryObj<typeof Breadcrumb>;

export const TwoLevel: Story = {
  args: {
    items: [
      { label: "Tasks", href: "#/tasks" },
      { label: "Migrate payments", href: "#/tasks/migrate-payments" },
    ],
  },
};

export const DeepNesting: Story = {
  args: {
    items: [
      { label: "Wiki", href: "#/wiki" },
      { label: "Architecture", href: "#/wiki/architecture" },
      { label: "Storage layer", href: "#/wiki/architecture/storage" },
      { label: "Sharding", href: "#/wiki/architecture/storage/sharding" },
    ],
  },
};

export const SingleLeaf: Story = {
  args: {
    items: [{ label: "Calendar", href: "#/calendar" }],
  },
};
