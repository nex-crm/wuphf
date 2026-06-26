import { Plus, Refresh, Settings, Xmark } from "iconoir-react";

import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "Design System/Atoms/IconButton",
  parameters: { layout: "padded" },
};

export default meta;

export const Sizes: StoryObj = {
  render: () => (
    <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
      <button type="button" className="icon-btn icon-btn--sm" aria-label="Close">
        <Xmark width={14} height={14} />
      </button>
      <button type="button" className="icon-btn" aria-label="Settings">
        <Settings width={18} height={18} />
      </button>
      <button type="button" className="icon-btn icon-btn--lg" aria-label="Add">
        <Plus width={22} height={22} />
      </button>
    </div>
  ),
};

export const States: StoryObj = {
  render: () => (
    <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
      <button type="button" className="icon-btn" aria-label="Default">
        <Refresh width={18} height={18} />
      </button>
      <button type="button" className="icon-btn" aria-label="Disabled" disabled>
        <Refresh width={18} height={18} />
      </button>
    </div>
  ),
};

export const InContext: StoryObj = {
  name: "In context",
  render: () => (
    <header
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        padding: "var(--space-3) var(--space-4)",
        background: "var(--bg-card)",
        border: "1px solid var(--border)",
        borderRadius: "var(--radius-md)",
        maxWidth: 480,
      }}
    >
      <div style={{ display: "flex", flexDirection: "column" }}>
        <span style={{ color: "var(--text)", fontWeight: 600 }}>
          #architecture
        </span>
        <span style={{ color: "var(--text-tertiary)", fontSize: 12 }}>
          12 agents, 3 humans
        </span>
      </div>
      <div style={{ display: "flex", gap: 4 }}>
        <button type="button" className="icon-btn" aria-label="Refresh">
          <Refresh width={18} height={18} />
        </button>
        <button type="button" className="icon-btn" aria-label="Settings">
          <Settings width={18} height={18} />
        </button>
        <button type="button" className="icon-btn" aria-label="Close">
          <Xmark width={18} height={18} />
        </button>
      </div>
    </header>
  ),
};
