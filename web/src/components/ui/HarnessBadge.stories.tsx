import type { Meta, StoryObj } from "@storybook/react-vite";

import type { HarnessKind } from "../../lib/harness";

import { HarnessBadge } from "./HarnessBadge";

const KINDS: HarnessKind[] = [
  "claude-code",
  "codex",
  "opencode",
  "openclaw",
  "hermes-agent",
];

const meta: Meta<typeof HarnessBadge> = {
  title: "Design System/Atoms/HarnessBadge",
  component: HarnessBadge,
  argTypes: {
    kind: { control: "select", options: KINDS },
    size: { control: { type: "range", min: 12, max: 64, step: 2 } },
  },
  args: { kind: "claude-code", size: 24 },
};

export default meta;
type Story = StoryObj<typeof HarnessBadge>;

export const Default: Story = {};

export const AllKinds: StoryObj = {
  render: () => (
    <div style={{ display: "flex", gap: 16, alignItems: "center" }}>
      {KINDS.map((kind) => (
        <div
          key={kind}
          style={{
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            gap: 6,
            fontSize: 11,
            color: "var(--text-secondary)",
          }}
        >
          <HarnessBadge kind={kind} size={32} />
          <span>{kind}</span>
        </div>
      ))}
    </div>
  ),
};

export const Sizes: StoryObj = {
  render: () => (
    <div style={{ display: "flex", gap: 16, alignItems: "center" }}>
      {[16, 24, 32, 48, 64].map((size) => (
        <HarnessBadge key={size} kind="claude-code" size={size} />
      ))}
    </div>
  ),
};
