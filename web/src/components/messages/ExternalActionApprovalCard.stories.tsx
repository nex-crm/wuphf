import type { Meta, StoryObj } from "@storybook/react-vite";

import type { AgentRequest } from "../../api/client";
import { ExternalActionApprovalCard } from "./ExternalActionApprovalCard";

// Renders the card inside a surface that mimics the blocking-interview shell so
// the story reads like the real overlay without mounting the whole app.
function CardSurface({ request }: { request: AgentRequest }) {
  return (
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
      <div
        style={{
          display: "flex",
          gap: 8,
          marginBottom: 12,
          alignItems: "center",
        }}
      >
        <span className="badge badge-yellow">BLOCKING</span>
        <span className="badge badge-orange">EXTERNAL ACTION</span>
        <span style={{ fontSize: 12, color: "var(--text-secondary)" }}>
          @{request.from} in #{request.channel}
        </span>
      </div>
      <ExternalActionApprovalCard
        request={request}
        submitting={false}
        onAnswer={() => {}}
        onApproveAlways={() => {}}
        onDismiss={() => {}}
      />
    </div>
  );
}

const meta: Meta<typeof CardSurface> = {
  title: "Features/Integrations/ExternalActionApprovalCard",
  component: CardSurface,
  parameters: { layout: "centered" },
};

export default meta;
type Story = StoryObj<typeof CardSurface>;

const gmailSend: AgentRequest = {
  id: "req-1",
  from: "growthops",
  kind: "approval",
  question: "@growthops wants to send email via Gmail. Approve?",
  title: "Send Email via Gmail",
  channel: "general",
  context: [
    "Why: Following up on Alex's demo request from this morning.",
    "",
    "What this will do:",
    "• To: alex@nex.ai",
    "• Subject: Great meeting you — next steps",
    "• Body: Hi Alex, thanks for the time today. Here is the trial link…",
    "",
    "Action: GMAIL_SEND_EMAIL via Gmail",
    "Account: Founder Gmail (alex@foundermail.com)",
    "Channel: #general",
  ].join("\n"),
  options: [
    { id: "approve", label: "Approve" },
    { id: "reject", label: "Reject" },
  ],
  recommended_id: "approve",
};

export const GmailSend: Story = {
  args: { request: gmailSend },
};

export const SlackPost: Story = {
  args: {
    request: {
      ...gmailSend,
      from: "support",
      title: "Send Message via Slack",
      question: "@support wants to post to Slack. Approve?",
      context: [
        "Why: Escalating the outage to the on-call channel.",
        "",
        "What this will do:",
        "• Channel: #incidents",
        "• Text: ⚠️ Payments API p95 latency above 2s for 5m — paging on-call.",
        "",
        "Action: SLACK_SEND_MESSAGE via Slack",
        "Account: Acme Workspace",
        "Channel: #ops",
      ].join("\n"),
    },
  },
};

export const MinimalNoPayload: Story = {
  args: {
    request: {
      ...gmailSend,
      title: "Refresh Token via Gmail",
      question: "@growthops wants to refresh the Gmail token. Approve?",
      context: [
        "Action: GMAIL_REFRESH_TOKEN via Gmail",
        "Account: Founder Gmail",
        "Channel: #general",
      ].join("\n"),
    },
  },
};

export const Redacted: Story = {
  args: {
    request: {
      ...gmailSend,
      redacted: true,
      redaction_reasons: ["api_key"],
    },
  },
};

// Slice 4b: a structured payload carrying the real masked HTTP envelope. The
// raw toggle shows the actual request (method + url + masked body).
export const StructuredWithEnvelope: Story = {
  args: {
    request: {
      ...gmailSend,
      action: {
        platform: "gmail",
        action_id: "GMAIL_SEND_EMAIL",
        verb: "Send Email",
        name: "Gmail",
        account: { name: "Founder Gmail (alex@foundermail.com)" },
        raw_envelope: {
          method: "POST",
          url: "https://backend.composio.dev/api/v3/tools/execute/GMAIL_SEND_EMAIL",
          data: {
            recipient_email: "alex@nex.ai",
            subject: "Great meeting you — next steps",
            body: "Hi Alex, thanks for the time today…",
            user_id: "***",
          },
        },
      },
    },
  },
};

// Review LOW #5: the gate could not reach the resolver, so the connection is
// unconfirmed — the card warns the human before they approve.
export const ConnectionUnverified: Story = {
  args: {
    request: { ...gmailSend, connection_unverified: true },
  },
};
