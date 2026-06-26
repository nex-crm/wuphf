import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { AgentRequest } from "../../api/client";
import { parseApprovalContext } from "../../lib/parseApprovalContext";
import {
  deriveActionIdentity,
  ExternalActionApprovalCard,
} from "./ExternalActionApprovalCard";

const GMAIL_CONTEXT = [
  "Why: Sending the welcome note.",
  "",
  "What this will do:",
  "• To: alex@nex.ai",
  "• Subject: Welcome to Nex",
  "• Body: Hi Alex, welcome aboard!",
  "",
  "Action: GMAIL_SEND_EMAIL via Gmail",
  "Account: live::gmail::default::abc123",
  "Channel: #general",
].join("\n");

function makeApprovalRequest(
  overrides: Partial<AgentRequest> = {},
): AgentRequest {
  return {
    id: "req-1",
    from: "growthops",
    kind: "approval",
    question: "@growthops wants to send email via Gmail. Approve?",
    title: "Send Email via Gmail",
    channel: "general",
    context: GMAIL_CONTEXT,
    options: [
      { id: "approve", label: "Approve" },
      { id: "reject", label: "Reject" },
    ],
    recommended_id: "approve",
    ...overrides,
  };
}

function renderCard(overrides: Partial<AgentRequest> = {}) {
  const onAnswer = vi.fn();
  const onApproveAlways = vi.fn();
  const onDismiss = vi.fn();
  render(
    <ExternalActionApprovalCard
      request={makeApprovalRequest(overrides)}
      submitting={false}
      onAnswer={onAnswer}
      onApproveAlways={onApproveAlways}
      onDismiss={onDismiss}
    />,
  );
  return { onAnswer, onApproveAlways, onDismiss };
}

describe("deriveActionIdentity", () => {
  it("splits the verb, action id, and platform out of the legacy strings", () => {
    const parsed = parseApprovalContext(GMAIL_CONTEXT);
    const identity = deriveActionIdentity(makeApprovalRequest(), parsed);
    expect(identity).toEqual({
      headline: "Send Email",
      actionId: "GMAIL_SEND_EMAIL",
      platformName: "Gmail",
      platformSlug: "gmail",
    });
  });

  it("falls back to the action id prefix for the platform slug", () => {
    const req = makeApprovalRequest({
      title: undefined,
      context: ["Action: SLACK_SEND_MESSAGE", "Channel: #general"].join("\n"),
    });
    const identity = deriveActionIdentity(req, parseApprovalContext(req.context));
    expect(identity.actionId).toBe("SLACK_SEND_MESSAGE");
    expect(identity.platformSlug).toBe("slack");
    // No "via X" and no title — the headline degrades to a title-cased id.
    expect(identity.headline).toBe("Slack Send Message");
  });
});

describe("<ExternalActionApprovalCard>", () => {
  it("shows the integration, verb, action id, payload, and account", () => {
    renderCard();
    expect(screen.getByText("Gmail")).toBeInTheDocument();
    expect(screen.getByText("Send Email")).toBeInTheDocument();
    expect(screen.getByText("GMAIL_SEND_EMAIL")).toBeInTheDocument();
    expect(screen.getByText("Sending the welcome note.")).toBeInTheDocument();
    expect(screen.getByText("To")).toBeInTheDocument();
    expect(screen.getByText("alex@nex.ai")).toBeInTheDocument();
    expect(
      screen.getByText("live::gmail::default::abc123"),
    ).toBeInTheDocument();
  });

  it("toggles the raw payload view", () => {
    renderCard();
    // Friendly fields are shown by default; the raw block is not.
    expect(screen.queryByText(/Subject: Welcome to Nex/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /show raw/i }));
    // Raw view renders the masked payload as a single block.
    expect(screen.getByText(/Subject: Welcome to Nex/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /hide raw/i }));
    expect(screen.queryByText(/Subject: Welcome to Nex/)).not.toBeInTheDocument();
  });

  it("approves and rejects with the right choice id", () => {
    const { onAnswer } = renderCard();
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    expect(onAnswer).toHaveBeenCalledWith("approve");
    fireEvent.click(screen.getByRole("button", { name: "Reject" }));
    expect(onAnswer).toHaveBeenCalledWith("reject");
  });

  it("mints a scoped grant target on always-allow", () => {
    const { onApproveAlways } = renderCard();
    fireEvent.click(screen.getByRole("button", { name: /always allow/i }));
    expect(onApproveAlways).toHaveBeenCalledWith({
      agentSlug: "growthops",
      platform: "gmail",
      actionId: "GMAIL_SEND_EMAIL",
      channel: "general",
    });
  });

  it("hides always-allow when the action cannot be scoped", () => {
    // No parseable Action footer → no action id → a grant cannot be scoped.
    renderCard({ context: "Some freeform note with no action footer." });
    expect(
      screen.queryByRole("button", { name: /always allow/i }),
    ).not.toBeInTheDocument();
    // Approve / Reject remain available.
    expect(screen.getByRole("button", { name: "Approve" })).toBeInTheDocument();
  });
});

describe("<ExternalActionApprovalCard> structured payload (slice 4b)", () => {
  it("derives identity from the structured action when present", () => {
    const parsed = parseApprovalContext(GMAIL_CONTEXT);
    const identity = deriveActionIdentity(
      makeApprovalRequest({
        title: "ignored legacy title",
        action: {
          platform: "slack",
          action_id: "SLACK_SEND_MESSAGE",
          verb: "Post Message",
          name: "Slack",
        },
      }),
      parsed,
    );
    expect(identity).toEqual({
      headline: "Post Message",
      actionId: "SLACK_SEND_MESSAGE",
      platformName: "Slack",
      platformSlug: "slack",
    });
  });

  it("shows the real masked HTTP envelope behind the raw toggle", () => {
    renderCard({
      action: {
        platform: "gmail",
        action_id: "GMAIL_SEND_EMAIL",
        verb: "Send Email",
        name: "Gmail",
        raw_envelope: {
          method: "POST",
          url: "https://backend.composio.dev/api/v3/tools/execute",
          data: { to: "lead@acme.com", token: "***" },
        },
      },
    });
    fireEvent.click(screen.getByRole("button", { name: /show raw/i }));
    expect(
      screen.getByText(/POST https:\/\/backend\.composio\.dev/),
    ).toBeInTheDocument();
    // The masked secret is shown as-is (already redacted server-side); never raw.
    expect(screen.getByText(/"token": "\*\*\*"/)).toBeInTheDocument();
  });

  it("warns when the connection could not be verified (review LOW #5)", () => {
    renderCard({ connection_unverified: true });
    expect(
      screen.getByText(/connection unverified/i),
    ).toBeInTheDocument();
  });
});
