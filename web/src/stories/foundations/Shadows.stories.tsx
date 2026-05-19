import type { Meta, StoryObj } from "@storybook/react-vite";

import { Section, TokenRow } from "./_swatch";

const meta: Meta = {
  title: "Design System/Foundations/Shadows",
  parameters: { layout: "fullscreen" },
};

export default meta;

const SHADOWS = [
  {
    label: "--shadow-popover",
    value: "var(--shadow-popover)",
    note: "menus, autocomplete, hover popovers",
  },
  {
    label: "card",
    value:
      "0 1px 3px rgba(59, 47, 47, 0.04), 0 8px 30px rgba(59, 47, 47, 0.04)",
    note: ".card — see Patterns / Card",
  },
  {
    label: "message action bar",
    value: "0 2px 8px rgba(0, 0, 0, 0.08)",
    note: "floats above message on hover",
  },
  {
    label: "confirm dialog",
    value: "0 12px 40px rgba(0, 0, 0, 0.12)",
    note: "modal cards",
  },
  {
    label: "cmd palette",
    value: "0 20px 60px rgba(0, 0, 0, 0.15)",
    note: "elevated overlay",
  },
];

export const All: StoryObj = {
  render: () => (
    <Section
      title="Elevation"
      description="Only `--shadow-popover` is tokenized today. Other elevations live inline next to their component — replace with tokens as the system grows."
    >
      <div
        style={{
          padding: "32px 16px",
          maxWidth: 920,
          background: "var(--bg-warm)",
        }}
      >
        {SHADOWS.map(({ label, value, note }) => (
          <TokenRow
            key={label}
            token={label}
            preview={
              <div
                style={{
                  width: 160,
                  height: 56,
                  background: "var(--bg-card)",
                  border: "1px solid var(--border)",
                  borderRadius: "var(--radius-md)",
                  boxShadow: value,
                }}
              />
            }
            note={note}
          />
        ))}
      </div>
    </Section>
  ),
};
