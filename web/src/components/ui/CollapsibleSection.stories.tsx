import type { Meta, StoryObj } from "@storybook/react-vite";

import { CollapsibleSection } from "./CollapsibleSection";

const meta: Meta<typeof CollapsibleSection> = {
  title: "Design System/Molecules/CollapsibleSection",
  component: CollapsibleSection,
  args: {
    title: "Recent activity",
    defaultOpen: true,
    id: "demo",
    children: (
      <div style={{ padding: 12, color: "var(--text)" }}>
        <p style={{ marginBottom: 8 }}>Lina merged a wiki entry — 2 min ago</p>
        <p style={{ marginBottom: 8 }}>Atlas closed task #4231 — 12 min ago</p>
        <p>Sage updated the calendar — 24 min ago</p>
      </div>
    ),
  },
  parameters: { layout: "padded" },
};

export default meta;
type Story = StoryObj<typeof CollapsibleSection>;

export const Default: Story = {};

export const Closed: Story = {
  args: { defaultOpen: false },
};

export const WithMeta: Story = {
  args: {
    meta: (
      <span
        style={{
          padding: "2px 6px",
          borderRadius: 4,
          background: "var(--bg-warm)",
          fontSize: 11,
          color: "var(--text-secondary)",
        }}
      >
        12
      </span>
    ),
  },
};
