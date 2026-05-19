import { useEffect } from "react";

import type { Meta, StoryObj } from "@storybook/react-vite";

import { useAppStore } from "../../stores/app";

import { AgentEventPill, AgentEventTickProvider } from "./AgentEventPill";

const meta: Meta<typeof AgentEventPill> = {
  title: "Features/Agents/AgentEventPill",
  component: AgentEventPill,
  decorators: [
    (Story) => (
      <AgentEventTickProvider>
        <div
          style={{
            background: "var(--surface-2, #f5f5f5)",
            padding: 12,
            width: 220,
            borderRadius: 6,
          }}
        >
          <Story />
        </div>
      </AgentEventTickProvider>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof AgentEventPill>;

function seedSnapshot(snap: {
  slug: string;
  activity?: string;
  detail?: string;
  kind?: "routine" | "milestone" | "stuck";
  ageSeconds?: number;
}) {
  const ageMs = (snap.ageSeconds ?? 0) * 1000;
  useAppStore.setState((state) => ({
    agentActivitySnapshots: {
      ...state.agentActivitySnapshots,
      [snap.slug]: {
        slug: snap.slug,
        activity: snap.activity,
        detail: snap.detail,
        kind: snap.kind,
        receivedAtMs: Date.now() - ageMs,
        haloUntilMs: Date.now() - ageMs + 2500,
      },
    },
  }));
}

function useSeed(snap: Parameters<typeof seedSnapshot>[0]) {
  useEffect(() => {
    seedSnapshot(snap);
  }, [snap]);
}

export const Halo: Story = {
  render: () => {
    useSeed({
      slug: "atlas",
      activity: "writing migration plan",
      kind: "milestone",
      ageSeconds: 0,
    });
    return <AgentEventPill slug="atlas" agentRole="engineer" />;
  },
};

export const Holding: Story = {
  render: () => {
    useSeed({
      slug: "lina",
      activity: "running tests",
      kind: "routine",
      ageSeconds: 6,
    });
    return <AgentEventPill slug="lina" agentRole="engineer" />;
  },
};

export const Idle: Story = {
  render: () => {
    useSeed({
      slug: "sage",
      activity: "drafting wiki entry",
      ageSeconds: 90,
    });
    return (
      <AgentEventPill
        slug="sage"
        agentRole="writer"
        fallbackTask="curating reference docs"
      />
    );
  },
};

export const Stuck: Story = {
  render: () => {
    useSeed({
      slug: "ops",
      activity: "waiting on user",
      kind: "stuck",
      ageSeconds: 5,
    });
    return <AgentEventPill slug="ops" agentRole="ops" />;
  },
};
