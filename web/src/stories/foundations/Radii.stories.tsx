import type { Meta, StoryObj } from "@storybook/react-vite";

import { Section, TokenRow } from "./_swatch";

const meta: Meta = {
  title: "Design System/Foundations/Radii",
  parameters: { layout: "fullscreen" },
};

export default meta;

const RADII = [
  { token: "--radius-sm", note: "inputs, kbd, small chips" },
  { token: "--radius-md", note: "cards, modals, toasts" },
  { token: "--radius-lg", note: "sheets, settings panels" },
  { token: "--radius-xl", note: "hero surfaces" },
  { token: "--radius-full", note: "avatars, status dots, pills" },
];

export const All: StoryObj = {
  render: () => (
    <Section
      title="Border radius"
      description="Four steps + `--radius-full`. Theme overrides may shift them — `nex.css` uses smaller radii than `global.css`'s defaults."
    >
      <div style={{ padding: "0 16px 16px", maxWidth: 920 }}>
        {RADII.map(({ token, note }) => (
          <TokenRow
            key={token}
            token={token}
            preview={
              <div
                style={{
                  width: 96,
                  height: 56,
                  background: "var(--accent-bg)",
                  border: "1px solid var(--accent)",
                  borderRadius: `var(${token})`,
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
