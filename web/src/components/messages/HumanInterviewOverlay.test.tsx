import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { AgentRequest } from "../../api/client";

const useRequestsMock = vi.fn();
vi.mock("../../hooks/useRequests", () => ({
  useRequests: () => useRequestsMock(),
}));

vi.mock("../../api/client", async () => {
  const actual =
    await vi.importActual<typeof import("../../api/client")>(
      "../../api/client",
    );
  return {
    ...actual,
    answerRequest: vi.fn().mockResolvedValue({}),
    cancelRequest: vi.fn().mockResolvedValue({}),
  };
});

import { HumanInterviewOverlay } from "./HumanInterviewOverlay";

function wrap(ui: ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

function makeRequest(overrides: Partial<AgentRequest> = {}): AgentRequest {
  return {
    id: "req-1",
    from: "planner",
    question: "What should we do?",
    blocking: true,
    status: "pending",
    options: [
      { id: "yes", label: "Yes" },
      { id: "no", label: "No" },
    ],
    ...overrides,
  };
}

describe("<HumanInterviewOverlay>", () => {
  it("renders nothing when there is no blocking request", () => {
    useRequestsMock.mockReturnValue({ blockingPending: null, pending: [] });
    const { container } = render(wrap(<HumanInterviewOverlay />));
    expect(container.firstChild).toBeNull();
  });

  it("renders the blocking interview card for a normal request", () => {
    const req = makeRequest({ title: "Approve the plan?" });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));
    expect(screen.getByText("Approve the plan?")).toBeInTheDocument();
    expect(screen.getByText("Yes")).toBeInTheDocument();
    expect(screen.getByText("No")).toBeInTheDocument();
  });

  it("shows the enhance-existing banner for enhance_skill_proposal kind", () => {
    const req = makeRequest({
      kind: "enhance_skill_proposal",
      title: "Enhance existing skill?",
      options: [
        { id: "enhance", label: "Enhance" },
        { id: "approve_anyway", label: "Approve anyway" },
        { id: "reject", label: "Reject" },
      ],
      recommended_id: "enhance",
      metadata: { enhances_slug: "email-ops" },
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));

    expect(screen.getByText(/Similar to existing skill:/i)).toBeInTheDocument();
    expect(screen.getByText("email-ops")).toBeInTheDocument();
    expect(screen.getByText("Enhance")).toBeInTheDocument();
    expect(screen.getByText("Approve anyway")).toBeInTheDocument();
    expect(screen.getByText("Reject")).toBeInTheDocument();
  });

  it("does not show the enhance banner for a normal skill_proposal", () => {
    const req = makeRequest({
      kind: "skill_proposal",
      options: [
        { id: "accept", label: "Accept" },
        { id: "reject", label: "Reject" },
      ],
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));

    expect(
      screen.queryByText(/Similar to existing skill:/i),
    ).not.toBeInTheDocument();
  });

  it("renders the EXTERNAL ACTION badge and Variant B layout for approval-kind requests", () => {
    const req = makeRequest({
      kind: "approval",
      title: "Send Email via Gmail",
      question: "@growthops wants to send email via Gmail. Approve?",
      context: [
        "Why: Sending welcome note.",
        "",
        "What this will do:",
        "• To: alex@nex.ai",
        "• Subject: Welcome to Nex",
        "• Body: Hi Alex, welcome aboard!",
        "",
        "Action: GMAIL_SEND_EMAIL via Gmail",
        "Account: live::gmail::default::abc123",
        "Channel: #general",
      ].join("\n"),
      options: [
        { id: "approve", label: "Approve" },
        { id: "reject", label: "Reject" },
      ],
      recommended_id: "approve",
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));

    expect(screen.getByText("EXTERNAL ACTION")).toBeInTheDocument();
    expect(screen.getByText("BLOCKING")).toBeInTheDocument();
    expect(screen.getByText("Send Email via Gmail")).toBeInTheDocument();
    // Why surface
    expect(screen.getByText("Sending welcome note.")).toBeInTheDocument();
    // Details inset: each label/value is a <dt>/<dd> pair
    expect(screen.getByText("To")).toBeInTheDocument();
    expect(screen.getByText("alex@nex.ai")).toBeInTheDocument();
    expect(screen.getByText("Subject")).toBeInTheDocument();
    expect(screen.getByText("Welcome to Nex")).toBeInTheDocument();
    expect(screen.getByText("Body")).toBeInTheDocument();
    expect(screen.getByText("Hi Alex, welcome aboard!")).toBeInTheDocument();
    // Footer
    expect(screen.getByText(/GMAIL_SEND_EMAIL via Gmail/)).toBeInTheDocument();
    expect(
      screen.getByText("live::gmail::default::abc123"),
    ).toBeInTheDocument();
  });

  it("renders the (truncated) chip when a detail value ends with the ellipsis", () => {
    const req = makeRequest({
      kind: "approval",
      title: "Send Email via Gmail",
      context: [
        "What this will do:",
        "• To: alex@nex.ai",
        "• Body: Hi Alex this is a very long body…",
        "",
        "Action: GMAIL_SEND_EMAIL via Gmail",
        "Channel: #general",
      ].join("\n"),
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));
    expect(screen.getByText("(truncated)")).toBeInTheDocument();
  });

  it("renders the empty fallback when neither Why nor details are present", () => {
    const req = makeRequest({
      kind: "approval",
      title: "Refresh OAuth Token via Gmail",
      context: [
        "Action: GMAIL_REFRESH_TOKEN via Gmail",
        "Channel: #general",
      ].join("\n"),
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));
    expect(
      screen.getByText(/No additional details available/i),
    ).toBeInTheDocument();
  });

  it("falls back to plain pre-wrap context for legacy/non-approval requests", () => {
    const req = makeRequest({
      kind: "interview",
      title: "Pricing call?",
      context: "platform: gmail\naction_id: GMAIL_SEND_EMAIL",
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));

    expect(screen.queryByText("EXTERNAL ACTION")).not.toBeInTheDocument();
    const ctx = screen.getByText((_, el) => {
      if (!el || el.tagName.toLowerCase() !== "p") return false;
      return (el.textContent ?? "").startsWith("platform: gmail");
    });
    expect(ctx.getAttribute("style") ?? "").toContain("pre-wrap");
  });

  it("styles the recommended option with btn-primary class", () => {
    const req = makeRequest({
      kind: "enhance_skill_proposal",
      options: [
        { id: "enhance", label: "Enhance" },
        { id: "approve_anyway", label: "Approve anyway" },
        { id: "reject", label: "Reject" },
      ],
      recommended_id: "enhance",
      metadata: { enhances_slug: "existing-skill" },
    });
    useRequestsMock.mockReturnValue({ blockingPending: req, pending: [req] });
    render(wrap(<HumanInterviewOverlay />));

    const enhanceBtn = screen.getByRole("button", { name: "Enhance" });
    expect(enhanceBtn.className).toContain("btn-primary");

    const approveBtn = screen.getByRole("button", { name: "Approve anyway" });
    expect(approveBtn.className).toContain("btn-ghost");
  });
});
