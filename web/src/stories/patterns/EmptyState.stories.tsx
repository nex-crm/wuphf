import { MailIn } from "iconoir-react";

import type { Meta, StoryObj } from "@storybook/react-vite";

const meta: Meta = {
  title: "Design System/Molecules/Empty state",
  parameters: { layout: "padded" },
};

export default meta;

export const Minimal: StoryObj = {
  render: () => (
    <div
      className="empty-state empty-state--padded"
      style={{ minHeight: 200 }}
    >
      No tasks yet — the office is quiet.
    </div>
  ),
};

export const WithIcon: StoryObj = {
  render: () => (
    <div
      className="empty-state empty-state--padded"
      style={{
        flexDirection: "column",
        gap: 12,
        minHeight: 260,
        textAlign: "center",
      }}
    >
      <MailIn width={32} height={32} />
      <div>
        <div
          style={{
            color: "var(--text)",
            fontWeight: 600,
            marginBottom: 4,
            fontSize: 14,
          }}
        >
          Inbox zero
        </div>
        <div style={{ color: "var(--text-secondary)", fontSize: 13 }}>
          Nothing waiting on you. Agents will ping when they need a call.
        </div>
      </div>
    </div>
  ),
};
