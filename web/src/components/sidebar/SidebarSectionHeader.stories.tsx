import type { Meta, StoryObj } from "@storybook/react-vite";
import type { CSSProperties } from "react";

import { SidebarSectionHeader } from "./SidebarSectionHeader";

const brandCanvas: CSSProperties = {
  background: "#612a92",
  minHeight: "100vh",
  padding: "32px",
  boxSizing: "border-box",
  ["--text" as string]: "#ffffff",
  ["--text-secondary" as string]: "rgba(255, 255, 255, 0.88)",
  ["--text-tertiary" as string]: "rgba(255, 255, 255, 0.6)",
};
const headerStack: CSSProperties = { width: 280 };

const meta = {
  title: "Sidebar/Section header",
  component: SidebarSectionHeader,
  parameters: { layout: "fullscreen" },
  tags: ["autodocs"],
  args: { label: "Channels", open: true, onToggle: () => {} },
  decorators: [
    (Story) => (
      <div style={brandCanvas}>
        <div style={headerStack}>
          <Story />
        </div>
      </div>
    ),
  ],
} satisfies Meta<typeof SidebarSectionHeader>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const WithAction: Story = {
  args: {
    label: "Issues",
    actions: (
      <button type="button" className="sidebar-section-action">
        View all
      </button>
    ),
  },
};

export const States: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <SidebarSectionHeader label="Agents (open)" open onToggle={() => {}} />
      <SidebarSectionHeader
        label="Channels (collapsed)"
        open={false}
        onToggle={() => {}}
      />
      <SidebarSectionHeader
        label="Issues + action"
        open
        onToggle={() => {}}
        actions={
          <button type="button" className="sidebar-section-action">
            View all
          </button>
        }
      />
      <SidebarSectionHeader
        label="Issues + action active"
        open
        onToggle={() => {}}
        actions={
          <button type="button" className="sidebar-section-action active">
            View all
          </button>
        }
      />
    </div>
  ),
};
