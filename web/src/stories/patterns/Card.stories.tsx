import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "Design System/Molecules/Card",
  parameters: { layout: "padded" },
};

export default meta;

export const Default: StoryObj = {
  render: () => (
    <div className="card" style={{ padding: 20, maxWidth: 480 }}>
      <h3
        style={{
          margin: 0,
          marginBottom: 8,
          fontSize: 15,
          fontWeight: 600,
          color: "var(--text)",
        }}
      >
        Onboarding draft saved
      </h3>
      <p
        style={{
          margin: 0,
          color: "var(--text-secondary)",
          fontSize: 13,
          lineHeight: 1.55,
        }}
      >
        Your draft is queued locally. We sync it back when you reopen the
        wizard.
      </p>
    </div>
  ),
};

export const Stacked: StoryObj = {
  render: () => (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "repeat(2, 1fr)",
        gap: 16,
        maxWidth: 720,
      }}
    >
      {[
        { title: "Atlas", role: "engineer", line: "Writing migration plan…" },
        { title: "Lina", role: "designer", line: "Wireframing the inbox" },
        { title: "Sage", role: "writer", line: "Drafting the FAQ" },
        { title: "Ops", role: "ops", line: "Watching CI" },
      ].map(({ title, role, line }) => (
        <div key={title} className="card" style={{ padding: 16 }}>
          <div
            style={{
              fontSize: 14,
              fontWeight: 600,
              color: "var(--text)",
              marginBottom: 2,
            }}
          >
            {title}
          </div>
          <div
            style={{
              fontSize: 11,
              color: "var(--text-tertiary)",
              textTransform: "uppercase",
              letterSpacing: "0.04em",
              marginBottom: 8,
            }}
          >
            {role}
          </div>
          <div style={{ fontSize: 13, color: "var(--text-secondary)" }}>
            {line}
          </div>
        </div>
      ))}
    </div>
  ),
};
