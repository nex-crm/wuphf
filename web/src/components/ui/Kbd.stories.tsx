import type { Meta, StoryObj } from "@storybook/react-vite";

import { Kbd, KbdSequence, MOD_KEY } from "./Kbd";

const meta: Meta<typeof Kbd> = {
  title: "Design System/Atoms/Kbd",
  component: Kbd,
  parameters: { layout: "centered" },
  argTypes: {
    size: { control: "inline-radio", options: ["sm", "md"] },
    variant: { control: "inline-radio", options: ["default", "inverse"] },
  },
  args: { children: "K", size: "md", variant: "default" },
};

export default meta;
type Story = StoryObj<typeof Kbd>;

export const Default: Story = {};

const SIZES = ["sm", "md"] as const;
const VARIANTS = ["default", "inverse"] as const;

export const Variants: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: `repeat(${SIZES.length}, auto)`,
        gap: 12,
        padding: 12,
        background: "var(--bg-warm)",
        borderRadius: "var(--radius-sm)",
        alignItems: "center",
      }}
    >
      {VARIANTS.flatMap((variant) =>
        SIZES.map((size) => (
          <Kbd key={`${variant}-${size}`} size={size} variant={variant}>
            ⌘
          </Kbd>
        )),
      )}
    </div>
  ),
};

export const Sequences: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <KbdSequence keys={[MOD_KEY, "K"]} />
      <KbdSequence keys={[MOD_KEY, "Shift", "P"]} />
      <KbdSequence keys={[["g"], ["g"]]} />
      <KbdSequence keys={["?"]} />
    </div>
  ),
};

export const InContext: Story = {
  name: "In context",
  parameters: { controls: { disable: true } },
  render: () => (
    <p
      style={{
        fontSize: 14,
        color: "var(--text)",
        maxWidth: 480,
        lineHeight: 1.6,
      }}
    >
      Press <Kbd>?</Kbd> to open the help modal. Use <Kbd>Esc</Kbd> to close
      the topmost panel, or <KbdSequence keys={[MOD_KEY, "K"]} /> for the
      command palette.
    </p>
  ),
};
