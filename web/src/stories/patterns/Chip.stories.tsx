import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "Design System/Atoms/Chip",
  parameters: { layout: "padded" },
};

export default meta;

export const Variants: StoryObj = {
  render: () => (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
      <span className="chip">atlas</span>
      <span className="chip chip--accent">on-call</span>
      <button type="button" className="chip chip--interactive">
        #architecture
      </button>
    </div>
  ),
};

export const Guidance: StoryObj = {
  render: () => (
    <div style={{ maxWidth: 720, color: "var(--text)", fontSize: 13 }}>
      <p style={{ marginBottom: 12 }}>
        <strong>Chip vs Badge.</strong> Both are small inline labels. The
        difference is what they mean.
      </p>
      <table
        style={{
          width: "100%",
          fontSize: 12,
          borderCollapse: "collapse",
          marginBottom: 16,
        }}
      >
        <thead>
          <tr style={{ textAlign: "left", color: "var(--text-secondary)" }}>
            <th style={{ padding: "8px 12px" }}>Use</th>
            <th style={{ padding: "8px 12px" }}>For</th>
            <th style={{ padding: "8px 12px" }}>Example</th>
          </tr>
        </thead>
        <tbody>
          <tr style={{ borderTop: "1px solid var(--border-light)" }}>
            <td
              style={{
                padding: "8px 12px",
                fontFamily: "var(--font-mono)",
                fontSize: 11,
              }}
            >
              .chip
            </td>
            <td style={{ padding: "8px 12px" }}>
              Identity, ownership — pill-shaped, larger.
            </td>
            <td style={{ padding: "8px 12px" }}>
              <span className="chip">atlas</span>
            </td>
          </tr>
          <tr style={{ borderTop: "1px solid var(--border-light)" }}>
            <td
              style={{
                padding: "8px 12px",
                fontFamily: "var(--font-mono)",
                fontSize: 11,
              }}
            >
              .badge
            </td>
            <td style={{ padding: "8px 12px" }}>
              Status, state — rectangular, smaller, semantic color.
            </td>
            <td style={{ padding: "8px 12px" }}>
              <span className="badge badge-green">shipped</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  ),
};

export const InContext: StoryObj = {
  name: "In context",
  render: () => (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: 8,
        padding: 16,
        border: "1px solid var(--border)",
        borderRadius: "var(--radius-md)",
        background: "var(--bg-card)",
        maxWidth: 520,
      }}
    >
      <div style={{ color: "var(--text-tertiary)", fontSize: 11, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.06em" }}>
        Owners
      </div>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
        <span className="chip">atlas</span>
        <span className="chip">lina</span>
        <span className="chip chip--accent">on-call · ops</span>
      </div>
      <div style={{ color: "var(--text-tertiary)", fontSize: 11, fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.06em", marginTop: 8 }}>
        Channels
      </div>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
        <button type="button" className="chip chip--interactive">#architecture</button>
        <button type="button" className="chip chip--interactive">#deploys</button>
        <button type="button" className="chip chip--interactive">#wiki</button>
      </div>
    </div>
  ),
};
