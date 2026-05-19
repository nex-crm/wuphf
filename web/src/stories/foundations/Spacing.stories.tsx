import type { Meta, StoryObj } from "@storybook/react-vite";

import { Section, TokenRow } from "./_swatch";

const meta: Meta = {
  title: "Design System/Foundations/Spacing",
  parameters: { layout: "fullscreen" },
};

export default meta;

const SCALE = [
  { token: "--space-1", px: 4, role: "tight — icon-to-text" },
  { token: "--space-2", px: 8, role: "default gap" },
  { token: "--space-3", px: 12, role: "card padding sm" },
  { token: "--space-4", px: 16, role: "card padding md" },
  { token: "--space-5", px: 20, role: "section gap" },
  { token: "--space-6", px: 24, role: "card padding lg / section padding" },
];

export const Scale: StoryObj = {
  render: () => (
    <Section
      title="Spacing scale"
      description="Six-step linear ramp. Prefer tokens over raw pixel values so spacing rhythm survives theme + density adjustments."
    >
      <div style={{ padding: "0 16px 16px", maxWidth: 920 }}>
        {SCALE.map(({ token, px, role }) => (
          <TokenRow
            key={token}
            token={token}
            preview={
              <div
                style={{
                  height: 14,
                  width: `var(${token})`,
                  background: "var(--accent)",
                  borderRadius: 2,
                }}
              />
            }
            note={`${px}px — ${role}`}
          />
        ))}
      </div>
    </Section>
  ),
};

export const StackExamples: StoryObj = {
  name: "In context",
  render: () => (
    <div style={{ padding: 24 }}>
      {SCALE.map(({ token, px }) => (
        <div
          key={token}
          style={{
            display: "flex",
            alignItems: "center",
            gap: 16,
            marginBottom: 12,
          }}
        >
          <code
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: "var(--text-secondary)",
              width: 90,
            }}
          >
            {token}
          </code>
          <div
            style={{
              display: "flex",
              gap: `var(${token})`,
              padding: 8,
              border: "1px dashed var(--border)",
              borderRadius: 4,
            }}
          >
            {[0, 1, 2, 3].map((i) => (
              <div
                key={i}
                style={{
                  width: 32,
                  height: 32,
                  background: "var(--accent-bg)",
                  borderRadius: 4,
                }}
              />
            ))}
          </div>
          <span style={{ fontSize: 11, color: "var(--text-tertiary)" }}>
            {px}px
          </span>
        </div>
      ))}
    </div>
  ),
};
