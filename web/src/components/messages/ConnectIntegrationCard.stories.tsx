import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Meta, StoryObj } from "@storybook/react-vite";

import type { AgentRequest } from "../../api/client";
import { ConnectIntegrationCard } from "./ConnectIntegrationCard";

function CardSurface({ request }: { request: AgentRequest }) {
  const qc = new QueryClient();
  return (
    <QueryClientProvider client={qc}>
      <div
        style={{
          maxWidth: 460,
          padding: "18px 20px",
          background: "var(--bg-card)",
          border: "1px solid var(--border)",
          borderRadius: "var(--radius-lg, 12px)",
          boxShadow: "0 12px 40px var(--overlay-soft)",
        }}
      >
        <div style={{ display: "flex", gap: 8, marginBottom: 12 }}>
          <span className="badge badge-yellow">BLOCKING</span>
          <span className="badge badge-orange">CONNECT</span>
        </div>
        <ConnectIntegrationCard
          request={request}
          submitting={false}
          onSkip={() => {}}
          onDismiss={() => {}}
        />
      </div>
    </QueryClientProvider>
  );
}

const meta: Meta<typeof CardSurface> = {
  title: "Features/Integrations/ConnectIntegrationCard",
  component: CardSurface,
  parameters: { layout: "centered" },
};

export default meta;
type Story = StoryObj<typeof CardSurface>;

const base: AgentRequest = {
  id: "request-9",
  from: "growthops",
  kind: "connect",
  question: "Connect Gmail so the team can send the follow-up email.",
  title: "Connect Gmail",
  channel: "general",
  platform: "gmail",
  options: [
    { id: "connect", label: "Connect" },
    { id: "skip", label: "Skip" },
  ],
  recommended_id: "connect",
};

export const Gmail: Story = {
  args: { request: base },
};

export const Slack: Story = {
  args: {
    request: {
      ...base,
      from: "support",
      title: "Connect Slack",
      platform: "slack",
      question: "Connect Slack so the team can post the incident update.",
    },
  },
};
