import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "Design System/Atoms/SectionHeader",
  parameters: { layout: "padded" },
};

export default meta;

export const Default: StoryObj = {
  render: () => (
    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <span className="section-header">Recent activity</span>
      <p style={{ color: "var(--text-secondary)", margin: 0 }}>
        Lina merged a wiki entry — 2 min ago
      </p>
    </div>
  ),
};

export const Stacked: StoryObj = {
  render: () => (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 24,
        maxWidth: 360,
      }}
    >
      {["Agents", "Channels", "Apps", "Tools"].map((label) => (
        <section key={label} style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <span className="section-header">{label}</span>
          <div
            style={{
              fontSize: 13,
              color: "var(--text-secondary)",
              padding: "8px 12px",
              border: "1px solid var(--border-light)",
              borderRadius: "var(--radius-sm)",
            }}
          >
            Section body
          </div>
        </section>
      ))}
    </div>
  ),
};
