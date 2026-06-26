import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "Design System/Atoms/Badge",
  parameters: { layout: "padded" },
};

export default meta;

const BADGES: Array<{ cls: string; label: string; meaning: string }> = [
  {
    cls: "badge badge-green",
    label: "shipped",
    meaning: "Success — finished, healthy, online",
  },
  {
    cls: "badge badge-accent",
    label: "draft",
    meaning: "Accent / olive — work-in-progress",
  },
  {
    cls: "badge badge-neutral",
    label: "archived",
    meaning: "Default — inactive, informational",
  },
  {
    cls: "badge badge-yellow",
    label: "warn",
    meaning: "Caution — needs attention but not critical",
  },
  {
    cls: "badge badge-orange",
    label: "stuck",
    meaning: "Warning — agent or task is blocked",
  },
];

export const Variants: StoryObj = {
  render: () => (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
      {BADGES.map(({ cls, label }) => (
        <span key={cls} className={cls}>
          {label}
        </span>
      ))}
    </div>
  ),
};

export const Guidance: StoryObj = {
  render: () => (
    <div style={{ maxWidth: 720, color: "var(--text)" }}>
      <table style={{ width: "100%", fontSize: 12, borderCollapse: "collapse" }}>
        <thead>
          <tr style={{ textAlign: "left", color: "var(--text-secondary)" }}>
            <th style={{ padding: "8px 12px" }}>Class</th>
            <th style={{ padding: "8px 12px" }}>Preview</th>
            <th style={{ padding: "8px 12px" }}>Use when</th>
          </tr>
        </thead>
        <tbody>
          {BADGES.map(({ cls, label, meaning }) => (
            <tr
              key={cls}
              style={{ borderTop: "1px solid var(--border-light)" }}
            >
              <td
                style={{
                  padding: "8px 12px",
                  fontFamily: "var(--font-mono)",
                  fontSize: 11,
                }}
              >
                {cls}
              </td>
              <td style={{ padding: "8px 12px" }}>
                <span className={cls}>{label}</span>
              </td>
              <td
                style={{ padding: "8px 12px", color: "var(--text-secondary)" }}
              >
                {meaning}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  ),
};
